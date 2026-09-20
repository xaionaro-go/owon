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

// transactionBackend exposes native completion independently of context expiration.
//
// Example: A remains in Exchange while tests attempt transaction close or command B.
type transactionBackend struct {
	Release       chan struct{}
	Failure       error
	ReturnContext bool
	Commands      []string
	Deadlines     []time.Time
	Recoveries    []owonmodel.SerialNumber
	Closes        int
	releaseOnce   sync.Once
}

// Exchange records entry and keeps command A native-owned until explicit release.
//
// Example: a queued B must not appear in Commands until A has returned.
func (backend *transactionBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	deadline, _ := ctx.Deadline()
	backend.Deadlines = append(backend.Deadlines, deadline)
	if command.Text != "A" {
		return []byte(command.Text), nil
	}

	<-backend.Release
	if backend.ReturnContext {
		return nil, ctx.Err()
	}

	return []byte(command.Text), backend.Failure
}

// ReopenAndValidate records the serial that must be proven after ambiguous failure.
//
// Example: C recovers once after A fails; A itself is never replayed.
func (backend *transactionBackend) ReopenAndValidate(
	_ context.Context,
	serial owonmodel.SerialNumber,
) error {
	backend.Recoveries = append(backend.Recoveries, serial)
	return nil
}

// Close records native cleanup admission.
//
// Example: cleanup must remain absent while A owns an unfinished exchange.
func (backend *transactionBackend) Close(context.Context) error {
	backend.Closes++
	return nil
}

// release closes the native completion signal once, including after failed assertions.
//
// Example: deferred release prevents a failed ownership assertion from stranding A.
func (backend *transactionBackend) release() {
	backend.releaseOnce.Do(
		// signalNativeCompletion releases all observers of the first exchange.
		//
		// Example: the test can call release both explicitly and during deferred cleanup.
		func() { close(backend.Release) })
	synctest.Wait()
}

// transactionCommandRunner publishes one public transaction execution result.
//
// Example: a test checks queued cancellation without blocking its own assertion path.
type transactionCommandRunner struct {
	Transaction *Transaction
	Command     owonprotocol.Command
	Result      chan<- error
}

// Run executes the command with the caller's supplied cancellation and deadline.
//
// Example: the result is published only after native Exchange has returned.
func (runner transactionCommandRunner) Run(ctx context.Context) {
	_, err := runner.Transaction.Execute(ctx, runner.Command)
	runner.Result <- err
}

// startTransactionCommand launches a public execution and returns its completion result.
//
// Example: A and B share the same transaction while the test controls native progress.
func startTransactionCommand(
	ctx context.Context,
	transaction *Transaction,
	text string,
) <-chan error {
	result := make(chan error, 1)
	observability.Go(ctx, transactionCommandRunner{
		Transaction: transaction,
		Command:     owonprotocol.Command{Text: text, ResponseMode: owonprotocol.ResponseModeNone},
		Result:      result,
	}.Run)
	return result
}

// transactionCloseRunner exposes when public Close has relinquished ownership.
//
// Example: concurrent Close calls must all wait for the same native completion.
type transactionCloseRunner struct {
	Transaction *Transaction
	Done        chan<- struct{}
}

// Run closes the transaction and signals its synchronous completion.
//
// Example: Done cannot close while an admitted Exchange is still native-owned.
func (runner transactionCloseRunner) Run(context.Context) {
	runner.Transaction.Close()
	close(runner.Done)
}

// startTransactionClose begins synchronous closure without blocking the test driver.
//
// Example: the driver can release A after proving Close is still waiting.
func startTransactionClose(transaction *Transaction) <-chan struct{} {
	done := make(chan struct{})
	observability.Go(context.Background(), transactionCloseRunner{Transaction: transaction, Done: done}.Run)
	return done
}

