package owonclient

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

// TestNewConnectionIsLazyAndOwnsItsTransport verifies construction needs no reachable daemon.
//
// Example: an absent Unix socket can be configured, then closed without any RPC.
func TestNewConnectionIsLazyAndOwnsItsTransport(t *testing.T) {
	config := ConnectionConfig{Endpoint: owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: "/missing/owon/socket"}}
	connection, err := NewConnection(t.Context(), config)
	require.NoError(t, err)
	require.NotNil(t, connection.Client())
	require.NotNil(t, connection.RPCClient())
	require.NoError(t, connection.Close())
	_, operationErr := connection.Client().DeviceInfo(t.Context())
	require.Error(t, operationErr)
	closeErr := connection.Close()
	require.Error(t, closeErr)
	joined := errors.Join(operationErr, closeErr)
	require.ErrorIs(t, joined, operationErr)
	require.ErrorIs(t, joined, closeErr)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = NewConnection(ctx, config)
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewConnection(nil, config)
	require.ErrorAs(t, err, new(*ErrInvalidInput))
}

// TestConnectionConfigRejectsMissingRemoteTLS verifies address changes cannot bypass credential policy.
//
// Example: a local configuration becomes invalid after switching to a remote address.
func TestConnectionConfigRejectsMissingRemoteTLS(t *testing.T) {
	config := ConnectionConfig{Endpoint: owonrpc.Endpoint{Network: owonrpc.NetworkTCP, Address: "127.0.0.1:50051"}}
	require.NoError(t, config.Validate())
	config.TLS = owonrpc.ClientTLSConfig{CAFile: "ca", CertificateFile: "cert", PrivateKeyFile: "key", ServerName: "server"}
	require.ErrorAs(t, config.Validate(), new(*owonrpc.ErrTLSConfiguration))
	config.TLS = owonrpc.ClientTLSConfig{}
	config.Endpoint.Address = "192.0.2.1:50051"
	require.ErrorAs(t, config.Validate(), new(*owonrpc.ErrTLSConfiguration))
	_, err := config.DialOptions()
	require.ErrorAs(t, err, new(*owonrpc.ErrTLSConfiguration))
	config.Endpoint.Network = owonrpc.NetworkUnspecified
	require.ErrorAs(t, config.Validate(), new(*owonrpc.ErrEndpoint))
}
