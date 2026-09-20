package owoncontrol

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

const (
	// dmmFunctionObservationPacing limits repeated function queries without treating elapsed time as readiness.
	//
	// Example: a mismatch waits 250 milliseconds before the next observed function query.
	dmmFunctionObservationPacing = 250 * time.Millisecond
)

// DMMConvergencePhase identifies the bounded phase in which a DMM operation stopped.
//
// Example: DMMConvergencePhaseFinal reports failure while checking state after remaining writes.
type DMMConvergencePhase uint8

const (
	// DMMConvergencePhaseUnknown identifies an unset or unrecognized phase.
	//
	// Example: a zero-value diagnostic can still be formatted safely.
	DMMConvergencePhaseUnknown DMMConvergencePhase = iota
	// DMMConvergencePhaseAdmission identifies failure before the transaction was admitted.
	//
	// Example: a queued caller whose context expires reports the admission phase.
	DMMConvergencePhaseAdmission
	// DMMConvergencePhaseInitial identifies the function barrier after the CONFIGURE write.
	//
	// Example: malformed or nonconverging initial observations report this phase.
	DMMConvergencePhaseInitial
	// DMMConvergencePhaseConfigure identifies failure while writing the requested function.
	//
	// Example: an ambiguous CONFIGURE exchange reports this phase before any observation.
	DMMConvergencePhaseConfigure
	// DMMConvergencePhaseWrite identifies a remaining setting write failure.
	//
	// Example: a transport error on RANGE reports the write phase.
	DMMConvergencePhaseWrite
	// DMMConvergencePhaseFinal identifies the function barrier after remaining writes.
	//
	// Example: a timeout after AUTO reports the final function convergence phase.
	DMMConvergencePhaseFinal
)

// String returns the stable diagnostic label for a convergence phase.
//
// Example: DMMConvergencePhaseWrite.String returns "remaining writes".
func (phase DMMConvergencePhase) String() string {
	switch phase {
	case DMMConvergencePhaseAdmission:
		return "admission"
	case DMMConvergencePhaseInitial:
		return "initial function convergence"
	case DMMConvergencePhaseConfigure:
		return "CONFIGURE write"
	case DMMConvergencePhaseWrite:
		return "remaining writes"
	case DMMConvergencePhaseFinal:
		return "final function convergence"
	default:
		return "unknown"
	}
}

// ErrDMMConvergence identifies a DMM function operation that did not prove its requested function twice consecutively.
// It preserves the last valid function observation, the number of transport-completed writes, and the underlying cause; the device application may be partial or unknown after a write was attempted.
//
// Example: errors.As extracts the requested selection and errors.Is still matches context.DeadlineExceeded.
type ErrDMMConvergence struct {
	Phase                                  DMMConvergencePhase
	RequestedSelection                     owonmodel.DMMFunctionSelection
	LastValidObservation                   *owonmodel.DMMFunctionSelection
	SuccessfulTransportCompletedWriteCount int
	Cause                                  error
}

// Error describes the failed DMM phase and honest application uncertainty.
//
// Example: a final-barrier timeout names prior completed writes without claiming that AUTO was observed.
func (err *ErrDMMConvergence) Error() string {
	if err == nil {
		return "DMM convergence failed"
	}
	message := "DMM convergence failed"
	if err.Phase != DMMConvergencePhaseUnknown {
		message += " during " + err.Phase.String()
	}
	message += " for " + dmmFunctionDescription(err.RequestedSelection.Function)
	if err.RequestedSelection.CurrentType != nil {
		message += " (" + dmmCurrentTypeDescription(*err.RequestedSelection.CurrentType) + ")"
	}
	if err.LastValidObservation == nil {
		message += "; no valid observation"
	} else {
		message += "; last valid observation " + dmmFunctionDescription(err.LastValidObservation.Function)
		if err.LastValidObservation.CurrentType != nil {
			message += " (" + dmmCurrentTypeDescription(*err.LastValidObservation.CurrentType) + ")"
		}
	}
	message += "; " + dmmWriteCompletionDescription(err.SuccessfulTransportCompletedWriteCount)
	if err.Phase == DMMConvergencePhaseAdmission {
		message += "; no writes were attempted"
	} else {
		message += "; possible partial or unknown application"
	}
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	return message
}

