package owonserver

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestClientAutoUsesTheSourceBackedCandidate verifies the complete typed RPC
// path reports transport completion without asserting device readback.
func TestClientAutoUsesTheSourceBackedCandidate(t *testing.T) {
	backend := &scriptedBackend{}
	client := newControlClient(t, backend)

	result, err := client.Auto(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.AppliedAt)
	require.Equal(t, []string{":AUToseton"}, backend.Commands)
}

// TestServerAutoRequiresTheOptionalActionSurface keeps unrelated Instrument
// consumers compatible while exposing an honest unsupported status.
func TestServerAutoRequiresTheOptionalActionSurface(t *testing.T) {
	server, err := NewServer(&identityInstrument{})
	require.NoError(t, err)

	_, err = server.Auto(t.Context(), nil)
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.ErrorContains(t, err, "does not support autoset action")
}
