package owonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEndpointMutationRecomputesSecurity rejects stale security decisions and invalid networks.
//
// Example: changing loopback to a remote address requires TLS without reparsing a URL.
func TestEndpointMutationRecomputesSecurity(t *testing.T) {
	endpoint, err := ParseEndpoint("tcp://127.0.0.1:50051")
	require.NoError(t, err)
	required, err := endpoint.RequiresTLS()
	require.NoError(t, err)
	require.False(t, required)
	endpoint.Address = "192.0.2.1:50051"
	required, err = endpoint.RequiresTLS()
	require.NoError(t, err)
	require.True(t, required)
	for _, network := range []Network{NetworkUnspecified, Network(99)} {
		endpoint.Network = network
		target, err := endpoint.Target()
		require.ErrorAs(t, err, new(*ErrEndpoint))
		require.Empty(t, target)
		_, err = endpoint.RequiresTLS()
		require.ErrorAs(t, err, new(*ErrEndpoint))
	}
}