// dmmFunctionDescription renders a DMM function name while retaining unknown values for diagnostics.
//
// Example: DMMFunctionDiode becomes "diode" instead of an opaque enum number.
func dmmFunctionDescription(function owonmodel.DMMFunction) string {
	switch function {
	case owonmodel.DMMFunctionVoltage:
		return "voltage"
	case owonmodel.DMMFunctionCurrent:
		return "current"
	case owonmodel.DMMFunctionResistance:
		return "resistance"
	case owonmodel.DMMFunctionCapacitance:
		return "capacitance"
	case owonmodel.DMMFunctionDiode:
		return "diode"
	case owonmodel.DMMFunctionContinuity:
		return "continuity"
	default:
		return fmt.Sprintf("function %d", function)
	}
}

// dmmCurrentTypeDescription renders a DMM AC/DC companion name while retaining unknown values for diagnostics.
//
// Example: DMMCurrentTypeDC becomes "DC" in a convergence error.
func dmmCurrentTypeDescription(currentType owonmodel.DMMCurrentType) string {
	switch currentType {
	case owonmodel.DMMCurrentTypeAC:
		return "AC"
	case owonmodel.DMMCurrentTypeDC:
		return "DC"
	default:
		return fmt.Sprintf("current type %d", currentType)
	}
}

// Unwrap preserves the cancellation, malformed-response, or transport cause.
//
// Example: errors.Is can classify a deadline while errors.As can inspect a malformed reply beneath this diagnostic.
func (err *ErrDMMConvergence) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// SetDMM compiles the complete multimeter patch before admitting any writes.
// Every function patch holds one admitted transaction across CONFIGURE and an
// initial two-observation barrier. Remaining settings are sent only after that
// barrier; a final two-observation barrier follows only when those settings
// exist. Function-only patches complete after the initial barrier, while
// settings-only patches retain per-exchange acknowledgement semantics.
// The configured session OperationTimeout supplies the post-admission
// convergence budget; caller cancellation and deadline govern admission.
//
// Example: a function/current mismatch cannot partially change relative mode, while a valid diode transition waits for observed convergence.
func (controller *Controller) SetDMM(
	ctx context.Context,
	request *owonmodel.DMMPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetDMM")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetDMM: %v", _err) }()
	}

	commands, err := owonscpi.CompileDMM(request)
	if err != nil {
		return err
	}
	var query owonprotocol.Command
	if request.Function != nil {
		query, err = owonscpi.DMMFunctionQuery(*request.Function)
		if err != nil {
			return err
		}
	}
	session, err := controller.sessionForOperation("set DMM")
	if err != nil {
		return err
	}
	if request.Function == nil {
		transaction, err := session.Begin(ctx)
		if err != nil {
			return err
		}
		defer transaction.Close()
		return controller.executeWritesWith(ctx, transaction, commands)
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return newDMMConvergenceError(
			DMMConvergencePhaseAdmission,
			request,
			nil,
			0,
			err,
		)
	}
	defer transaction.Close()
	operationContext, cancel, err := transaction.OperationContext(ctx)
	if err != nil {
		return newDMMConvergenceError(
			DMMConvergencePhaseAdmission,
			request,
			nil,
			0,
			err,
		)
	}
	defer cancel()
	requested := dmmFunctionSelection(request)
	completedWrites := 0
	if _, err := transaction.Execute(operationContext, commands[0]); err != nil {
		return newDMMConvergenceError(
			DMMConvergencePhaseConfigure,
			request,
			nil,
			completedWrites,
			fmt.Errorf("apply command %q: %w", commands[0].Text, err),
		)
	}
	completedWrites++
	lastObservation, err := waitForDMMFunction(operationContext, transaction, query, requested)
	if err != nil {
		return newDMMConvergenceError(
			DMMConvergencePhaseInitial,
			request,
			lastObservation,
			completedWrites,
			err,
		)
	}
	for _, command := range commands[1:] {
		if _, err := transaction.Execute(operationContext, command); err != nil {
			return newDMMConvergenceError(
				DMMConvergencePhaseWrite,
				request,
				lastObservation,
				completedWrites,
				fmt.Errorf("apply command %q: %w", command.Text, err),
			)
		}
		completedWrites++
	}
	if len(commands) == 1 {
		return nil
	}
	finalObservation, err := waitForDMMFunction(operationContext, transaction, query, requested)
	if finalObservation != nil {
		lastObservation = finalObservation
	}
	if err != nil {
		return newDMMConvergenceError(
			DMMConvergencePhaseFinal,
			request,
			lastObservation,
			completedWrites,
			err,
		)
	}
	return nil
}

