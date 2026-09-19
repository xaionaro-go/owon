package owonctl_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

// TestClientConfigUsesSharedDefaultsAndExplicitOverrides verifies reusable flag policy.
//
// Example: an explicit loopback address is retained while an omitted timeout uses its default.
func TestClientConfigUsesSharedDefaultsAndExplicitOverrides(t *testing.T) {
	var config owonctl.ClientConfig
	flags := pflag.NewFlagSet("client", pflag.ContinueOnError)
	config.RegisterFlags(flags)
	require.Equal(t, owonrpc.DefaultEndpoint(), config.Address)
	require.Equal(t, owonctl.DefaultTimeout, config.Timeout)
	require.NoError(t, flags.Parse([]string{"--address", "tcp://127.0.0.1:50051", "--timeout", "3s"}))
	connection, err := config.ConnectionConfig()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:50051", connection.Endpoint.Address)
	require.Equal(t, 3*time.Second, config.Timeout)
	require.Empty(t, connection.TLS.CAFile)
}

// TestClientConfigRejectsInvalidTransportPolicy verifies validation before connection creation.
//
// Example: local TLS credentials and incomplete remote credentials are rejected.
func TestClientConfigRejectsInvalidTransportPolicy(t *testing.T) {
	for _, config := range []*owonctl.ClientConfig{
		nil,
		{Address: "unix:///tmp/owon.sock", Timeout: 0},
		{Address: "invalid", Timeout: time.Second},
		{Address: "tcp://192.0.2.1:50051", Timeout: time.Second},
		{Address: "unix:///tmp/owon.sock", Timeout: time.Second, TLS: owonrpc.ClientTLSConfig{CAFile: "ca.pem"}},
	} {
		connection, err := config.ConnectionConfig()
		require.Error(t, err)
		require.Zero(t, connection)
	}
}

// TestCLIErrorContractsPreserveTheirCauses verifies public classifications retain original errors.
//
// Example: an endpoint cause is inspectable and appears only once in a diagnostic.
func TestCLIErrorContractsPreserveTheirCauses(t *testing.T) {
	cause := errors.New("original cause")
	for _, err := range []error{
		&owonctl.ErrConfiguration{Field: "address", Reason: "invalid", Cause: cause},
		&owonctl.ErrFlagParse{Command: "owonctl", Cause: cause},
	} {
		require.ErrorIs(t, err, cause)
		require.Equal(t, 1, strings.Count(err.Error(), cause.Error()))
	}
	arguments := &owonctl.ErrCommandArguments{Command: "dmm", Reason: "invalid function"}
	require.Equal(t, "dmm: invalid function", arguments.Error())
	require.Nil(t, arguments.Unwrap())
}
