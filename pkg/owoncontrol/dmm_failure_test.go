package owoncontrol

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestSetDMMResetsMatchesForUnavailableAndAlternatingObservations verifies only consecutive matches converge.
//
// Example: a transient `error` and an intervening continuity token both prevent an early diode write.
func TestSetDMMResetsMatchesForUnavailableAndAlternatingObservations(t *testing.T) {
	synctest.Test(t,
		// verifyConsecutiveObservationRules checks transient and nonmatching replies on virtual time.
		//
		// Example: an exact `error` reply resets the first matching streak.
		func(t *testing.T) {
			for _, testCase := range []struct {
				Name      string
				Responses [][]byte
				Queries   int
			}{
				{Name: "unavailable", Responses: [][]byte{[]byte("DIODe"), []byte("error"), []byte("DIODe"), []byte("DIODe")}, Queries: 4},
				{Name: "alternating", Responses: [][]byte{[]byte("DIODe"), []byte("CONTinuity"), []byte("DIODe"), []byte("DIODe")}, Queries: 4},
			} {
				backend := &dmmSequenceBackend{Responses: map[string][][]byte{":DMM:CONFIGURE?": testCase.Responses}}
				diode := owonmodel.DMMFunctionDiode
				require.NoError(t, newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode}), testCase.Name)
				require.Len(t, backend.Commands, testCase.Queries+1, testCase.Name)
			}
		})
}

// TestSetDMMStopsOnMalformedFunctionReply verifies malformed observations are terminal and no setting follows them.
//
// Example: a valid first diode match followed by an unknown token returns the parser classification with one completed write.
func TestSetDMMStopsOnMalformedFunctionReply(t *testing.T) {
	synctest.Test(t,
		// verifyMalformedFunctionReply checks terminal parser errors before remaining writes.
		//
		// Example: an unknown token leaves REL unapplied.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE?": {[]byte("DIODe"), []byte("bogus")},
			}}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			var malformed *owonscpi.ErrMalformedResponse
			require.ErrorAs(t, err, &convergence)
			require.ErrorAs(t, err, &malformed)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Equal(t, 1, convergence.SuccessfulTransportCompletedWriteCount)
			require.NotNil(t, convergence.LastValidObservation)
			require.ErrorContains(t, err, "partial or unknown")
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?"}, backend.Commands)
		})
}

// TestSetDMMRejectsGenericCurrentTypeObservationBeforeRemainingWrites keeps
// malformed device semantics out of convergence state.
//
// Example: a resistance query returning AC stops after CONFIGURE and one
// observation, without manufacturing a last valid resistance observation or
// sending REL.
func TestSetDMMRejectsGenericCurrentTypeObservationBeforeRemainingWrites(t *testing.T) {
	synctest.Test(t,
		// verifyGenericCurrentTypeFailure checks parser classification and command
		// ordering at the controller boundary.
		//
		// Example: AC is invalid for the generic resistance query.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE?": {[]byte("AC")},
			}}
			resistance, relative := owonmodel.DMMFunctionResistance, true
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{
				Function: &resistance,
				Relative: &relative,
			})
			var convergence *ErrDMMConvergence
			var malformed *owonscpi.ErrMalformedResponse
			require.ErrorAs(t, err, &convergence)
			require.ErrorAs(t, err, &malformed)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Nil(t, convergence.LastValidObservation)
			require.Equal(t, 1, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE RESISTANCE", ":DMM:CONFIGURE?"}, backend.Commands)
		})
}

// TestSetDMMFinalFailureRetainsTheLastValidObservation verifies diagnostics span both barriers.
//
// Example: a malformed post-write reply still reports the matching initial diode observation.
func TestSetDMMFinalFailureRetainsTheLastValidObservation(t *testing.T) {
	synctest.Test(t,
		// verifyFinalDiagnosticObservation checks that the initial barrier's valid reply remains visible.
		//
		// Example: a malformed final reply does not erase the last diode observation.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE?": {[]byte("DIODe"), []byte("DIODe"), []byte("bogus")},
			}}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			var malformed *owonscpi.ErrMalformedResponse
			require.ErrorAs(t, err, &convergence)
			require.ErrorAs(t, err, &malformed)
			require.Equal(t, DMMConvergencePhaseFinal, convergence.Phase)
			require.NotNil(t, convergence.LastValidObservation)
			require.Equal(t, owonmodel.DMMFunctionDiode, convergence.LastValidObservation.Function)
			require.ErrorContains(t, err, "last valid observation diode")
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?", ":DMM:REL ON", ":DMM:CONFIGURE?"}, backend.Commands)
		})
}