// TestTransactionCloseRetainsActiveExchange excludes both queued and newly admitted I/O.
//
// Example: close requests reject queued B promptly but cannot admit a different transaction C.
func TestTransactionCloseRetainsActiveExchange(t *testing.T) {
	synctest.Test(t,
		// verifyCloseOwnership checks each side of native completion without a wall-clock wait.
		//
		// Example: repeated closes join A, then C enters exactly once.
		func(t *testing.T) {
			backend := &transactionBackend{Release: make(chan struct{})}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			b := startTransactionCommand(context.Background(), transaction, "B")
			synctest.Wait()
			firstClose := startTransactionClose(transaction)
			secondClose := startTransactionClose(transaction)
			synctest.Wait()

			c := make(chan error, 1)
			observability.Go(context.Background(), sessionExecuteRunner{Session: session, Command: owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone}, Started: make(chan struct{}, 1), Result: c}.Run)
			synctest.Wait()
			require.Equal(t, []string{"A"}, backend.Commands)
			require.Empty(t, a)
			require.Empty(t, c)
			require.Len(t, b, 1, "close must wake a queued execution before native completion")
			requireErrorType[*ErrUnavailable](t, <-b)
			select {
			case <-firstClose:
				require.FailNow(t, "Close returned before native completion")
			default:
			}
			select {
			case <-secondClose:
				require.FailNow(t, "concurrent Close returned before native completion")
			default:
			}

			backend.release()
			require.NoError(t, <-a)
			require.NoError(t, <-c)
			<-firstClose
			<-secondClose
			transaction.Close()
			require.Equal(t, []string{"A", "C"}, backend.Commands)
			require.Empty(t, backend.Recoveries)
			require.NoError(t, session.Close())
			require.Equal(t, 1, backend.Closes)
		})
}

// TestTransactionCloseExcludesBackendCleanup retains the outer lease through native return.
//
// Example: Session.Close waits alongside Transaction.Close without entering Backend.Close early.
func TestTransactionCloseExcludesBackendCleanup(t *testing.T) {
	synctest.Test(t,
		// verifyCleanupOwnership observes native cleanup before and after releasing A.
		//
		// Example: only one backend close occurs after both public close paths complete.
		func(t *testing.T) {
			backend := &transactionBackend{Release: make(chan struct{})}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			closed := startTransactionClose(transaction)
			cleanup := make(chan error, 1)
			observability.Go(context.Background(),
				// closeSession waits for transaction ownership before native cleanup.
				//
				// Example: its result cannot publish while A remains blocked.
				func(ctx context.Context) { cleanup <- session.CloseContext(ctx) })
			synctest.Wait()
			require.Zero(t, backend.Closes)
			require.Empty(t, cleanup)

			backend.release()
			require.NoError(t, <-a)
			<-closed
			require.NoError(t, <-cleanup)
			require.Equal(t, 1, backend.Closes)
			require.Equal(t, []string{"A"}, backend.Commands)
		})
}

// TestTransactionExecutionGatePreservesFreshBudget serializes commands without timing queued work.
//
// Example: B waits three device budgets for A yet receives a full new budget when admitted.
func TestTransactionExecutionGatePreservesFreshBudget(t *testing.T) {
	synctest.Test(t,
		// verifyGateBudget advances only the bubble clock while native work remains outstanding.
		//
		// Example: B's deadline is relative to B entry, not its invocation.
		func(t *testing.T) {
			backend := &transactionBackend{Release: make(chan struct{})}
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			b := startTransactionCommand(context.Background(), transaction, "B")
			synctest.Wait()
			time.Sleep(3 * time.Second)
			synctest.Wait()
			require.Equal(t, []string{"A"}, backend.Commands)
			require.Empty(t, a)
			require.Empty(t, b)
			require.False(t, session.poisoned)

			wantDeadline := time.Now().Add(time.Second)
			backend.release()
			require.NoError(t, <-a)
			require.NoError(t, <-b)
			require.Equal(t, []string{"A", "B"}, backend.Commands)
			require.Equal(t, wantDeadline, backend.Deadlines[1])
			require.Empty(t, backend.Recoveries)
		})
}

