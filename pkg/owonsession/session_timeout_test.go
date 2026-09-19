package owonsession

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// deadlineBackend records the actual I/O deadline and can wait for its expiry.
//
// Example: missing deadlines return immediately so baseline regressions cannot hang.
type deadlineBackend struct {
	Deadline         time.Time
	RecoveryDeadline time.Time
	Wait             bool
	RecoveryWait     bool
	Calls            int
	Recoveries       int
}

// TestConfiguredBudget verifies normalization, override and earlier caller deadlines.
//
// Example: a one-second caller cannot be extended by a longer device budget.
func TestConfiguredBudget(t *testing.T) {
	synctest.Test(t,
		// verifyConfiguredBudgets compares actual exchange deadlines for each public configuration.
		//
		// Example: zero selects the same policy as an explicitly configured default.
		func(t *testing.T) {
			for _, test := range []struct {
				Timeout time.Duration
				Parent  time.Duration
				Want    time.Duration
			}{
				{Want: DefaultDeviceOperationTimeout},
				{Timeout: 2 * time.Second, Want: 2 * time.Second},
				{Timeout: time.Minute, Parent: time.Second, Want: time.Second},
			} {
				backend := new(deadlineBackend)
				session, err := New(backend, Config{ExpectedSerial: " serial ", OperationTimeout: test.Timeout})
				require.NoError(t, err)
				ctx := context.Background()
				if test.Parent != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.Parent)
					defer cancel()
				}
				start := time.Now()
				_, err = session.Execute(ctx, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
				require.NoError(t, err)
				require.Equal(t, start.Add(test.Want), backend.Deadline)
				require.Equal(t, owonmodel.SerialNumber("serial"), session.config.ExpectedSerial)
			}
			_, err := New(new(deadlineBackend), Config{ExpectedSerial: "serial", OperationTimeout: -time.Second})
			requireErrorType[*ErrInvalidConfig](t, err)
		})
}

// TestSessionAdmissionDoesNotSpendDeviceBudget verifies observable waiting has no device timer.
//
// Example: after waiting three budgets, an admitted command still gets its complete own budget.
func TestSessionAdmissionDoesNotSpendDeviceBudget(t *testing.T) {
	synctest.Test(t,
		// verifyAdmissionBudget holds admission while the other caller durably waits.
		//
		// Example: no backend call or poison occurs during the queued interval.
		func(t *testing.T) {
			backend := new(deadlineBackend)
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			result := make(chan error, 1)
			observability.Go(context.Background(), sessionExecuteRunner{Session: session, Command: owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}, Started: make(chan struct{}, 1), Result: result}.Run)
			synctest.Wait()
			time.Sleep(3 * time.Second)
			require.Zero(t, backend.Calls)
			require.Empty(t, result)
			require.False(t, session.poisoned)
			start := time.Now()
			transaction.Close()
			require.NoError(t, <-result)
			require.Equal(t, start.Add(time.Second), backend.Deadline)
		})
}

// TestTransactionCommandsReceiveSeparateBudgets keeps healthy multi-command work unconstrained by a batch timer.
//
// Example: two commands can span more than one operation budget while each completes on time.
func TestTransactionCommandsReceiveSeparateBudgets(t *testing.T) {
	synctest.Test(t,
		// verifyPerCommandBudget delays between two commands while retaining transaction ownership.
		//
		// Example: the second command's deadline is relative to its own entry, not Begin.
		func(t *testing.T) {
			backend := new(deadlineBackend)
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
			require.NoError(t, err)
			transaction, err := session.Begin(context.Background())
			require.NoError(t, err)
			defer transaction.Close()
			command := owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}
			_, err = transaction.Execute(context.Background(), command)
			require.NoError(t, err)
			first := backend.Deadline
			time.Sleep(2 * time.Second)
			_, err = transaction.Execute(context.Background(), command)
			require.NoError(t, err)
			require.Equal(t, first.Add(2*time.Second), backend.Deadline)
			require.Equal(t, 2, backend.Calls)
			require.Zero(t, backend.Recoveries)
		})
}

