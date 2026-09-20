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
	generatorObservationPacing          = 250 * time.Millisecond
	generatorMaximumContextObservations = 8
	// generatorMaximumOutputObservations bounds CHANNEL? retries for unavailable or changing replies.
	// Eight observations permit transient settling while preventing an observe operation from waiting forever.
	generatorMaximumOutputObservations = 8
)

// GeneratorConvergencePhase identifies the bounded phase in which an observed generator operation stopped.
//
// Example: GeneratorConvergencePhaseContext reports a malformed or nonconverging FUNCTION? barrier.
type GeneratorConvergencePhase uint8

const (
	// GeneratorConvergencePhaseUnknown identifies an unset convergence phase.
	//
	// Example: a zero-value diagnostic remains safe to format.
	GeneratorConvergencePhaseUnknown GeneratorConvergencePhase = iota
	// GeneratorConvergencePhaseAdmission identifies failure before transaction admission.
	//
	// Example: a canceled waiter reports admission without generator writes.
	GeneratorConvergencePhaseAdmission
	// GeneratorConvergencePhaseOutputPreflight identifies failure while preserving omitted output state.
	//
	// Example: malformed CHANNEL? data prevents a waveform write.
	GeneratorConvergencePhaseOutputPreflight
	// GeneratorConvergencePhaseWrite identifies a transport failure while sending a generator write.
	//
	// Example: an ambiguous FUNCTION exchange reports the write phase.
	GeneratorConvergencePhaseWrite
	// GeneratorConvergencePhaseContext identifies failure to observe the requested waveform twice.
	//
	// Example: an unknown FUNCTION? token blocks dependent writes.
	GeneratorConvergencePhaseContext
	// GeneratorConvergencePhaseRemainingWrites identifies a later setting-write failure.
	//
	// Example: a frequency exchange failure follows a verified waveform context.
	GeneratorConvergencePhaseRemainingWrites
	// GeneratorConvergencePhaseOutput identifies failure while applying or observing final output state.
	//
	// Example: a final CHANNEL? mismatch leaves logical output unverified.
	GeneratorConvergencePhaseOutput
)

// String returns a stable diagnostic label for one generator convergence phase.
//
// Example: GeneratorConvergencePhaseOutput.String returns "final output".
func (phase GeneratorConvergencePhase) String() string {
	switch phase {
	case GeneratorConvergencePhaseAdmission:
		return "admission"
	case GeneratorConvergencePhaseOutputPreflight:
		return "output preflight"
	case GeneratorConvergencePhaseWrite:
		return "waveform write"
	case GeneratorConvergencePhaseContext:
		return "waveform context"
	case GeneratorConvergencePhaseRemainingWrites:
		return "remaining writes"
	case GeneratorConvergencePhaseOutput:
		return "final output"
	default:
		return "unknown"
	}
}

// ErrGeneratorConvergence identifies an observed generator operation whose logical result is not fully known.
// It retains the last context/output observation, completed writes, compensation status, and cause.
//
// Example: errors.As extracts the requested waveform while errors.Is preserves a transport cancellation.
type ErrGeneratorConvergence struct {
	Phase           GeneratorConvergencePhase
	Context         owonmodel.GeneratorWaveformContext
	LastObservation *owonmodel.GeneratorObservation
	CompletedWrites []string
	Delivery        owonmodel.GeneratorDeliveryStatus
	Compensation    owonmodel.GeneratorOutputCompensation
	Cause           error
}

// ErrGeneratorObservationNonconvergence retains the historical control-package name for the model error.
//
// Example: controller callers can use errors.As without importing the model package for this diagnostic.
type ErrGeneratorObservationNonconvergence = owonmodel.ErrGeneratorObservationNonconvergence

