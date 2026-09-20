package owonserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// keepAwakeProbe exposes only the optional startup policy for server delegation tests.
//
// Example: the public Instrument contract remains unchanged while the native controller gains policy wiring.
type keepAwakeProbe struct {
	Instrument
	Calls int
	Error error
}

// EnsureKeepAwake records one policy delegation and returns the scripted result.
//
// Example: daemon startup can inspect the exact policy failure without an RPC round trip.
func (probe *keepAwakeProbe) EnsureKeepAwake(context.Context) error {
	probe.Calls++
	return probe.Error
}

// TestServerDelegatesKeepAwakeToOptionalInstrument verifies startup-only policy delegation.
//
// Example: a native controller receives exactly one call and its error is preserved.
func TestServerDelegatesKeepAwakeToOptionalInstrument(t *testing.T) {
	cause := errors.New("timer policy failed")
	probe := &keepAwakeProbe{Error: cause}
	server, err := NewServer(probe)
	require.NoError(t, err)
	require.ErrorIs(t, server.EnsureKeepAwake(t.Context()), cause)
	require.Equal(t, 1, probe.Calls)
}

// TestServerRejectsKeepAwakeWithoutOptionalSupport keeps unsupported startup policy explicit.
//
// Example: consumer Instrument implementations receive Unimplemented rather than a silent no-op.
func TestServerRejectsUnsupportedKeepAwakePolicy(t *testing.T) {
	server, err := NewServer(&identityInstrument{})
	require.NoError(t, err)
	require.ErrorContains(t, server.EnsureKeepAwake(t.Context()), "does not support keep-awake policy")
}