// TestTransactionQueuedCancellationDoesNotIssueOrPoison rejects canceled waiters promptly.
//
// Example: canceling B does not wait for A, write B, or require recovery before C.
func TestTransactionQueuedCancellationDoesNotIssueOrPoison(t *testing.T) {
	synctest.Test(t,
		// verifyQueuedCancellation observes cancellation independently of native completion.
		//
		// Example: the same transaction remains usable after the canceled waiter leaves.
		func(t *testing.T) {
			backend := &transactionBackend{Release: make(chan struct{})}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := startTransactionCommand(ctx, transaction, "B")
			synctest.Wait()
			cancel()
			synctest.Wait()
			require.Len(t, b, 1)
			require.ErrorIs(t, <-b, context.Canceled)
			require.Equal(t, []string{"A"}, backend.Commands)
			require.False(t, session.poisoned)
			require.Empty(t, a)

			backend.release()
			require.NoError(t, <-a)
			require.NoError(t, <-startTransactionCommand(context.Background(), transaction, "C"))
			require.Equal(t, []string{"A", "C"}, backend.Commands)
			require.Empty(t, backend.Recoveries)
		})
}

// TestTransactionFailureSuppressesQueuedExecution publishes failure before the gate is released.
//
// Example: B retains A's failure cause without I/O, and a new C transaction recovers once.
func TestTransactionFailureSuppressesQueuedExecution(t *testing.T) {
	synctest.Test(t,
		// verifyFailurePublication compares exact commands and recovery identity across the failure.
		//
		// Example: no replay of A and no late B write can reach the backend.
		func(t *testing.T) {
			cause := errors.New("ambiguous native exchange")
			backend := &transactionBackend{Release: make(chan struct{}), Failure: cause}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			b := startTransactionCommand(context.Background(), transaction, "B")
			synctest.Wait()
			require.Equal(t, []string{"A"}, backend.Commands)
			backend.release()
			require.ErrorIs(t, <-a, cause)
			require.ErrorIs(t, <-b, cause)
			require.Equal(t, []string{"A"}, backend.Commands)
			require.True(t, session.poisoned)

			transaction.Close()
			_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone})
			require.NoError(t, err)
			require.Equal(t, []string{"A", "C"}, backend.Commands)
			require.Equal(t, []owonmodel.SerialNumber{"serial"}, backend.Recoveries)
			require.False(t, session.poisoned)
		})
}

// TestTransactionCloseAfterDeadlineRetainsNativeOwnership forbids timeout-based detachment.
//
// Example: expired A still excludes C and recovery until its native callback returns.
func TestTransactionCloseAfterDeadlineRetainsNativeOwnership(t *testing.T) {
	synctest.Test(t,
		// verifyExpiredNativeOwnership separates deadline expiration from native completion.
		//
		// Example: releasing A publishes DeadlineExceeded before one validated recovery admits C.
		func(t *testing.T) {
			backend := &transactionBackend{Release: make(chan struct{}), ReturnContext: true}
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			defer backend.release()
			a := startTransactionCommand(context.Background(), transaction, "A")
			synctest.Wait()
			time.Sleep(3 * time.Second)
			closed := startTransactionClose(transaction)
			c := make(chan error, 1)
			observability.Go(context.Background(), sessionExecuteRunner{Session: session, Command: owonprotocol.Command{Text: "C", ResponseMode: owonprotocol.ResponseModeNone}, Started: make(chan struct{}, 1), Result: c}.Run)
			synctest.Wait()
			require.Equal(t, []string{"A"}, backend.Commands)
			require.Empty(t, backend.Recoveries)
			require.Empty(t, a)
			require.Empty(t, c)

			backend.release()
			require.ErrorIs(t, <-a, context.DeadlineExceeded)
			<-closed
			require.NoError(t, <-c)
			require.Equal(t, []string{"A", "C"}, backend.Commands)
			require.Equal(t, []owonmodel.SerialNumber{"serial"}, backend.Recoveries)
		})
}

