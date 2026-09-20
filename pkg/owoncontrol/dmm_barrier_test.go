package owoncontrol

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// dmmSequenceBackend returns one deterministic response per query and records every exchange.
//
// Example: a delayed firmware transition is modeled by continuity, diode, diode replies.
type dmmSequenceBackend struct {
	Commands  []string
	Responses map[string][][]byte
}

// Exchange records one command and consumes its next scripted response.
//
// Example: no-response writes return an empty payload while ASCII queries consume queued tokens.
func (backend *dmmSequenceBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	responses := backend.Responses[command.Text]
	if len(responses) == 0 {
		return nil, nil
	}
	response := responses[0]
	backend.Responses[command.Text] = responses[1:]
	return append([]byte(nil), response...), nil
}

// ReopenAndValidate satisfies the session backend contract without changing the scripted device.
//
// Example: convergence tests do not model reconnect work.
func (*dmmSequenceBackend) ReopenAndValidate(context.Context, owonmodel.SerialNumber) error {
	return nil
}

// Close satisfies the session backend contract without external resources.
//
// Example: the in-memory command recorder needs no cleanup.
func (*dmmSequenceBackend) Close(context.Context) error {
	return nil
}

// TestSetDMMWaitsBeforeFollowingWritesAndVerifiesTheFinalFunction verifies both barriers.
//
// Example: REL/RANGE/AUTO are not sent until two diode observations, and success requires two final observations.
func TestSetDMMWaitsBeforeFollowingWritesAndVerifiesTheFinalFunction(t *testing.T) {
	synctest.Test(t,
		// verifyDMMBarrierOrdering advances virtual pacing time and checks every exchange boundary.
		//
		// Example: the final AUTO write is followed by exactly two function queries.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE?": {
					[]byte("CONTinuity"),
					[]byte("DIODe"),
					[]byte("DIODe"),
					[]byte("DIODe"),
					[]byte("DIODe"),
				},
			}}
			controller := newTestInstrument(t, backend)
			diode, enabled, rangeValue, autoRange := owonmodel.DMMFunctionDiode, true, owonmodel.DMMRangeV, true
			err := controller.SetDMM(t.Context(), &owonmodel.DMMPatch{
				Function:  &diode,
				Relative:  &enabled,
				Range:     &rangeValue,
				AutoRange: &autoRange,
			})
			require.NoError(t, err)
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:REL ON",
				":DMM:RANGE V",
				":DMM:AUTO ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.Commands)
		})
}

// TestSetDMMFunctionOnlyUsesOneInitialBarrier verifies a function-only patch does not perform a final duplicate barrier.
//
// Example: two matching observations complete a diode-only request with one CONFIGURE write.
func TestSetDMMFunctionOnlyUsesOneInitialBarrier(t *testing.T) {
	synctest.Test(t,
		// verifyFunctionOnlyBarrier checks that no final barrier is added without remaining writes.
		//
		// Example: a diode-only patch emits two queries after CONFIGURE and then returns.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE?": {[]byte("DIODe"), []byte("DIODe")},
			}}
			diode := owonmodel.DMMFunctionDiode
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &diode})
			require.NoError(t, err)
			require.Equal(t, []string{":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE?", ":DMM:CONFIGURE?"}, backend.Commands)
		})
}

// TestSetDMMVoltageBarrierRequiresMatchingCurrentType verifies AC/DC is part of the V/I query result comparison.
//
// Example: an AC observation is a valid nonmatch for a requested DC voltage and does not complete the barrier.
func TestSetDMMVoltageBarrierRequiresMatchingCurrentType(t *testing.T) {
	synctest.Test(t,
		// verifyVoltageCurrentTypeMatching advances virtual pacing through an AC mismatch and two DC matches.
		//
		// Example: the function-specific voltage query preserves the requested dimension while matching its observed type.
		func(t *testing.T) {
			backend := &dmmSequenceBackend{Responses: map[string][][]byte{
				":DMM:CONFIGURE:VOLTAGE?": {[]byte("AC"), []byte("DC"), []byte("DC")},
			}}
			voltage, direct := owonmodel.DMMFunctionVoltage, owonmodel.DMMCurrentTypeDC
			err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &voltage, CurrentType: &direct})
			require.NoError(t, err)
			require.Equal(t, []string{
				":DMM:CONFIGURE:VOLTAGE DC",
				":DMM:CONFIGURE:VOLTAGE?",
				":DMM:CONFIGURE:VOLTAGE?",
				":DMM:CONFIGURE:VOLTAGE?",
			}, backend.Commands)
		})
}

// TestSetDMMSettingsOnlyRetainsTransportAcknowledgement verifies settings-only patches do not add a readback query.
//
// Example: relative mode emits exactly its existing no-response write.
func TestSetDMMSettingsOnlyRetainsTransportAcknowledgement(t *testing.T) {
	backend := &dmmSequenceBackend{}
	relative := true
	err := newTestInstrument(t, backend).SetDMM(t.Context(), &owonmodel.DMMPatch{Relative: &relative})
	require.NoError(t, err)
	require.Equal(t, []string{":DMM:REL ON"}, backend.Commands)
}

