package owonsession

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestValidateCommandRejectsAmbiguousInput verifies that a raw request cannot
// inject another command or omit response framing.
//
// Example: `*IDN?;:RUN` is rejected before any USB write.
func TestValidateCommandRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()

	for _, command := range []owonprotocol.Command{
		{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeUnspecified},
		{Text: "*IDN?;:RUN", ResponseMode: owonprotocol.ResponseModeASCII},
		{Text: "*IDN?\n:RUN", ResponseMode: owonprotocol.ResponseModeASCII},
		{Text: "", ResponseMode: owonprotocol.ResponseModeNone},
	} {
		require.Error(t, owonprotocol.ValidateCommand(command))
	}
	require.NoError(t, owonprotocol.ValidateCommand(owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}))
}

// TestSessionRejectsAnUnconstructedValue verifies zero-value calls fail closed.
//
// Example: a manually declared Session cannot block forever on a nil admission channel.
func TestSessionRejectsAnUnconstructedValue(t *testing.T) {
	t.Parallel()

	var session Session
	_, err := session.Begin(context.Background())
	requireErrorType[*ErrUnavailable](t, err)
	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	requireErrorType[*ErrUnavailable](t, err)
	requireErrorType[*ErrUnavailable](t, session.CloseContext(context.Background()))
}

// TestNewRejectsTypedNilBackend verifies interface boundaries fail closed.
//
// Example: a nil backend pointer cannot be hidden inside Backend and panic later.
func TestNewRejectsTypedNilBackend(t *testing.T) {
	t.Parallel()

	var backend *synchronizedBackend
	_, err := New(backend, Config{ExpectedSerial: "25061855"})
	requireErrorType[*ErrUnavailable](t, err)
}

// sessionExecuteRunner owns one concurrent session execution and its result.
//
// Example: transaction tests start it while another operation holds admission.
type sessionExecuteRunner struct {
	Session *Session
	Command owonprotocol.Command
	Started chan<- struct{}
	Result  chan<- error
}

// Run reports entry, executes one command, and publishes its result.
//
// Example: the caller proves the command cannot reach the backend before transaction close.
func (runner sessionExecuteRunner) Run(ctx context.Context) {
	runner.Started <- struct{}{}
	_, err := runner.Session.Execute(ctx, runner.Command)
	runner.Result <- err
}

// synchronizedBackend serializes fixture state independently from session admission.
//
// Example: concurrent test assertions can inspect Commands without a data race.
type synchronizedBackend struct {
	mu             sync.Mutex
	commands       []string
	exchangeErrors []error
	reopenCalls    int
}

// Exchange records a command and returns the next configured failure.
//
// Example: a first timeout models a poisoned operation without replay.
func (backend *synchronizedBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.commands = append(backend.commands, command.Text)
	if len(backend.exchangeErrors) == 0 {
		return nil, nil
	}
	err := backend.exchangeErrors[0]
	backend.exchangeErrors = backend.exchangeErrors[1:]

	return nil, err
}

// ReopenAndValidate records recovery of the poisoned fixture session.
//
// Example: the first operation after a transaction failure increments ReopenCalls.
func (backend *synchronizedBackend) ReopenAndValidate(
	_ context.Context,
	_ owonmodel.SerialNumber,
) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.reopenCalls++

	return nil
}

// Close satisfies Backend without owning external resources.
//
// Example: transaction tests can close their session without cleanup side effects.
func (backend *synchronizedBackend) Close(_ context.Context) error {
	return nil
}

// Commands returns a race-free copy of recorded command text.
//
// Example: callers compare complete operation ordering after all runners finish.
func (backend *synchronizedBackend) Commands() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return append([]string(nil), backend.commands...)
}

// ReopenCalls returns the number of validated session recoveries.
//
// Example: one post-failure operation must observe exactly one recovery.
func (backend *synchronizedBackend) ReopenCalls() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return backend.reopenCalls
}