// Error describes the failed phase without claiming electrical output behavior.
//
// Example: a failed context barrier names its last logical observation and partial-application status.
func (err *ErrGeneratorConvergence) Error() string {
	if err == nil {
		return "generator convergence failed"
	}
	message := "generator convergence failed"
	if err.Phase != GeneratorConvergencePhaseUnknown {
		message += " during " + err.Phase.String()
	}
	if err.Context.Requested != nil {
		message += fmt.Sprintf(" for waveform %d", *err.Context.Requested)
	}
	if err.LastObservation == nil {
		message += "; no valid observation"
	} else if err.LastObservation.Context.ObservedToken != "" {
		message += "; last waveform token " + err.LastObservation.Context.ObservedToken
	} else if err.LastObservation.Context.Reason != "" {
		message += "; waveform context unknown: " + err.LastObservation.Context.Reason
	} else if err.LastObservation.Output.Token != "" {
		message += "; last output token " + err.LastObservation.Output.Token
	} else {
		message += "; no valid observation"
	}
	if err.LastObservation != nil && err.LastObservation.Output.Status == owonmodel.GeneratorObservationUnknown {
		message += "; final logical output unknown"
	}
	if len(err.CompletedWrites) == 0 {
		message += "; no writes completed"
	} else if len(err.CompletedWrites) == 1 {
		message += "; 1 write completed"
	} else {
		message += fmt.Sprintf("; %d writes completed", len(err.CompletedWrites))
	}
	if err.Delivery == owonmodel.GeneratorDeliveryPartialOrUnknown {
		message += "; possible partial or unknown application"
	}
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	return message
}