// Exchange observes the complete command budget and returns cancellation only after entry.
//
// Example: a default background caller receives a finite deadline at the backend boundary.
func (backend *deadlineBackend) Exchange(
	ctx context.Context,
	_ owonprotocol.Command,
) ([]byte, error) {
	backend.Calls++
	deadline, exists := ctx.Deadline()
	backend.Deadline = deadline
	if !exists {
		return nil, errors.New("exchange has no device deadline")
	}
	if backend.Wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []byte("OWON,HDS2202S,serial,1.0"), nil
}

// ReopenAndValidate observes the independent recovery budget.
//
// Example: a failed recovery leaves the pending command unissued.
func (backend *deadlineBackend) ReopenAndValidate(
	ctx context.Context,
	_ owonmodel.SerialNumber,
) error {
	backend.Recoveries++
	deadline, exists := ctx.Deadline()
	backend.RecoveryDeadline = deadline
	if !exists {
		return errors.New("recovery has no device deadline")
	}
	if backend.RecoveryWait {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// Close owns no external resources for this deterministic boundary fixture.
//
// Example: a test can close the session after every admitted call has returned.
func (*deadlineBackend) Close(context.Context) error { return nil }

// TestSessionDefaultOperationBudget establishes a finite deadline for unbounded callers.
//
// Example: a silent query ends after ten virtual seconds, without client cancellation or replay.
func TestSessionDefaultOperationBudget(t *testing.T) {
	synctest.Test(t,
		// verifyDefaultBudget checks the actual backend context rather than a constructor field.
		//
		// Example: a later identity receives recovery and a fresh command budget.
		func(t *testing.T) {
			backend := &deadlineBackend{Wait: true}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			start := time.Now()
			_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, start.Add(10*time.Second), backend.Deadline)
			require.Equal(t, 1, backend.Calls)
			require.True(t, session.poisoned)
			backend.Wait = false
			_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
			require.NoError(t, err)
			require.Equal(t, 1, backend.Recoveries)
			require.Equal(t, start.Add(20*time.Second), backend.RecoveryDeadline)
			require.Equal(t, backend.RecoveryDeadline, backend.Deadline)
			require.False(t, session.poisoned)
		})
}

// TestSessionRecoveryBudgetLeavesPoison verifies recovery silence cannot admit a command.
//
// Example: a failed recovery returns its timeout and leaves the quarantined session untouched.
func TestSessionRecoveryBudgetLeavesPoison(t *testing.T) {
	synctest.Test(t,
		// verifyRecoveryBudget starts with a previously quarantined session.
		//
		// Example: no pending identity write occurs while recovery cannot prove the device.
		func(t *testing.T) {
			backend := &deadlineBackend{RecoveryWait: true}
			session, err := New(backend, Config{ExpectedSerial: "serial"})
			require.NoError(t, err)
			session.poisoned = true
			_, err = session.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.True(t, session.poisoned)
			require.Zero(t, backend.Calls)
			require.Equal(t, 1, backend.Recoveries)
			require.Empty(t, session.admission)
		})
}

// TestDeviceBudgetRejectsCanceledEntry verifies cancellation before device work never poisons or opens.
//
// Example: an already-expired recovery cannot touch either the backend or its resource opener.
func TestDeviceBudgetRejectsCanceledEntry(t *testing.T) {
	backend := new(deadlineBackend)
	session, err := New(backend, Config{ExpectedSerial: "serial"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = session.Execute(ctx, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, session.poisoned)
	require.Zero(t, backend.Calls)
	session.poisoned = true
	_, err = session.Begin(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, session.poisoned)
	require.Zero(t, backend.Recoveries)
	require.ErrorIs(t, session.recover(ctx), context.Canceled)
	require.Zero(t, backend.Recoveries)
}