// TestSetDMMSettingsOnlyRetainsPerExchangeBudget verifies settings-only patches do not acquire a whole-operation convergence deadline.
//
// Example: two six-second writes complete in twelve virtual seconds because each retains the existing per-exchange budget.
func TestSetDMMSettingsOnlyRetainsPerExchangeBudget(t *testing.T) {
	synctest.Test(t,
		// verifySettingsOnlyBudget keeps the legacy write-only operation outside the function barrier budget.
		//
		// Example: the two writes finish after the function-only ten-second cap would have expired.
		func(t *testing.T) {
			backend := &dmmSlowSettingsBackend{}
			controller := newTestInstrument(t, backend)
			relative, rangeValue := true, owonmodel.DMMRangeV
			require.NoError(t, controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Relative: &relative, Range: &rangeValue}))
			require.Equal(t, []string{":DMM:REL ON", ":DMM:RANGE V"}, backend.Commands)
		})
}

// dmmSlowSettingsBackend delays each write while respecting its per-exchange context.
//
// Example: a settings-only patch can span more than ten seconds without a convergence child context.
type dmmSlowSettingsBackend struct {
	Commands []string
}

// Exchange records one setting and waits six virtual seconds before acknowledging it.
//
// Example: an earlier caller deadline still cancels the exchange through ctx.Done.
func (backend *dmmSlowSettingsBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ReopenAndValidate satisfies the session backend contract for slow settings writes.
//
// Example: no recovery is needed when each delayed exchange completes.
func (*dmmSlowSettingsBackend) ReopenAndValidate(context.Context, owonmodel.SerialNumber) error {
	return nil
}

// Close satisfies the session backend contract without external resources.
//
// Example: virtual-time settings tests need no native cleanup.
func (*dmmSlowSettingsBackend) Close(context.Context) error {
	return nil
}

// TestSetDMMBarrierRepairsDelayedIntermediateTransition compares the legacy write-only path with the observed barrier.
//
// Example: a fast REL after CONFIGURE loses a pending diode transition, while the new path waits for two diode replies first.
func TestSetDMMBarrierRepairsDelayedIntermediateTransition(t *testing.T) {
	synctest.Test(t,
		// verifyLegacyAndBarrierOutcome models the captured old-to-intermediate-to-target sequence in virtual time.
		//
		// Example: the legacy operation returns after writes even though a subsequent query still observes continuity.
		func(t *testing.T) {
			diode, relative := owonmodel.DMMFunctionDiode, true
			patch := &owonmodel.DMMPatch{Function: &diode, Relative: &relative}

			legacyBackend := &dmmDelayedTargetBackend{}
			legacyController := newTestInstrument(t, legacyBackend)
			commands, err := owonscpi.CompileDMM(patch)
			require.NoError(t, err)
			require.NoError(t, legacyController.executeWrites(t.Context(), commands))
			query, err := owonscpi.DMMFunctionQuery(diode)
			require.NoError(t, err)
			response, err := legacyController.Execute(t.Context(), query)
			require.NoError(t, err)
			observation, err := owonscpi.ParseDMMFunction(diode, response)
			require.NoError(t, err)
			require.Equal(t, owonmodel.DMMFunctionContinuity, observation.Function)

			barrierBackend := &dmmDelayedTargetBackend{}
			barrierController := newTestInstrument(t, barrierBackend)
			require.NoError(t, barrierController.SetDMM(t.Context(), patch))
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:REL ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, barrierBackend.Commands)
		})
}

// TestSetDMMBarrierExcludesQueuedCallersUntilFinalObservation verifies the held transaction spans both barriers.
//
// Example: a queued second function patch cannot insert CONFIGURE between the first patch's REL and final queries.
func TestSetDMMBarrierExcludesQueuedCallersUntilFinalObservation(t *testing.T) {
	synctest.Test(t,
		// verifyQueuedCallerIsolation blocks the first final query while a second caller waits for admission.
		//
		// Example: releasing the first final observation lets the two complete operations appear as two contiguous command groups.
		func(t *testing.T) {
			backend := newDMMQueuedBackend()
			controller := newTestInstrument(t, backend)
			diode, relative := owonmodel.DMMFunctionDiode, true
			patch := &owonmodel.DMMPatch{Function: &diode, Relative: &relative}
			firstResult := make(chan error, 1)
			observability.Go(t.Context(),
				// runFirstDMMBarrier starts the owner operation whose final query is held.
				//
				// Example: the first operation retains session admission while waiting for release.
				func(ctx context.Context) { firstResult <- controller.SetDMM(ctx, patch) })
			<-backend.FirstConfigureEntered
			secondResult := make(chan error, 1)
			observability.Go(t.Context(),
				// runQueuedDMMBarrier starts the second owner operation behind admission.
				//
				// Example: its CONFIGURE cannot run until the first final query is released.
				func(ctx context.Context) { secondResult <- controller.SetDMM(ctx, patch) })
			<-backend.FirstFinalQueryEntered
			require.Equal(t, []string{
				":DMM:CONFIGURE DIODE",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
				":DMM:REL ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.CommandsSnapshot())
			select {
			case err := <-secondResult:
				t.Fatalf("queued DMM operation completed while the first transaction was held: %v", err)
			default:
			}
			close(backend.ReleaseFirstFinalQuery)
			require.NoError(t, <-firstResult)
			require.NoError(t, <-secondResult)
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
				":DMM:REL ON",
				":DMM:CONFIGURE?",
				":DMM:CONFIGURE?",
			}, backend.CommandsSnapshot())
		})
}