// TestTransactionClosedAndUnconstructedValuesRejectIO keeps unusable transactions nonblocking.
//
// Example: nil, zero, and already-closed transactions return typed errors and tolerate repeated Close.
func TestTransactionClosedAndUnconstructedValuesRejectIO(t *testing.T) {
	command := owonprotocol.Command{Text: "B", ResponseMode: owonprotocol.ResponseModeNone}
	for _, transaction := range []*Transaction{nil, {}} {
		_, err := transaction.Execute(context.Background(), command)
		requireErrorType[*ErrUnavailable](t, err)
		transaction.Close()
		transaction.Close()
	}

	backend := new(synchronizedBackend)
	session, err := New(backend, Config{ExpectedSerial: "serial"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	transaction.Close()
	_, err = transaction.Execute(context.Background(), command)
	requireErrorType[*ErrUnavailable](t, err)
	require.Empty(t, backend.Commands())

	transaction, err = session.Begin(context.Background())
	require.NoError(t, err)
	_, err = transaction.Execute(context.Background(), command)
	require.NoError(t, err)
	transaction.Close()
	transaction.Close()
	_, err = transaction.Execute(context.Background(), command)
	requireErrorType[*ErrUnavailable](t, err)
	require.Equal(t, []string{"B"}, backend.Commands())
	require.NoError(t, session.Close())
}

// TestTransactionOperationContextUsesConfiguredBudgetAfterAdmission verifies
// the child deadline starts only after Begin succeeds and never extends a
// caller deadline.
//
// Example: a five-second caller receives a two-second session budget, while a
// one-second caller retains its earlier deadline.
func TestTransactionOperationContextUsesConfiguredBudgetAfterAdmission(t *testing.T) {
	synctest.Test(t,
		// verifyOperationContextDeadlines compares the configured and parent
		// deadline boundaries without real-time waiting.
		//
		// Example: OperationContext performs no backend exchange or admission.
		func(t *testing.T) {
			backend := new(transactionBackend)
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: 2 * time.Second})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()

			start := time.Now()
			parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer parentCancel()
			operationContext, cancel, err := transaction.OperationContext(parent)
			require.NoError(t, err)
			defer cancel()
			deadline, ok := operationContext.Deadline()
			require.True(t, ok)
			require.Equal(t, start.Add(2*time.Second), deadline)
			require.NoError(t, operationContext.Err())
			require.Empty(t, backend.Commands)

			shortParent, shortCancel := context.WithTimeout(context.Background(), time.Second)
			defer shortCancel()
			shortContext, shortOperationCancel, err := transaction.OperationContext(shortParent)
			require.NoError(t, err)
			defer shortOperationCancel()
			shortDeadline, ok := shortContext.Deadline()
			require.True(t, ok)
			require.Equal(t, start.Add(time.Second), shortDeadline)
		})
}

// TestTransactionOperationContextRejectsUnavailableInputs keeps the narrow
// context API fail-closed after ownership ends.
//
// Example: nil, zero, nil-parent, and closed transactions return typed
// session-unavailable errors rather than panicking or admitting I/O.
func TestTransactionOperationContextRejectsUnavailableInputs(t *testing.T) {
	for _, transaction := range []*Transaction{nil, {}} {
		_, _, err := transaction.OperationContext(context.Background())
		requireErrorType[*ErrUnavailable](t, err)
	}
	backend := new(transactionBackend)
	session, err := New(backend, Config{ExpectedSerial: "serial"})
	require.NoError(t, err)
	transaction, err := session.Begin(context.Background())
	require.NoError(t, err)
	_, _, err = transaction.OperationContext(nil)
	requireErrorType[*ErrUnavailable](t, err)
	transaction.Close()
	_, _, err = transaction.OperationContext(context.Background())
	requireErrorType[*ErrUnavailable](t, err)
	require.Empty(t, backend.Commands)
}
