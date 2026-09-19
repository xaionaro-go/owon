package owonsession

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// retainedIOBackend waits for explicit native completion after its context expires.
//
// Example: it models cancellation that cannot yet release ownership of a native transfer.
type retainedIOBackend struct {
	Release    chan struct{}
	Calls      int
	Recoveries int
}

// Exchange retains the first native operation until the test releases completion.
//
// Example: a second command must not enter while this call is still outstanding.
func (backend *retainedIOBackend) Exchange(
	ctx context.Context,
	_ owonprotocol.Command,
) ([]byte, error) {
	backend.Calls++
	if backend.Calls == 1 {
		<-backend.Release
		return nil, ctx.Err()
	}
	return nil, nil
}

// ReopenAndValidate records recovery only after retained native completion.
//
// Example: recovery cannot overlap the ambiguous first transfer.
func (backend *retainedIOBackend) ReopenAndValidate(
	context.Context,
	owonmodel.SerialNumber,
) error {
	backend.Recoveries++
	return nil
}

// Close releases no extra resources after the fixture's transfer has completed.
//
// Example: tests join both operation runners before ending their bubble.
func (*retainedIOBackend) Close(context.Context) error { return nil }

// TestSessionDeadlineRetainsNativeOwnership verifies expiry never detaches in-flight work.
//
// Example: three elapsed budgets cannot allow the waiting caller to overlap a stuck transfer.
func TestSessionDeadlineRetainsNativeOwnership(t *testing.T) {
	synctest.Test(t,
		// verifyRetainedOwnership uses explicit completion release rather than a forced session lease.
		//
		// Example: after release, the failed first operation is recovered exactly once before the second.
		func(t *testing.T) {
			backend := &retainedIOBackend{Release: make(chan struct{})}
			session, err := New(backend, Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
			require.NoError(t, err)
			first, second := make(chan error, 1), make(chan error, 1)
			defer
			// releaseNativeCompletion also unblocks fixture operations after a fatal assertion.
			//
			// Example: the synctest barrier joins released runners before inspecting no further state.
			func() {
				close(backend.Release)
				synctest.Wait()
			}()
			command := owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}
			observability.Go(context.Background(), sessionExecuteRunner{Session: session, Command: command, Started: make(chan struct{}, 1), Result: first}.Run)
			synctest.Wait()
			observability.Go(context.Background(), sessionExecuteRunner{Session: session, Command: command, Started: make(chan struct{}, 1), Result: second}.Run)
			synctest.Wait()
			time.Sleep(3 * time.Second)
			require.Equal(t, 1, backend.Calls)
			require.Zero(t, backend.Recoveries)
			require.Empty(t, first)
			require.Empty(t, second)
			backend.Release <- struct{}{}
			require.ErrorIs(t, <-first, context.DeadlineExceeded)
			require.NoError(t, <-second)
			require.Equal(t, 2, backend.Calls)
			require.Equal(t, 1, backend.Recoveries)
		})
}
