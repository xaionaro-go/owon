package owonusb

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/gousb"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// timedBulkEndpoint completes fragments under one caller-owned operation budget.
//
// Example: three short writes cannot each renew a one-second exchange deadline.
type timedBulkEndpoint struct {
	ReadDelay  time.Duration
	WriteDelay time.Duration
	ReadChunks [][]byte
	WriteWidth int
	QuietProbe bool
	Written    []byte
	Deadlines  []time.Time
}

// waitTransfer records a deadline and waits for either completion or cancellation.
//
// Example: the simulated clock deterministically selects an earlier operation deadline.
func (endpoint *timedBulkEndpoint) waitTransfer(
	ctx context.Context,
	delay time.Duration,
) error {
	deadline, exists := ctx.Deadline()
	if !exists {
		return &ErrInvalidConfig{Reason: "test transfer has no deadline"}
	}
	endpoint.Deadlines = append(endpoint.Deadlines, deadline)
	if ctx.Err() != nil {
		return gousb.TransferCancelled
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return gousb.TransferCancelled
	case <-timer.C:
		return nil
	}
}

// ReadContext returns one completed frame fragment or native cancellation.
//
// Example: an empty fragment list models a silent endpoint until cancellation.
func (endpoint *timedBulkEndpoint) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	if endpoint.QuietProbe {
		endpoint.QuietProbe = false
		<-ctx.Done()

		return 0, gousb.TransferCancelled
	}
	if len(endpoint.ReadChunks) == 0 {
		<-ctx.Done()
		return 0, gousb.TransferCancelled
	}
	if err := endpoint.waitTransfer(ctx, endpoint.ReadDelay); err != nil {
		return 0, err
	}
	chunk := endpoint.ReadChunks[0]
	endpoint.ReadChunks = endpoint.ReadChunks[1:]
	return copy(destination, chunk), nil
}

// WriteContext records only accepted bytes, never a canceled fragment.
//
// Example: timeout after two one-byte fragments leaves exactly two accepted bytes.
func (endpoint *timedBulkEndpoint) WriteContext(
	ctx context.Context,
	source []byte,
) (int, error) {
	if err := endpoint.waitTransfer(ctx, endpoint.WriteDelay); err != nil {
		return 0, err
	}
	count := len(source)
	if endpoint.WriteWidth > 0 {
		count = min(count, endpoint.WriteWidth)
	}
	endpoint.Written = append(endpoint.Written, source[:count]...)
	return count, nil
}

// TestTransportExchangeBudgetCoversAllFragments verifies one deadline covers the complete protocol exchange.
//
// Example: a partial write, fragmented read, or final quiet probe can consume the same one-second budget.
func TestTransportExchangeBudgetCoversAllFragments(t *testing.T) {
	synctest.Test(t,
		// verifyFragmentBudgets runs each phase through production native-session framing and Session.
		//
		// Example: late fragments cannot extend the operation while holding serialized ownership.
		func(t *testing.T) {
			for _, test := range []struct {
				Name        string
				Endpoint    timedBulkEndpoint
				Mode        owonprotocol.ResponseMode
				Budget      time.Duration
				WantWritten string
			}{
				{Name: "partial write", Endpoint: timedBulkEndpoint{WriteDelay: 400 * time.Millisecond, WriteWidth: 1}, Mode: owonprotocol.ResponseModeASCII, Budget: time.Second, WantWritten: "*I"},
				{Name: "fragmented read", Endpoint: timedBulkEndpoint{ReadDelay: 400 * time.Millisecond, ReadChunks: [][]byte{[]byte("a"), []byte("b"), []byte("c\n")}}, Mode: owonprotocol.ResponseModeASCII, Budget: time.Second, WantWritten: "*IDN?\n"},
				{Name: "silent read", Endpoint: timedBulkEndpoint{}, Mode: owonprotocol.ResponseModeASCII, Budget: time.Second, WantWritten: "*IDN?\n"},
				{Name: "silent write", Endpoint: timedBulkEndpoint{WriteDelay: time.Hour}, Mode: owonprotocol.ResponseModeASCII, Budget: time.Second},
				{Name: "no response probe", Endpoint: timedBulkEndpoint{WriteDelay: 2 * time.Millisecond}, Mode: owonprotocol.ResponseModeNone, Budget: 3 * time.Millisecond, WantWritten: "*IDN?\n"},
			} {
				verifyExchangeBudget(t, test.Name, &test.Endpoint, test.Mode, test.Budget, test.WantWritten)
			}
		})
}

// verifyExchangeBudget checks exact accepted bytes, poisoning and no replay for one timed exchange.
//
// Example: the second transaction command cannot issue after a timeout in the first.
func verifyExchangeBudget(
	t *testing.T,
	name string,
	endpoint *timedBulkEndpoint,
	mode owonprotocol.ResponseMode,
	budget time.Duration,
	wantWritten string,
) {
	session, err := newEndpointSession(endpoint, endpoint, 1024)
	require.NoError(t, err)
	backend := &Backend{resources: &usbResources{session: session}}
	transport, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "serial", OperationTimeout: budget})
	require.NoError(t, err)
	transaction, err := transport.Begin(context.Background())
	require.NoError(t, err)
	defer transaction.Close()
	start := time.Now()
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: mode})
	require.ErrorIs(t, err, context.DeadlineExceeded, name)
	require.ErrorIs(t, err, gousb.TransferCancelled, name)
	require.Equal(t, budget, time.Since(start), name)
	require.Equal(t, wantWritten, string(endpoint.Written), name)
	_, err = transaction.Execute(context.Background(), owonprotocol.Command{Text: "NEXT", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorIs(t, err, context.DeadlineExceeded, name)
	require.Equal(t, wantWritten, string(endpoint.Written), name)
	for _, deadline := range endpoint.Deadlines {
		require.Equal(t, start.Add(budget), deadline, name)
	}
	transaction.Close()
	_, err = transport.Begin(context.Background())
	require.ErrorContains(t, err, "recover poisoned session", name)
	require.Equal(t, wantWritten, string(endpoint.Written), name)
}