// dmmWriteCompletionDescription keeps convergence diagnostics grammatical
// while preserving the exact completed-write count.
//
// Example: one completed write is rendered as "1 write completed".
func dmmWriteCompletionDescription(count int) string {
	switch count {
	case 0:
		return "no writes completed"
	case 1:
		return "1 write completed"
	default:
		return fmt.Sprintf("%d writes completed", count)
	}
}

// dmmFunctionSelection copies the requested function context into a readback comparison value.
//
// Example: a voltage/DC patch retains its explicit DC companion without sharing the caller's pointer.
func dmmFunctionSelection(request *owonmodel.DMMPatch) owonmodel.DMMFunctionSelection {
	selection := owonmodel.DMMFunctionSelection{Function: *request.Function}
	if request.CurrentType != nil {
		currentType := *request.CurrentType
		selection.CurrentType = &currentType
	}
	return selection
}

// newDMMConvergenceError builds the public partial-application diagnostic from one failed phase.
//
// Example: a failed first CONFIGURE write reports zero completed writes while preserving its transport cause.
func newDMMConvergenceError(
	phase DMMConvergencePhase,
	request *owonmodel.DMMPatch,
	lastObservation *owonmodel.DMMFunctionSelection,
	completedWrites int,
	cause error,
) error {
	return &ErrDMMConvergence{
		Phase:                                  phase,
		RequestedSelection:                     dmmFunctionSelection(request),
		LastValidObservation:                   lastObservation,
		SuccessfulTransportCompletedWriteCount: completedWrites,
		Cause:                                  cause,
	}
}

// waitForDMMFunction performs cancellable observation pacing until two target matches are consecutive.
// Exact device `error` replies reset the match count; malformed replies terminate the operation.
//
// Example: continuity, diode, diode requires three queries before the function barrier completes for diode.
func waitForDMMFunction(
	ctx context.Context,
	executor commandExecutor,
	query owonprotocol.Command,
	requested owonmodel.DMMFunctionSelection,
) (_lastObservation *owonmodel.DMMFunctionSelection, _err error) {
	matches := 0
	for {
		response, err := executor.Execute(ctx, query)
		if err != nil {
			return _lastObservation, fmt.Errorf("query DMM function: %w", err)
		}
		observation, err := owonscpi.ParseDMMFunction(requested.Function, response)
		if err != nil {
			var unavailable *owonscpi.ErrDMMFunctionUnavailable
			if !errors.As(err, &unavailable) {
				return _lastObservation, err
			}
			matches = 0
		} else {
			_lastObservation = observation
			if dmmFunctionSelectionsMatch(requested, *observation) {
				matches++
			} else {
				matches = 0
			}
			if matches >= 2 {
				if err := ctx.Err(); err != nil {
					return _lastObservation, err
				}
				return _lastObservation, nil
			}
		}
		if err := waitForDMMObservationPacing(ctx); err != nil {
			return _lastObservation, err
		}
	}
}

// dmmFunctionSelectionsMatch compares only the query context and function-specific AC/DC result.
//
// Example: a resistance token does not match a diode request, while a voltage/DC context matches voltage/DC.
func dmmFunctionSelectionsMatch(
	requested owonmodel.DMMFunctionSelection,
	observed owonmodel.DMMFunctionSelection,
) bool {
	if requested.Function != observed.Function {
		return false
	}
	if requested.CurrentType == nil {
		return observed.CurrentType == nil
	}
	return observed.CurrentType != nil && *requested.CurrentType == *observed.CurrentType
}

// waitForDMMObservationPacing delays the next query while remaining cancellable by the operation context.
//
// Example: cancellation during the 250ms load interval returns context.Canceled without another query.
func waitForDMMObservationPacing(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(dmmFunctionObservationPacing)
	defer timer.Stop()
	select {
	case <-timer.C:
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DMMMeasurement queries a scalar and timestamps its successfully interpreted observation.
//
// Example: malformed payloads produce no observation or capture timestamp.
func (controller *Controller) DMMMeasurement(ctx context.Context) (_result *owonmodel.DMMMeasurement, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.DMMMeasurement")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.DMMMeasurement: %v", _err) }()
	}

	session, err := controller.sessionForOperation("query DMM measurement")
	if err != nil {
		return nil, err
	}
	response, err := session.Execute(ctx, owonscpi.DMMMeasurementQuery())
	if err != nil {
		return nil, fmt.Errorf("query DMM measurement: %w", err)
	}
	measurement, err := owonscpi.ParseDMMMeasurement(response)
	if err != nil {
		return nil, err
	}
	measurement.CapturedAt = time.Now()
	return measurement, nil
}