// TestSetDMMTransportFailureDoesNotReplayAndRecoversOnlyAtTheNextAdmission verifies quarantine boundaries.
//
// Example: a failed CONFIGURE is attempted once, while the next operation performs the only recovery before its own write.
func TestSetDMMTransportFailureDoesNotReplayAndRecoversOnlyAtTheNextAdmission(t *testing.T) {
	cause := errors.New("ambiguous transfer")
	backend := &scriptedBackend{
		ExchangeErrors: []error{cause},
		Responses:      map[string][]byte{":DMM:CONFIGURE?": []byte("DIODe")},
	}
	controller := newTestInstrument(t, backend)
	diode := owonmodel.DMMFunctionDiode
	err := controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode})
	var convergence *ErrDMMConvergence
	require.ErrorAs(t, err, &convergence)
	require.ErrorIs(t, err, cause)
	require.Equal(t, DMMConvergencePhaseConfigure, convergence.Phase)
	require.Zero(t, convergence.SuccessfulTransportCompletedWriteCount)
	require.ErrorContains(t, err, "CONFIGURE write")
	require.ErrorContains(t, err, "no valid observation")
	require.Equal(t, []string{":DMM:CONFIGURE DIODE"}, backend.Commands)
	require.Zero(t, backend.ReopenCalls)
	require.NoError(t, controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode}))
	require.Equal(t, []string{
		":DMM:CONFIGURE DIODE",
		":DMM:CONFIGURE DIODE",
		":DMM:CONFIGURE?",
		":DMM:CONFIGURE?",
	}, backend.Commands)
	require.Equal(t, 1, backend.ReopenCalls)
}

// cancelAfterFirstFunctionQueryBackend cancels its caller after one valid observation.
//
// Example: the controller's pacing wait observes cancellation without issuing a second query or remaining write.
type cancelAfterFirstFunctionQueryBackend struct {
	*dmmSequenceBackend
	Cancel  context.CancelFunc
	Queries int
}

// Exchange records one command and cancels after the first function observation.
//
// Example: a cancellable pacing test does not depend on a wall-clock deadline.
func (backend *cancelAfterFirstFunctionQueryBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	response, err := backend.dmmSequenceBackend.Exchange(ctx, command)
	if command.Text == ":DMM:CONFIGURE?" {
		backend.Queries++
		if backend.Queries == 1 {
			backend.Cancel()
		}
	}
	return response, err
}

// cancelAtFunctionQueryBackend cancels after a selected matching response has
// been received, before the barrier can linearize success.
//
// Example: cancellation after the second final diode reply must not issue a
// fifth query or poison the session.
type cancelAtFunctionQueryBackend struct {
	*dmmSequenceBackend
	Cancel   context.CancelFunc
	CancelAt int
	Queries  int
}

// Exchange records the matching reply and cancels the parent before return.
//
// Example: waitForDMMFunction observes the cancellation at its explicit
// success boundary rather than treating the reply as a completed barrier.
func (backend *cancelAtFunctionQueryBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	response, err := backend.dmmSequenceBackend.Exchange(ctx, command)
	if command.Text == ":DMM:CONFIGURE?" {
		backend.Queries++
		if backend.Queries == backend.CancelAt {
			backend.Cancel()
		}
	}
	return response, err
}

// TestSetDMMCancellationDuringPacingStopsBeforeTheNextQuery verifies operation cancellation propagation.
//
// Example: cancellation after one query returns context.Canceled and preserves the first write count.
func TestSetDMMCancellationDuringPacingStopsBeforeTheNextQuery(t *testing.T) {
	synctest.Test(t,
		// verifyPacingCancellation checks operation cancellation without a wall-clock wait.
		//
		// Example: one mismatch cancels the operation before its next observation.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &cancelAfterFirstFunctionQueryBackend{
				dmmSequenceBackend: &dmmSequenceBackend{Responses: map[string][][]byte{":DMM:CONFIGURE?": {[]byte("CONTinuity"), []byte("DIODe")}}},
				Cancel:             cancel,
			}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Equal(t, 1, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?"}, backend.Commands)
		})
}