// TestSessionTransactionPreventsOperationInterleaving verifies one admission covers a batch.
//
// Example: A and C remain adjacent while concurrent B waits for transaction close.
func TestSessionTransactionPreventsOperationInterleaving(t *testing.T) {
	t.Parallel()

	backend := new(synchronizedBackend)
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "A", ResponseMode: owonprotocol.ResponseModeNone})
	require.NoError(t, err)
	started := make(chan struct{}, 1)
	result := make(chan error, 1)
	observability.Go(context.Background(), sessionExecuteRunner{
		Session: session, Command: owonprotocol.Command{Text: "B", ResponseMode: owonprotocol.ResponseModeNone}, Started: started, Result: result,
	}.Run)
	<-started
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone})
	require.NoError(t, err)
	require.Equal(t, []string{"A", "C"}, backend.Commands())
	transaction.Close()
	require.NoError(t, <-result)
	require.Equal(t, []string{"A", "C", "B"}, backend.Commands())
}

// TestSessionTransactionAdmissionCancellationDoesNoIO verifies canceled waiters stay outside.
//
// Example: a second operation timing out behind a held transaction records no command.
func TestSessionTransactionAdmissionCancellationDoesNoIO(t *testing.T) {
	t.Parallel()

	backend := new(synchronizedBackend)
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = session.Begin(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, backend.Commands())
	transaction.Close()
	transaction, err = session.Begin(context.Background())
	require.NoError(t, err)
	cancel()
	_, err = transaction.Execute(ctx, owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, backend.Commands())
	transaction.Close()
}

// TestSessionTransactionFailureStopsAndRecovers verifies fail-closed batch behavior.
//
// Example: command B is skipped after A fails and the next operation revalidates before C.
func TestSessionTransactionFailureStopsAndRecovers(t *testing.T) {
	t.Parallel()

	backend := &synchronizedBackend{exchangeErrors: []error{context.DeadlineExceeded}}
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "A", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "B", ResponseMode: owonprotocol.ResponseModeNone})
	require.Error(t, err)
	transaction.Close()
	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone})
	require.NoError(t, err)
	require.Equal(t, []string{"A", "C"}, backend.Commands())
	require.Equal(t, 1, backend.ReopenCalls())
}

// TestSessionAdmissionHonorsContext verifies waiting requests do not block past their deadline.
//
// Example: a second RPC times out while another transaction owns the USB session.
func TestSessionAdmissionHonorsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, verifySessionAdmissionDeadline)
}

