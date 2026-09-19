package owonsession

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// cleanupContextKey identifies the originating operation in detached cleanup.
//
// Example: backend cleanup retains an operation label after its caller cancels.
type cleanupContextKey struct{}

// contextCloseBackend observes the context delivered to native cleanup.
//
// Example: the embedded backend supplies exchanges while Close checks cleanup lifetime.
type contextCloseBackend struct {
	scriptedBackend
	Value string
	Err   error
	Calls int
}

// Close records values and cancellation independently of the caller's wait.
//
// Example: a canceled request must still deliver its label with no cancellation error.
func (backend *contextCloseBackend) Close(ctx context.Context) error {
	backend.Value, _ = ctx.Value(cleanupContextKey{}).(string)
	backend.Err = ctx.Err()
	backend.Calls++
	return nil
}

// TestCloseContextPreservesValuesAfterCancellation checks detached cleanup identity and lifetime.
//
// Example: a second caller joins cleanup without replacing the initiating operation label.
func TestCloseContextPreservesValuesAfterCancellation(t *testing.T) {
	backend := new(contextCloseBackend)
	session, err := New(backend, Config{ExpectedSerial: "serial"})
	require.NoError(t, err)
	transaction, err := session.Begin(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), cleanupContextKey{}, "operation"))
	cancel()
	require.ErrorIs(t, session.CloseContext(ctx), context.Canceled)
	transaction.Close()
	require.NoError(t, session.CloseContext(t.Context()))
	require.Equal(t, "operation", backend.Value)
	require.NoError(t, backend.Err)
	require.Equal(t, 1, backend.Calls)
}