// Unwrap preserves the parser, cancellation, or transport cause.
//
// Example: errors.Is still matches context.DeadlineExceeded beneath a convergence diagnostic.
func (err *ErrGeneratorConvergence) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// SetGeneratorObserved applies a generator patch with waveform convergence and operation-local output restoration.
// Numeric generator fields are transport-complete but remain readback-unverified until their query grammar is validated.
//
// Example: an omitted Output field is read before a waveform transition and restored as the final mutation.
func (controller *Controller) SetGeneratorObserved(
	ctx context.Context,
	request *owonmodel.GeneratorPatch,
) (_result *owonmodel.GeneratorOperationResult, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetGeneratorObserved")
		defer
		// traceResult records completion without exposing raw query payloads.
		//
		// Example: a failed operation retains its typed diagnostic fields for the caller.
		func() { logger.Tracef(ctx, "/Controller.SetGeneratorObserved: %v", _err) }()
	}

	commands, err := owonscpi.CompileGenerator(request)
	if err != nil {
		return nil, err
	}
	result := &owonmodel.GeneratorOperationResult{
		Delivery:     owonmodel.GeneratorDeliveryNotAttempted,
		Observation:  &owonmodel.GeneratorObservation{},
		Compensation: owonmodel.GeneratorOutputCompensation{Status: owonmodel.GeneratorOutputCompensationNotRequired},
	}
	if request.Waveform != nil {
		requested := *request.Waveform
		result.Observation.Context.Requested = &requested
		result.Observation.Context.Match = owonmodel.GeneratorContextUnknown
	}

	session, err := controller.sessionForOperation("set observed generator")
	if err != nil {
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseAdmission, result, err)
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseAdmission, result, err)
	}
	defer transaction.Close()
	operationContext, cancel, err := transaction.OperationContext(ctx)
	if err != nil {
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseAdmission, result, err)
	}
	defer cancel()

	writeCommands, outputCommand := owonscpi.SplitGeneratorOutputCommand(commands)
	var preservedOutput *bool
	if request.Waveform != nil && request.Output == nil {
		// A waveform transition has a restoration obligation even if preflight
		// cannot obtain a value. No compensation command is attempted in that case.
		result.Compensation.Status = owonmodel.GeneratorOutputCompensationNotAttempted
		outputObservation, observeErr := waitForGeneratorOutput(operationContext, transaction, nil)
		updateGeneratorOutputObservation(result, outputObservation)
		if observeErr != nil {
			return result, newGeneratorConvergenceError(GeneratorConvergencePhaseOutputPreflight, result, observeErr)
		}
		preservedOutput = cloneBool(outputObservation.Value)
		result.Compensation = owonmodel.GeneratorOutputCompensation{
			Requested: cloneBool(preservedOutput),
			Status:    owonmodel.GeneratorOutputCompensationUnverified,
		}
		compensationCommand := owonscpi.GeneratorOutputCommand(*preservedOutput)
		outputCommand = &compensationCommand
	}

	if request.Waveform != nil {
		if len(writeCommands) == 0 {
			return result, newGeneratorConvergenceError(GeneratorConvergencePhaseWrite, result, &owonmodel.ErrInvalidRequest{Reason: "compiled generator waveform write is missing"})
		}
		if err := executeGeneratorWrite(operationContext, transaction, writeCommands[0], result); err != nil {
			if preservedOutput != nil {
				result.Compensation.Status = owonmodel.GeneratorOutputCompensationNotAttempted
				markGeneratorOutputUnknown(result, &result.Observation.Output, "waveform write delivery is ambiguous")
			}
			return result, newGeneratorConvergenceError(GeneratorConvergencePhaseWrite, result, err)
		}
		writeCommands = writeCommands[1:]
		waveformObservation, observeErr := waitForGeneratorWaveform(operationContext, transaction, *request.Waveform)
		updateGeneratorContext(result, waveformObservation)
		if observeErr != nil {
			if preservedOutput != nil && generatorSemanticObservationError(observeErr) {
				compensationErr := compensateGeneratorOutput(operationContext, transaction, *preservedOutput, result)
				if compensationErr != nil {
					observeErr = errors.Join(observeErr, compensationErr)
				}
			} else if preservedOutput != nil {
				result.Compensation.Status = owonmodel.GeneratorOutputCompensationNotAttempted
				markGeneratorOutputUnknown(result, &result.Observation.Output, "waveform observation delivery is ambiguous")
			}
			return result, newGeneratorConvergenceError(GeneratorConvergencePhaseContext, result, observeErr)
		}
	}

	for _, command := range writeCommands {
		if err := executeGeneratorWrite(operationContext, transaction, command, result); err != nil {
			if preservedOutput != nil {
				result.Compensation.Status = owonmodel.GeneratorOutputCompensationNotAttempted
				markGeneratorOutputUnknown(result, &result.Observation.Output, "remaining generator write delivery is ambiguous")
			}
			return result, newGeneratorConvergenceError(GeneratorConvergencePhaseRemainingWrites, result, err)
		}
	}
	if outputCommand == nil {
		result.Delivery = owonmodel.GeneratorDeliveryTransportCompleteReadbackUnverified
		return result, nil
	}
	var requestedOutput bool
	switch {
	case request.Output != nil:
		requestedOutput = *request.Output
	case preservedOutput != nil:
		requestedOutput = *preservedOutput
	default:
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseOutput, result, &owonmodel.ErrInvalidRequest{Reason: "compiled generator output command has no target"})
	}
	result.Compensation.Requested = cloneBool(&requestedOutput)
	if err := executeGeneratorWrite(operationContext, transaction, *outputCommand, result); err != nil {
		// The command may have reached the device before the exchange failed;
		// retain an attempted-but-ambiguous output classification.
		result.Compensation.Status = owonmodel.GeneratorOutputCompensationFailed
		markGeneratorOutputUnknown(result, nil, "final logical output write delivery is ambiguous")
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseOutput, result, err)
	}
	if preservedOutput == nil || request.Output != nil {
		// Explicit output is verified only for waveform operations in this slice; ordinary output writes retain transport semantics.
		if request.Waveform == nil {
			result.Delivery = owonmodel.GeneratorDeliveryTransportCompleteReadbackUnverified
			return result, nil
		}
	}
	outputObservation, observeErr := waitForGeneratorOutput(operationContext, transaction, &requestedOutput)
	result.Compensation.Requested = cloneBool(&requestedOutput)
	if observeErr != nil {
		markGeneratorOutputUnknown(result, outputObservation, "final logical output observation did not converge")
		result.Compensation.Status = owonmodel.GeneratorOutputCompensationFailed
		return result, newGeneratorConvergenceError(GeneratorConvergencePhaseOutput, result, observeErr)
	}
	updateGeneratorOutputObservation(result, outputObservation)
	result.Compensation.Observed = cloneBool(outputObservation.Value)
	result.Compensation.Status = owonmodel.GeneratorOutputCompensationVerified
	result.Delivery = owonmodel.GeneratorDeliveryTransportCompleteReadbackVerified
	if generatorReadbackHasUnverifiedFields(request) {
		result.Delivery = owonmodel.GeneratorDeliveryTransportCompleteReadbackUnverified
	}
	return result, nil
}