// TestSetDMMCancellationBeforeSuccessLinearizationStopsWithoutReplay verifies
// cancellation immediately before the second-match success boundary.
//
// Example: the same admitted session remains usable for a fresh function-only
// request after the canceled operation returns.
func TestSetDMMCancellationBeforeSuccessLinearizationStopsWithoutReplay(t *testing.T) {
	synctest.Test(t,
		// verifySuccessLinearizationCancellation checks the exact first command
		// sequence, session poison state, and subsequent operation.
		//
		// Example: the canceled second observation is recorded once and no fifth
		// query is attempted.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &cancelAtFunctionQueryBackend{
				dmmSequenceBackend: &dmmSequenceBackend{Responses: map[string][][]byte{
					":DMM:CONFIGURE?": {
						[]byte("DIODe"), []byte("DIODe"), []byte("DIODe"),
						[]byte("DIODe"), []byte("DIODe"), []byte("DIODe"),
					},
				}},
				Cancel:   cancel,
				CancelAt: 4,
			}
			session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "25061855"})
			require.NoError(t, err)
			controller, err := New(session, owonmodel.DeviceIdentity{})
			require.NoError(t, err)
			diode := owonmodel.DMMFunctionDiode
			patch := &owonmodel.DMMPatch{Function: &diode}
			relative := true
			err = controller.SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, DMMConvergencePhaseFinal, convergence.Phase)
			require.Equal(t, 2, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:REL ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.Commands)

			require.NoError(t, controller.SetDMM(t.Context(), patch))
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:REL ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.Commands)
		})
}

// dmmTwoMatchExecutor supplies two matching observations without transport.
//
// Example: the linearization test can isolate the final context check from
// session admission and native exchange behavior.
type dmmTwoMatchExecutor struct {
	Calls int
}

// Execute returns one diode token per deterministic query.
//
// Example: exactly two calls are sufficient for a successful barrier.
func (executor *dmmTwoMatchExecutor) Execute(context.Context, owonprotocol.Command) ([]byte, error) {
	executor.Calls++
	return []byte("DIODe"), nil
}

// dmmAfterLinearizationContext lets a test cancel after the explicit Err check
// has returned nil but before waitForDMMFunction returns.
//
// Example: cancellation after the check must preserve the successful barrier.
type dmmAfterLinearizationContext struct {
	context.Context
	CheckStarted  chan struct{}
	AllowCheck    chan struct{}
	CheckReturned chan struct{}
	ErrCalls      int
}

// Err blocks the one success-boundary check until the test permits it.
//
// Example: the parent is canceled only after this check has returned nil.
func (ctx *dmmAfterLinearizationContext) Err() error {
	ctx.ErrCalls++
	if ctx.ErrCalls < 3 {
		return ctx.Context.Err()
	}
	select {
	case <-ctx.CheckStarted:
		return ctx.Context.Err()
	default:
		close(ctx.CheckStarted)
		<-ctx.AllowCheck
		close(ctx.CheckReturned)
		return nil
	}
}

// TestWaitForDMMFunctionCancellationAfterSuccessLinearizationKeepsSuccess
// verifies the after-check side of the terminal cancellation boundary.
//
// Example: cancellation after Err returns nil does not turn two matching
// observations into a failed convergence.
func TestWaitForDMMFunctionCancellationAfterSuccessLinearizationKeepsSuccess(t *testing.T) {
	synctest.Test(t,
		func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := &dmmAfterLinearizationContext{
				Context:       parent,
				CheckStarted:  make(chan struct{}),
				AllowCheck:    make(chan struct{}),
				CheckReturned: make(chan struct{}),
			}
			executor := new(dmmTwoMatchExecutor)
			result := make(chan error, 1)
			go func() {
				_, err := waitForDMMFunction(ctx, executor, owonprotocol.Command{Text: ":DMM:CONFIGURE?", ResponseMode: owonprotocol.ResponseModeASCII}, owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionDiode})
				result <- err
			}()
			synctest.Wait()
			time.Sleep(dmmFunctionObservationPacing)
			synctest.Wait()
			<-ctx.CheckStarted
			close(ctx.AllowCheck)
			<-ctx.CheckReturned
			cancel()
			require.NoError(t, <-result)
			require.Equal(t, 2, executor.Calls)
		})
}