// verifySessionAdmissionDeadline exercises queued admission under the simulated clock.
//
// Example: the deadline advances only after the blocked transaction waits for its admission token.
func verifySessionAdmissionDeadline(t *testing.T) {

	session, err := New(&scriptedBackend{}, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	session.admission <- struct{}{}
	defer releaseTestAdmission(session.admission)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err = session.Execute(ctx, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestSessionCloseContextHonorsCancellation verifies shutdown can be bounded by its caller.
//
// Example: a stuck in-flight USB exchange cannot indefinitely block service shutdown.
func TestSessionCloseContextHonorsCancellation(t *testing.T) {
	t.Parallel()

	session, err := New(&scriptedBackend{}, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	session.admission <- struct{}{}
	defer releaseTestAdmission(session.admission)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, session.CloseContext(ctx), context.Canceled)
	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.ErrorContains(t, err, "session is closed")
}

// TestSessionCloseContextBlocksRetainedTransaction verifies the close barrier reaches held transactions.
//
// Example: a timed-out close cannot let a retained transaction issue a later command.
func TestSessionCloseContextBlocksRetainedTransaction(t *testing.T) {
	t.Parallel()

	backend := new(synchronizedBackend)
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	closeContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	require.ErrorIs(t, session.CloseContext(closeContext), context.Canceled)

	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "after-close", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorContains(t, err, "session is closed")
	require.Empty(t, backend.Commands())
	transaction.Close()
	require.NoError(t, session.Close())
}

// TestSessionRetriesFailedBackendCleanup verifies retained native ownership remains reachable.
//
// Example: a transient close error is retried by the next Close call.
func TestSessionRetriesFailedBackendCleanup(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{CloseErrors: []error{errors.New("busy"), nil}}
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	require.ErrorContains(t, session.Close(), "busy")
	require.NoError(t, session.Close())
	require.Equal(t, 2, backend.CloseCalls)
}

// TestSessionCloseContextKeepsGenerationErrors verifies concurrent waiters retain their attempt result.
//
// Example: an old waiter reports err1 while a later retry reports err2.
func TestSessionCloseContextKeepsGenerationErrors(t *testing.T) {
	t.Parallel()
	synctest.Test(t, verifyCloseGenerationWaiters)
}

// verifyCloseGenerationWaiters proves joining by durable in-bubble waits, not a pre-call snapshot.
//
// Example: both joined callers receive the immutable first result before a retry receives the second.
func verifyCloseGenerationWaiters(t *testing.T) {

	err1 := errors.New("first close failed")
	err2 := errors.New("second close failed")
	backend := newStagedCloseBackend(err1, err2)
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	var runners sync.WaitGroup
	defer
	// releaseAndJoin prevents assertion failure from stranding any fixture generation.
	//
	// Example: closing the release channel unblocks every current and future cleanup attempt.
	func() {
		close(backend.release)
		runners.Wait()
		synctest.Wait()
	}()

	firstResults := make(chan error, 2)
	runners.Add(1)
	observability.Go(context.Background(),
		// waitForFirstClose publishes one result from the first close generation.
		//
		// Example: both initial callers receive the same first cleanup failure.
		func(context.Context) {
			defer runners.Done()
			firstResults <- session.CloseContext(context.Background())
		})
	<-backend.entered
	runners.Add(1)
	observability.Go(context.Background(),
		// waitForSecondClose enters the existing generation without an intervening test barrier.
		//
		// Example: synctest.Wait can establish that this caller is waiting inside CloseContext.
		func(context.Context) {
			defer runners.Done()
			firstResults <- session.CloseContext(context.Background())
		})
	synctest.Wait()
	require.Empty(t, firstResults)
	require.Equal(t, 1, backend.closeCallCount())
	backend.release <- struct{}{}
	firstError, joinedError := <-firstResults, <-firstResults
	require.ErrorIs(t, firstError, err1)
	require.ErrorIs(t, joinedError, err1)

	secondResult := make(chan error, 1)
	runners.Add(1)
	observability.Go(context.Background(),
		// waitForRetry publishes the result of the next close generation.
		//
		// Example: a second cleanup failure remains distinct from the first attempt.
		func(context.Context) {
			defer runners.Done()
			secondResult <- session.CloseContext(context.Background())
		})
	<-backend.entered
	backend.release <- struct{}{}

	require.ErrorIs(t, <-secondResult, err2)
	require.ErrorIs(t, firstError, err1)
	require.ErrorIs(t, joinedError, err1)
	require.NotErrorIs(t, firstError, err2)
	require.NotErrorIs(t, joinedError, err2)
	require.Equal(t, 2, backend.closeCallCount())
}

// releaseTestAdmission restores a session token held by a cancellation test.
//
// Example: a deferred call prevents one assertion failure from leaving the channel full.
func releaseTestAdmission(admission chan struct{}) {
	<-admission
}

// TestSessionPoisonsAmbiguousFailureWithoutReplay verifies that a failed
// exchange is not replayed and a later request must recover and re-identify.
//
// Example: a timeout after writing `:RUN` produces exactly one `:RUN` write.
func TestSessionPoisonsAmbiguousFailureWithoutReplay(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{ExchangeErrors: []error{context.DeadlineExceeded, nil}}
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)

	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{":RUN"}, backend.Commands)

	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.NoError(t, err)
	require.Equal(t, 1, backend.ReopenCalls)
	require.Equal(t, []string{":RUN", "*IDN?"}, backend.Commands)
	require.Equal(t, owonmodel.SerialNumber("25061855"), backend.ReopenedSerial)
}

// TestSessionPreservesPoisonWhenRecoveryFails verifies that no new command
// reaches a session that could not prove it is the expected instrument.
//
// Example: a serial mismatch prevents the pending identity query from writing.
func TestSessionPreservesPoisonWhenRecoveryFails(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{
		ExchangeErrors: []error{errors.New("ambiguous transfer")},
		ReopenError:    errors.New("serial mismatch"),
	}
	session, err := New(backend, Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)

	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	require.Error(t, err)
	_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.ErrorContains(t, err, "recover poisoned session")
	require.Equal(t, []string{":RUN"}, backend.Commands)
}

// scriptedBackend records session activity and returns deterministic results.
//
// Example: tests configure one timeout followed by one successful exchange.
type scriptedBackend struct {
	Commands       []string
	ExchangeErrors []error
	ReopenCalls    int
	ReopenedSerial owonmodel.SerialNumber
	ReopenError    error
	Responses      map[string][]byte
	CloseErrors    []error
	CloseCalls     int
}

// stagedCloseBackend blocks each close until its test releases the matching generation.
//
// Example: close waiters can be staged around two deterministic backend failures.
type stagedCloseBackend struct {
	mu          sync.Mutex
	closeErrors []error
	closeCalls  int
	entered     chan struct{}
	release     chan struct{}
}

// newStagedCloseBackend constructs a synchronized close-only test backend.
//
// Example: the first and second calls can return distinct errors without racing fixture state.
func newStagedCloseBackend(closeErrors ...error) *stagedCloseBackend {
	return &stagedCloseBackend{
		closeErrors: append([]error(nil), closeErrors...),
		entered:     make(chan struct{}, len(closeErrors)),
		release:     make(chan struct{}, len(closeErrors)),
	}
}

// Exchange satisfies Backend for close-generation tests without performing I/O.
//
// Example: only Close participates in this fixture's staged lifecycle.
func (backend *stagedCloseBackend) Exchange(
	context.Context,
	owonprotocol.Command,
) ([]byte, error) {
	return nil, nil
}

// ReopenAndValidate satisfies Backend for close-generation tests without reopening.
//
// Example: no transaction recovery is needed while testing cleanup generations.
func (backend *stagedCloseBackend) ReopenAndValidate(
	context.Context,
	owonmodel.SerialNumber,
) error {
	return nil
}

// Close blocks until released and returns the next configured generation error.
//
// Example: each close attempt has an explicit synchronization point for its waiter.
func (backend *stagedCloseBackend) Close(context.Context) error {
	err := backend.nextCloseError()
	backend.entered <- struct{}{}
	<-backend.release

	return err
}

// nextCloseError advances the scripted cleanup generation under the fixture mutex.
//
// Example: Close can block on release without holding the lock needed by closeCallCount.
func (backend *stagedCloseBackend) nextCloseError() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	index := backend.closeCalls
	backend.closeCalls++
	if index >= len(backend.closeErrors) {
		return nil
	}

	return backend.closeErrors[index]
}