// generatorReadbackHasUnverifiedFields reports fields whose validated query grammar is not in this slice.
//
// Example: a frequency write keeps transport-complete status but cannot claim numeric readback verification.
func generatorReadbackHasUnverifiedFields(request *owonmodel.GeneratorPatch) bool {
	if request == nil {
		return false
	}
	return request.FrequencyHz != nil || request.PeriodSeconds != nil || request.AmplitudeVolts != nil ||
		request.OffsetVolts != nil || request.HighVolts != nil || request.LowVolts != nil ||
		request.SymmetryPercent != nil || request.PulseWidthSeconds != nil || request.RisingSeconds != nil ||
		request.FallingSeconds != nil || request.DutyPercent != nil || request.Load != nil
}

// executeGeneratorWrite records only transport-completed generator mutations.
//
// Example: a failed exchange is omitted from CompletedWrites because its application is ambiguous.
func executeGeneratorWrite(
	ctx context.Context,
	executor commandExecutor,
	command owonprotocol.Command,
	result *owonmodel.GeneratorOperationResult,
) error {
	if _, err := executor.Execute(ctx, command); err != nil {
		return fmt.Errorf("apply command %q: %w", command.Text, err)
	}
	result.CompletedWrites = append(result.CompletedWrites, command.Text)
	return nil
}

// waitForGeneratorWaveform requires two consecutive matching typed waveform observations.
//
// Example: SQUARE, SINe, SINe delays dependent writes until the two SINe replies.
func waitForGeneratorWaveform(
	ctx context.Context,
	executor commandExecutor,
	requested owonmodel.GeneratorWaveform,
) (_lastObservation *owonscpiGeneratorWaveformObservation, _err error) {
	// The local alias keeps the helper's result private while allowing the controller to retain raw parser data.
	return waitForGeneratorWaveformObservation(ctx, executor, requested)
}

type owonscpiGeneratorWaveformObservation = owonmodel.GeneratorWaveformObservation

