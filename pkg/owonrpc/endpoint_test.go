package owonrpc

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDefaultEndpointUsesRuntimeDirectory checks per-invocation discovery without filesystem writes.
//
// Example: separate applications in one login derive the same socket, including URL-sensitive path bytes.
func TestDefaultEndpointUsesRuntimeDirectory(t *testing.T) {
	for _, runtimeDirectory := range []string{"/run/user/1234", "/tmp/owon runtime#?%"} {
		t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
		endpoint, err := ParseEndpoint(DefaultEndpoint())
		require.NoError(t, err)
		require.Equal(t, NetworkUnix, endpoint.Network)
		require.Equal(t, filepath.Join(runtimeDirectory, "owon", "owond.sock"), endpoint.Address)
		required, err := endpoint.RequiresTLS()
		require.NoError(t, err)
		require.False(t, required)
	}
	runtimeDirectory := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	_, err := ParseEndpoint(DefaultEndpoint())
	require.NoError(t, err)
	_, err = os.Stat(runtimeDirectory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestDefaultEndpointFallsBackToUserTemporarySocket checks an absent or unusable runtime path.
//
// Example: an unset runtime directory uses /tmp independently of a process-specific TMPDIR.
func TestDefaultEndpointFallsBackToUserTemporarySocket(t *testing.T) {
	t.Setenv("TMPDIR", "/unrelated/process/temp")
	for _, runtimeDirectory := range []string{"", "relative/path", "/" + strings.Repeat("x", 107)} {
		t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
		endpoint, err := ParseEndpoint(DefaultEndpoint())
		require.NoError(t, err)
		require.Equal(t, filepath.Join("/tmp", "owon-"+strconv.Itoa(os.Geteuid()), "owond.sock"), endpoint.Address)
		require.Equal(t, NetworkUnix, endpoint.Network)
		required, err := endpoint.RequiresTLS()
		require.NoError(t, err)
		require.False(t, required)
	}
}

// TestUnixTargetPreservesFilenameSyntax verifies decoded paths survive URL reserialization.
//
// Example: a hash in a filename is not reinterpreted as the target URL's fragment.
func TestUnixTargetPreservesFilenameSyntax(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/tmp/scope#name", "/tmp/scope?name", "/tmp/scope%name", "/tmp/scope name", "/tmp/осциллограф"} {
		input := &url.URL{Scheme: "unix", Path: path}
		endpoint, err := ParseEndpoint(input.String())
		require.NoError(t, err)
		value, err := endpoint.Target()
		require.NoError(t, err)
		target, err := url.Parse(value)
		require.NoError(t, err)
		require.Equal(t, path, target.Path)
		require.Empty(t, target.Fragment)
		require.Empty(t, target.RawQuery)
	}
}

// TestEndpointTargetsAndMalformedAddresses verifies endpoint failures never yield a usable target.
//
// Example: TCP URLs require a concrete host and port, while unknown networks have no gRPC target.
func TestEndpointTargetsAndMalformedAddresses(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"unix://relative", "unix:relative", "tcp://localhost", "tcp://:1234"} {
		endpoint, err := ParseEndpoint(value)
		requireErrorType[*ErrEndpoint](t, err)
		target, targetErr := endpoint.Target()
		require.ErrorAs(t, targetErr, new(*ErrEndpoint))
		require.Empty(t, target)
	}
	endpoint, err := ParseEndpoint("tcp://127.0.0.1:1234")
	require.NoError(t, err)
	target, err := endpoint.Target()
	require.NoError(t, err)
	require.Equal(t, "dns:///127.0.0.1:1234", target)
	required, err := endpoint.RequiresTLS()
	require.NoError(t, err)
	require.False(t, required)
}

// TestParseEndpointAcceptsUnixAndLoopbackTLS verifies safe listener endpoint policy.
//
// Example: Unix sockets need no TLS while non-loopback TCP requires TLS.
func TestParseEndpointAcceptsUnixAndLoopbackTLS(t *testing.T) {
	t.Parallel()

	unixEndpoint, err := ParseEndpoint("unix:///run/owond/owond.sock")
	require.NoError(t, err)
	require.Equal(t, NetworkUnix, unixEndpoint.Network)
	target, err := unixEndpoint.Target()
	require.NoError(t, err)
	require.Equal(t, "unix:///run/owond/owond.sock", target)
	required, err := unixEndpoint.RequiresTLS()
	require.NoError(t, err)
	require.False(t, required)
	loopback, err := ParseEndpoint("tcp://127.0.0.1:50051")
	require.NoError(t, err)
	required, err = loopback.RequiresTLS()
	require.NoError(t, err)
	require.False(t, required)
	remote, err := ParseEndpoint("tcp://0.0.0.0:50051")
	require.NoError(t, err)
	required, err = remote.RequiresTLS()
	require.NoError(t, err)
	require.True(t, required)
	_, err = ParseEndpoint("udp://127.0.0.1:50051")
	require.Error(t, err)
}

// TestParseEndpointRejectsDiscardedOrInvalidComponents verifies every URL byte is meaningful.
//
// Example: a TCP path or out-of-range port is rejected instead of silently discarded.
func TestParseEndpointRejectsDiscardedOrInvalidComponents(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"unix://authority/run/owond.sock",
		"unix:///run/owond.sock?ignored=true",
		"tcp://127.0.0.1:50051/path",
		"tcp://127.0.0.1:0",
		"tcp://127.0.0.1:70000",
		"tcp://127.0.0.1:not-a-port",
	} {
		_, err := ParseEndpoint(value)
		require.Error(t, err, value)
	}
}