// cancelAfterDMMCommandBackend cancels after one selected exchange completes.
//
// Example: selecting REL cancels before the controller can start final verification.
type cancelAfterDMMCommandBackend struct {
	*dmmSequenceBackend
	Cancel        context.CancelFunc
	CancelCommand string
}

// Exchange records the selected command and cancels before the next controller phase.
//
// Example: the completed REL write remains counted while no post-cancel query is sent.
func (backend *cancelAfterDMMCommandBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	response, err := backend.dmmSequenceBackend.Exchange(ctx, command)
	if command.Text == backend.CancelCommand {
		backend.Cancel()
	}
	return response, err
}

// TestSetDMMCancellationBeforeFinalBarrierStopsFurtherCommands verifies cancellation after a successful remaining write.
//
// Example: the final phase preserves two completed writes and does not replay REL or issue a query after cancellation.
func TestSetDMMCancellationBeforeFinalBarrierStopsFurtherCommands(t *testing.T) {
	synctest.Test(t,
		// verifyFinalCancellation checks the operation boundary immediately after the remaining setting write.
		//
		// Example: cancellation cannot turn a transport-completed write into a claimed final observation.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &cancelAfterDMMCommandBackend{
				dmmSequenceBackend: &dmmSequenceBackend{Responses: map[string][][]byte{":DMM:CONFIGURE?": {[]byte("DIODe"), []byte("DIODe")}}},
				Cancel:             cancel,
				CancelCommand:      ":DMM:REL ON",
			}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, DMMConvergencePhaseFinal, convergence.Phase)
			require.Equal(t, 2, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?", ":DMM:REL ON"}, backend.Commands)
		})
}

// TestSetDMMCancellationAfterConfigureStopsBeforeInitialObservation verifies a canceled CONFIGURE phase.
//
// Example: a transport-completed CONFIGURE is reported with one completed write and no follow-up query.
func TestSetDMMCancellationAfterConfigureStopsBeforeInitialObservation(t *testing.T) {
	synctest.Test(t,
		// verifyConfigureCancellation cancels immediately after the function write.
		//
		// Example: the initial barrier reports cancellation without replaying CONFIGURE.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &cancelAfterDMMCommandBackend{
				dmmSequenceBackend: &dmmSequenceBackend{},
				Cancel:             cancel,
				CancelCommand:      ":DMM:CONFIGURE DIODE",
			}
			diode := owonmodel.DMMFunctionDiode
			err := newTestInstrument(t, backend).SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Equal(t, 1, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE"}, backend.Commands)
		})
}

// TestSetDMMCancellationBetweenRemainingWritesStopsBeforeNextWrite verifies cancellation in the remaining-write phase.
//
// Example: a canceled REL completion prevents RANGE and final verification from being sent.
func TestSetDMMCancellationBetweenRemainingWritesStopsBeforeNextWrite(t *testing.T) {
	synctest.Test(t,
		// verifyRemainingWriteCancellation cancels after REL while RANGE is still compiled but unsent.
		//
		// Example: the completed count is two and the failed phase identifies remaining writes.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &cancelAfterDMMCommandBackend{
				dmmSequenceBackend: &dmmSequenceBackend{Responses: map[string][][]byte{":DMM:CONFIGURE?": {[]byte("DIODe"), []byte("DIODe")}}},
				Cancel:             cancel,
				CancelCommand:      ":DMM:REL ON",
			}
			diode, relative, rangeValue := owonmodel.DMMFunctionDiode, true, owonmodel.DMMRangeV
			err := newTestInstrument(t, backend).SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode, Relative: &relative, Range: &rangeValue})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, DMMConvergencePhaseWrite, convergence.Phase)
			require.Equal(t, 2, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?", ":DMM:REL ON"}, backend.Commands)
		})
}

// dmmRepeatingFunctionBackend returns one semantic function before remaining writes and another afterward.
//
// Example: a permanently wrong function keeps either convergence barrier pending until the operation budget expires.
type dmmRepeatingFunctionBackend struct {
	Commands        []string
	InitialReply    []byte
	AfterWriteReply []byte
	SettingWritten  bool
}