// waitForGeneratorWaveformObservation performs one bounded waveform readback loop.
//
// Example: an exact device `error` reply resets matching without being treated as malformed data.
func waitForGeneratorWaveformObservation(
	ctx context.Context,
	executor commandExecutor,
	requested owonmodel.GeneratorWaveform,
) (_lastObservation *owonmodel.GeneratorWaveformObservation, _err error) {
	matches := 0
	observations := 0
	for {
		response, err := executor.Execute(ctx, owonscpi.GeneratorFunctionQuery())
		if err != nil {
			return _lastObservation, fmt.Errorf("query generator function: %w", err)
		}
		observation, parseErr := owonscpi.ParseGeneratorFunction(response)
		if parseErr != nil {
			_lastObservation = observation
			var unavailable *owonscpi.ErrGeneratorObservationUnavailable
			if !errors.As(parseErr, &unavailable) {
				return _lastObservation, parseErr
			}
			matches = 0
			observations++
			if observations >= generatorMaximumContextObservations {
				return _lastObservation, &ErrGeneratorObservationNonconvergence{Query: owonscpi.GeneratorFunctionQuery().Text}
			}
		} else {
			_lastObservation = observation
			observations++
			if observation.Value != nil && *observation.Value == requested {
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
			if observations >= generatorMaximumContextObservations {
				return _lastObservation, &ErrGeneratorObservationNonconvergence{Query: owonscpi.GeneratorFunctionQuery().Text}
			}
		}
		if err := waitForGeneratorObservationPacing(ctx); err != nil {
			return _lastObservation, err
		}
	}
}

// waitForGeneratorOutput requires two consecutive logical output observations, optionally matching a target.
//
// Example: OFF, ON, ON converges to ON after resetting the first mismatch.
func waitForGeneratorOutput(
	ctx context.Context,
	executor commandExecutor,
	requested *bool,
) (_lastObservation *owonmodel.GeneratorOutputObservation, _err error) {
	matches := 0
	observations := 0
	var previous *bool
	for {
		response, err := executor.Execute(ctx, owonscpi.GeneratorChannelQuery())
		if err != nil {
			return _lastObservation, fmt.Errorf("query generator channel: %w", err)
		}
		observation, parseErr := owonscpi.ParseGeneratorChannel(response)
		if parseErr != nil {
			_lastObservation = observation
			var unavailable *owonscpi.ErrGeneratorObservationUnavailable
			if !errors.As(parseErr, &unavailable) {
				return _lastObservation, parseErr
			}
			matches = 0
			previous = nil
			observations++
			if observations >= generatorMaximumOutputObservations {
				return _lastObservation, &ErrGeneratorObservationNonconvergence{Query: owonscpi.GeneratorChannelQuery().Text}
			}
		} else if observation.Value != nil {
			_lastObservation = observation
			observations++
			if requested != nil && *observation.Value != *requested {
				matches = 0
			} else if previous != nil && *previous == *observation.Value {
				matches++
			} else {
				matches = 1
			}
			value := *observation.Value
			previous = &value
			if matches >= 2 {
				if err := ctx.Err(); err != nil {
					return _lastObservation, err
				}
				return _lastObservation, nil
			}
			if observations >= generatorMaximumOutputObservations {
				return _lastObservation, &ErrGeneratorObservationNonconvergence{Query: owonscpi.GeneratorChannelQuery().Text}
			}
		}
		if err := waitForGeneratorObservationPacing(ctx); err != nil {
			return _lastObservation, err
		}
	}
}

// waitForGeneratorObservationPacing delays retries without treating elapsed time as readiness.
//
// Example: a mismatch waits 250 milliseconds before the next query while honoring cancellation.
func waitForGeneratorObservationPacing(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(generatorObservationPacing)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// compensateGeneratorOutput performs one operation-local logical output restoration and barrier.
//
// Example: malformed context after FUNCTION restores the preflight OFF state before returning its primary error.
func compensateGeneratorOutput(
	ctx context.Context,
	executor commandExecutor,
	requested bool,
	result *owonmodel.GeneratorOperationResult,
) error {
	result.Compensation.Requested = cloneBool(&requested)
	if err := executeGeneratorWrite(ctx, executor, owonscpi.GeneratorOutputCommand(requested), result); err != nil {
		result.Compensation.Status = owonmodel.GeneratorOutputCompensationFailed
		markGeneratorOutputUnknown(result, nil, "compensating logical output write delivery is ambiguous")
		return fmt.Errorf("compensate generator output: %w", err)
	}
	observation, err := waitForGeneratorOutput(ctx, executor, &requested)
	if err != nil {
		markGeneratorOutputUnknown(result, observation, "compensated logical output observation did not converge")
		result.Compensation.Status = owonmodel.GeneratorOutputCompensationFailed
		return fmt.Errorf("verify compensated generator output: %w", err)
	}
	updateGeneratorOutputObservation(result, observation)
	result.Compensation.Observed = cloneBool(observation.Value)
	result.Compensation.Status = owonmodel.GeneratorOutputCompensationVerified
	return nil
}

// generatorSemanticObservationError limits compensation to parser/match failures on a healthy transaction.
//
// Example: an unknown FUNCTION? token gets one best-effort output restoration.
func generatorSemanticObservationError(err error) bool {
	var malformed *owonscpi.ErrMalformedResponse
	if errors.As(err, &malformed) {
		return true
	}
	var nonconvergence *ErrGeneratorObservationNonconvergence
	return errors.As(err, &nonconvergence)
}

// updateGeneratorContext copies one parser observation into the public operation result.
//
// Example: a malformed token is retained as ObservedToken with Unknown context status.
func updateGeneratorContext(result *owonmodel.GeneratorOperationResult, observation *owonmodel.GeneratorWaveformObservation) {
	if result == nil || result.Observation == nil || observation == nil {
		return
	}
	context := &result.Observation.Context
	context.Observed = cloneWaveform(observation.Value)
	context.ObservedToken = observation.Token
	context.ObservedRaw = observation.Raw
	context.Reason = observation.Reason
	context.Match = owonmodel.GeneratorContextUnknown
	if observation.Value != nil && context.Requested != nil {
		if *observation.Value == *context.Requested {
			context.Match = owonmodel.GeneratorContextMatch
		} else {
			context.Match = owonmodel.GeneratorContextMismatch
		}
	}
	result.Observation.CapturedAt = time.Now()
}

// updateGeneratorOutputObservation copies one parser observation into the public operation result.
//
// Example: final OFF readback is retained without claiming that a physical cable is inactive.
func updateGeneratorOutputObservation(result *owonmodel.GeneratorOperationResult, observation *owonmodel.GeneratorOutputObservation) {
	if result == nil || result.Observation == nil || observation == nil {
		return
	}
	result.Observation.Output = *observation
	result.Observation.Output.Value = cloneBool(observation.Value)
	result.Observation.CapturedAt = time.Now()
}

// markGeneratorOutputUnknown removes stale output certainty after a final write or verification failure.
//
// Example: a preflight OFF observation is not reused as the final state after an ambiguous CHANNEL write.
func markGeneratorOutputUnknown(
	result *owonmodel.GeneratorOperationResult,
	observation *owonmodel.GeneratorOutputObservation,
	reason string,
) {
	if result == nil || result.Observation == nil {
		return
	}
	if observation == nil {
		result.Observation.Output = owonmodel.GeneratorOutputObservation{}
	} else {
		updateGeneratorOutputObservation(result, observation)
	}
	result.Observation.Output.Status = owonmodel.GeneratorObservationUnknown
	result.Observation.Output.Value = nil
	result.Observation.Output.Reason = reason
	result.Observation.CapturedAt = time.Now()
}

// cloneBool returns independent ownership for an optional bool.
//
// Example: result compensation state cannot alias a caller's mutable patch field.
func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// cloneWaveform returns independent ownership for an optional waveform.
//
// Example: an observed waveform remains stable after parser-local storage is reused.
func cloneWaveform(value *owonmodel.GeneratorWaveform) *owonmodel.GeneratorWaveform {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// newGeneratorConvergenceError builds a public partial-application diagnostic from one result.
//
// Example: a failed FUNCTION exchange reports the completed writes and compensation outcome.
func newGeneratorConvergenceError(
	phase GeneratorConvergencePhase,
	result *owonmodel.GeneratorOperationResult,
	cause error,
) error {
	delivery := owonmodel.GeneratorDeliveryPartialOrUnknown
	if result != nil && len(result.CompletedWrites) == 0 && (phase == GeneratorConvergencePhaseAdmission || phase == GeneratorConvergencePhaseOutputPreflight) {
		delivery = owonmodel.GeneratorDeliveryNotAttempted
	}
	if result != nil {
		result.Delivery = delivery
	}
	diagnostic := &ErrGeneratorConvergence{Phase: phase, Delivery: delivery, Cause: cause}
	if result != nil {
		diagnostic.LastObservation = result.Observation
		diagnostic.Compensation = result.Compensation
		diagnostic.CompletedWrites = append([]string(nil), result.CompletedWrites...)
		if result.Observation != nil {
			diagnostic.Context = result.Observation.Context
		}
	}
	return diagnostic
}
