package owonusb

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// deadlineReader exposes startup identity silence without native device access.
//
// Example: missing operation deadlines fail immediately instead of hanging the baseline test.
type deadlineReader struct {
	Deadline time.Time
}

// ReadContext reports the deadline that bounds an initial identity response.
//
// Example: a configured native startup budget applies before Session exists.
func (reader *deadlineReader) ReadContext(
	ctx context.Context,
	_ []byte,
) (int, error) {
	deadline, exists := ctx.Deadline()
	reader.Deadline = deadline
	if !exists {
		return 0, errors.New("identity has no device deadline")
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

// TestUSBInitialIdentityHasDeviceBudget verifies initial native identity has finite cancellation.
//
// Example: the injected resource boundary replaces only hardware, not recovery or framing.
func TestUSBInitialIdentityHasDeviceBudget(t *testing.T) {
	synctest.Test(t,
		// verifyInitialIdentityBudget tests both the default and an explicit finite startup policy.
		//
		// Example: a timeout rejects candidate resources without exposing an unvalidated session.
		func(t *testing.T) {
			for _, timeout := range []time.Duration{0, time.Second} {
				reader := new(deadlineReader)
				session, err := newEndpointSession(reader, new(endpointWriter), 1024)
				require.NoError(t, err)
				opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{{session: session}}}
				backend := &Backend{config: (Config{Serial: "serial", OperationTimeout: timeout}).withDefaults(), opener: opener}
				start := time.Now()
				err = backend.ReopenAndValidate(context.Background(), "serial")
				require.ErrorIs(t, err, context.DeadlineExceeded)
				want := timeout
				if want == 0 {
					want = owonsession.DefaultDeviceOperationTimeout
				}
				require.Equal(t, start.Add(want), reader.Deadline)
				require.Equal(t, 1, opener.Calls)
				require.NoError(t, backend.Close(context.Background()))
				require.Nil(t, backend.resources)
			}
			reader := new(deadlineReader)
			session, err := newEndpointSession(reader, new(endpointWriter), 1024)
			require.NoError(t, err)
			opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{{session: session}}}
			backend := &Backend{config: (Config{Serial: "serial", OperationTimeout: time.Minute}).withDefaults(), opener: opener}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			start := time.Now()
			require.ErrorIs(t, backend.ReopenAndValidate(ctx, "serial"), context.DeadlineExceeded)
			require.Equal(t, start.Add(time.Second), reader.Deadline)
			require.NoError(t, backend.Close(context.Background()))
		})
}