// Exchange records commands and returns a phase-specific function token.
//
// Example: initial continuity and post-write continuity are valid nonmatches for a diode request.
func (backend *dmmRepeatingFunctionBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	switch command.Text {
	case ":DMM:CONFIGURE DIODE":
		return nil, nil
	case ":DMM:CONFIGURE?":
		if backend.SettingWritten {
			return append([]byte(nil), backend.AfterWriteReply...), nil
		}
		return append([]byte(nil), backend.InitialReply...), nil
	default:
		backend.SettingWritten = true
		return nil, nil
	}
}

// ReopenAndValidate satisfies the session backend contract for budget tests.
//
// Example: no recovery is needed when semantic replies are fully framed.
func (*dmmRepeatingFunctionBackend) ReopenAndValidate(context.Context, owonmodel.SerialNumber) error {
	return nil
}

// Close satisfies the session backend contract without external resources.
//
// Example: the virtual-time budget fixture needs no native cleanup.
func (*dmmRepeatingFunctionBackend) Close(context.Context) error {
	return nil
}

// TestSetDMMInitialBarrierHonorsWholeOperationBudget verifies the ten-second cap without a caller deadline.
//
// Example: endless valid nonmatches stop at the initial phase instead of spinning forever or sending settings.
func TestSetDMMInitialBarrierHonorsWholeOperationBudget(t *testing.T) {
	synctest.Test(t,
		// verifyInitialBudget expires the cooperative child context in virtual time.
		//
		// Example: the CONFIGURE write is counted, but REL never follows failed convergence.
		func(t *testing.T) {
			backend := &dmmRepeatingFunctionBackend{InitialReply: []byte("CONTinuity"), AfterWriteReply: []byte("CONTinuity")}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Equal(t, 1, convergence.SuccessfulTransportCompletedWriteCount)
			require.NotContains(t, backend.Commands, ":DMM:REL ON")
		})
}

// TestSetDMMUsesConfiguredOperationTimeoutAfterAdmission verifies the session
// budget, rather than a controller constant, bounds convergence work.
//
// Example: a 900ms configured budget issues three paced observations and
// never consumes the historical ten-second controller cap.
func TestSetDMMUsesConfiguredOperationTimeoutAfterAdmission(t *testing.T) {
	synctest.Test(t,
		// verifyConfiguredDMMBudget records the exact observation count at the
		// configured deadline.
		//
		// Example: the caller remains unbounded while admission succeeds first.
		func(t *testing.T) {
			backend := &dmmRepeatingFunctionBackend{InitialReply: []byte("CONTinuity")}
			session, err := owonsession.New(backend, owonsession.Config{
				ExpectedSerial:   "25061855",
				OperationTimeout: 900 * time.Millisecond,
			})
			require.NoError(t, err)
			controller, err := New(session, owonmodel.DeviceIdentity{})
			require.NoError(t, err)
			diode := owonmodel.DMMFunctionDiode
			err = controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, DMMConvergencePhaseInitial, convergence.Phase)
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.Commands)
		})
}

// TestSetDMMFinalBarrierHonorsWholeOperationBudget verifies the ten-second cap after remaining writes.
//
// Example: REL completes before an endless final nonmatch, but no later write or success timestamp is possible.
func TestSetDMMFinalBarrierHonorsWholeOperationBudget(t *testing.T) {
	synctest.Test(t,
		// verifyFinalBudget expires the final barrier in virtual time after the remaining write.
		//
		// Example: the public phase and count distinguish a post-write convergence failure.
		func(t *testing.T) {
			backend := &dmmRepeatingFunctionBackend{InitialReply: []byte("DIODe"), AfterWriteReply: []byte("CONTinuity")}
			diode, relative := owonmodel.DMMFunctionDiode, true
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode, Relative: &relative})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, DMMConvergencePhaseFinal, convergence.Phase)
			require.Equal(t, 2, convergence.SuccessfulTransportCompletedWriteCount)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?", ":DMM:REL ON"}, backend.Commands[:4])
		})
}