// closeCallCount returns the number of synchronized backend cleanup attempts.
//
// Example: the regression proves one retry invokes Close exactly twice.
func (backend *stagedCloseBackend) closeCallCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return backend.closeCalls
}

// Exchange records one command and returns its scripted response.
//
// Example: a configured first error models an ambiguous USB transfer.
func (backend *scriptedBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	if len(backend.ExchangeErrors) > 0 {
		err := backend.ExchangeErrors[0]
		backend.ExchangeErrors = backend.ExchangeErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), backend.Responses[command.Text]...), nil
}

// ReopenAndValidate records the expected serial and returns its scripted error.
//
// Example: recovery tests assert that serial validation precedes a new write.
func (backend *scriptedBackend) ReopenAndValidate(
	_ context.Context,
	serial owonmodel.SerialNumber,
) error {
	backend.ReopenCalls++
	backend.ReopenedSerial = serial
	return backend.ReopenError
}

// Close satisfies Backend for deterministic tests.
//
// Example: no resources are owned by this in-memory backend.
func (backend *scriptedBackend) Close(_ context.Context) error {
	backend.CloseCalls++
	if len(backend.CloseErrors) == 0 {
		return nil
	}
	err := backend.CloseErrors[0]
	backend.CloseErrors = backend.CloseErrors[1:]

	return err
}