// dmmQueuedBackend blocks the first operation's final observation and returns diode for every query.
//
// Example: the second operation can record no command until the first operation releases its transaction.
type dmmQueuedBackend struct {
	mu                     sync.Mutex
	commands               []string
	queryCount             int
	firstFinalQueryBlocked bool
	FirstConfigureEntered  chan struct{}
	FirstFinalQueryEntered chan struct{}
	ReleaseFirstFinalQuery chan struct{}
}

// newDMMQueuedBackend constructs a channel-controlled transaction-isolation backend.
//
// Example: one final-query release controls when the next admission can start.
func newDMMQueuedBackend() *dmmQueuedBackend {
	return &dmmQueuedBackend{
		FirstConfigureEntered:  make(chan struct{}),
		FirstFinalQueryEntered: make(chan struct{}),
		ReleaseFirstFinalQuery: make(chan struct{}),
	}
}

// Exchange records every command and blocks only the first operation's second final query.
//
// Example: all function observations are diode, so each barrier completes after its second query.
func (backend *dmmQueuedBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	firstConfigure, block := backend.recordCommand(command.Text)
	if firstConfigure {
		close(backend.FirstConfigureEntered)
	}
	if !block {
		return []byte("DIODe"), nil
	}
	close(backend.FirstFinalQueryEntered)
	select {
	case <-backend.ReleaseFirstFinalQuery:
		return []byte("DIODe"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// recordCommand appends one command and decides whether its exchange is the first final barrier query.
//
// Example: the fourth function query is blocked while the first transaction remains admitted.
func (backend *dmmQueuedBackend) recordCommand(command string) (firstConfigure, block bool) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.commands = append(backend.commands, command)
	if command == ":DMM:CONFIGURE DIODE" && len(backend.commands) == 1 {
		firstConfigure = true
	}
	if command != ":DMM:CONFIGURE?" {
		return firstConfigure, false
	}
	backend.queryCount++
	if backend.queryCount == 4 && !backend.firstFinalQueryBlocked {
		backend.firstFinalQueryBlocked = true
		return firstConfigure, true
	}
	return firstConfigure, false
}

// ReopenAndValidate satisfies the session backend contract for queued-call isolation.
//
// Example: no recovery is needed when all exchanges are framed successfully.
func (*dmmQueuedBackend) ReopenAndValidate(context.Context, owonmodel.SerialNumber) error {
	return nil
}

// Close satisfies the session backend contract without external resources.
//
// Example: the queued transaction fixture needs no native cleanup.
func (*dmmQueuedBackend) Close(context.Context) error {
	return nil
}

// CommandsSnapshot returns a synchronized copy of all recorded commands.
//
// Example: assertions can inspect command order while the first exchange is blocked.
func (backend *dmmQueuedBackend) CommandsSnapshot() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]string(nil), backend.commands...)
}

// dmmDelayedTargetBackend models a transition lost when a setting write overlaps the initial function change.
//
// Example: the first query reports continuity, then two diode replies prove readiness for the guarded path.
type dmmDelayedTargetBackend struct {
	Commands        []string
	QueryCount      int
	TargetConfirmed bool
	TargetLost      bool
}

// Exchange applies the deterministic transition model and records native command order.
//
// Example: a REL write before target confirmation marks the requested diode transition lost.
func (backend *dmmDelayedTargetBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	switch command.Text {
	case ":DMM:CONFIGURE DIODE":
		backend.QueryCount = 0
		backend.TargetConfirmed = false
		backend.TargetLost = false
	case ":DMM:CONFIGURE?":
		if backend.TargetLost {
			return []byte("CONTinuity"), nil
		}
		backend.QueryCount++
		if backend.QueryCount == 1 {
			return []byte("CONTinuity"), nil
		}
		if backend.QueryCount == 3 {
			backend.TargetConfirmed = true
		}
		return []byte("DIODe"), nil
	case ":DMM:REL ON":
		if !backend.TargetConfirmed {
			backend.TargetLost = true
		}
	}
	return nil, nil
}

// ReopenAndValidate satisfies the session backend contract for the transition model.
//
// Example: the differential fixture does not need recovery.
func (*dmmDelayedTargetBackend) ReopenAndValidate(context.Context, owonmodel.SerialNumber) error {
	return nil
}

// Close satisfies the session backend contract without external resources.
//
// Example: virtual-time transition tests need no device cleanup.
func (*dmmDelayedTargetBackend) Close(context.Context) error {
	return nil
}