// TestSetDMMRemainingWriteFailureStopsFollowingWrites verifies completed-write accounting and no replay.
//
// Example: a RANGE transport failure leaves AUTO and the final barrier unsent while preserving its cause.
func TestSetDMMRemainingWriteFailureStopsFollowingWrites(t *testing.T) {
	cause := errors.New("range transfer ambiguous")
	backend := &scriptedBackend{
		ExchangeErrors: []error{nil, nil, nil, nil, cause},
		Responses:      map[string][]byte{":DMM:CONFIGURE?": []byte("DIODe")},
	}
	diode, relative, rangeValue := owonmodel.DMMFunctionDiode, true, owonmodel.DMMRangeV
	err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode, Relative: &relative, Range: &rangeValue})
	var convergence *ErrDMMConvergence
	require.ErrorAs(t, err, &convergence)
	require.ErrorIs(t, err, cause)
	require.Equal(t, DMMConvergencePhaseWrite, convergence.Phase)
	require.Equal(t, 2, convergence.SuccessfulTransportCompletedWriteCount)
	require.ErrorContains(t, err, "possible partial or unknown application")
	require.Equal(t, []string{
		":DMM:CONFIGURE DIODE",
		":DMM:CONFIGURE?",
		":DMM:CONFIGURE?",
		":DMM:REL ON",
		":DMM:RANGE V",
	}, backend.Commands)
}

// TestSetDMMAdmissionDeadlineDoesNotWrite verifies the operation cap starts before session admission.
//
// Example: a held unrelated transaction makes a short-deadline function request fail without a CONFIGURE exchange.
func TestSetDMMAdmissionDeadlineDoesNotWrite(t *testing.T) {
	synctest.Test(t,
		// verifyAdmissionBudget starts a virtual deadline while another transaction owns the session.
		//
		// Example: admission expiry produces no CONFIGURE exchange and no DMM write count.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{}
			session, err := owonsession.New(
				backend,
				owonsession.Config{ExpectedSerial: "25061855"},
			)
			require.NoError(t, err)
			controller, err := New(session, owonmodel.DeviceIdentity{})
			require.NoError(t, err)
			held, err := session.Begin(t.Context())
			require.NoError(t, err)
			defer held.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
			defer cancel()
			diode := owonmodel.DMMFunctionDiode
			err = controller.SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, DMMConvergencePhaseAdmission, convergence.Phase)
			require.Zero(t, convergence.SuccessfulTransportCompletedWriteCount)
			require.ErrorContains(t, err, "no writes were attempted")
			require.Empty(t, backend.Commands)
		})
}

// TestSetDMMSessionAdmissionUsesCallerContext verifies admission remains
// governed by the caller context rather than the configured device budget.
//
// Example: an indefinitely held session expires a function request without
// issuing CONFIGURE under virtual time.
func TestSetDMMSessionAdmissionUsesCallerContext(t *testing.T) {
	synctest.Test(t,
		// verifyCallerAdmissionBudget waits for the caller deadline rather than a
		// post-admission device deadline.
		//
		// Example: admission failure remains distinct from any partial device application.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{}
			session, err := owonsession.New(
				backend,
				owonsession.Config{ExpectedSerial: "25061855"},
			)
			require.NoError(t, err)
			controller, err := New(session, owonmodel.DeviceIdentity{})
			require.NoError(t, err)
			held, err := session.Begin(t.Context())
			require.NoError(t, err)
			defer held.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
			defer cancel()
			diode := owonmodel.DMMFunctionDiode
			err = controller.SetDMM(ctx, &owonmodel.DMMPatch{Function: &diode})
			var convergence *ErrDMMConvergence
			require.ErrorAs(t, err, &convergence)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, DMMConvergencePhaseAdmission, convergence.Phase)
			require.Zero(t, convergence.SuccessfulTransportCompletedWriteCount)
			require.Empty(t, backend.Commands)
		})
}

// TestSetDMMNilContextFailsBeforeAdmission verifies nil contexts never reach a child-context constructor or device exchange.
//
// Example: a function request retains the public convergence diagnostic while reporting no attempted DMM writes.
func TestSetDMMNilContextFailsBeforeAdmission(t *testing.T) {
	backend := &dmmSequenceBackend{}
	diode := owonmodel.DMMFunctionDiode
	err := newTestInstrument(t, backend).SetDMM(nil, &owonmodel.DMMPatch{Function: &diode})
	var convergence *ErrDMMConvergence
	var unavailable *owonsession.ErrUnavailable
	require.ErrorAs(t, err, &convergence)
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, DMMConvergencePhaseAdmission, convergence.Phase)
	require.Zero(t, convergence.SuccessfulTransportCompletedWriteCount)
	require.ErrorContains(t, err, "no writes were attempted")
	require.Empty(t, backend.Commands)
}
