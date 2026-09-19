package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"google.golang.org/grpc"
)

// TestParseOptionsEnforcesRemoteMutualTLS verifies listener-specific credential policy.
//
// Example: local TLS flags and remote servers without a client allowlist are rejected.
func TestParseOptionsEnforcesRemoteMutualTLS(t *testing.T) {
	t.Parallel()

	_, err := parseOptions([]string{"--serial", "25061855", "--tls-cert", "server.pem"}, io.Discard)
	require.ErrorContains(t, err, "rejected")
	_, err = parseOptions([]string{"--serial", "25061855", "--max-subscriptions", "0"}, io.Discard)
	require.ErrorContains(t, err, "[1,1024]")
	_, err = parseOptions([]string{
		"--serial", "25061855", "--listen", "tcp://0.0.0.0:50051",
		"--tls-cert", "server.pem", "--tls-key", "server.key", "--tls-client-ca", "ca.pem",
	}, io.Discard)
	require.ErrorContains(t, err, "tls-client")
	config, err := parseOptions([]string{
		"--serial", " 25061855 ", "--model", " HDS2202S ", "--listen", "tcp://0.0.0.0:50051",
		"--tls-cert", "server.pem", "--tls-key", "server.key", "--tls-client-ca", "ca.pem",
		"--tls-client-san", "collector.example,spiffe://example/collector",
	}, io.Discard)
	require.NoError(t, err)
	require.EqualValues(t, "25061855", config.Serial)
	require.Equal(t, "HDS2202S", config.Model)
	require.Equal(t, []string{"collector.example", "spiffe://example/collector"}, config.ClientSANs)
}

// TestParseOptionsReturnsTypedErrConfigurations verifies operator input is classifiable.
//
// Example: automation can distinguish an empty expected model from a transport failure with errors.As.
func TestParseOptionsReturnsTypedErrConfigurations(t *testing.T) {
	t.Parallel()

	_, err := parseOptions([]string{"--model", ""}, io.Discard)
	var configuration *ErrConfiguration
	require.ErrorAs(t, err, &configuration)
	require.Equal(t, "--model", configuration.Field)
}

// TestParseOptionsTreatsHelpAsAProcessSuccessSignal verifies standard flag help is distinguishable.
//
// Example: owond --help can exit zero without attempting serial selection.
func TestParseOptionsTreatsHelpAsAProcessSuccessSignal(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	_, err := parseOptions([]string{"--help"}, &output)
	require.NoError(t, err)
	require.Contains(t, output.String(), "Usage:")
}

// TestParseOptionsWrapsFlagFailures verifies malformed values are classifiable configuration errors.
//
// Example: an invalid max-subscriptions value never reaches USB discovery.
func TestParseOptionsWrapsFlagFailures(t *testing.T) {
	t.Parallel()

	_, err := parseOptions([]string{"--max-subscriptions", "not-a-number"}, io.Discard)
	var configuration *ErrConfiguration
	require.ErrorAs(t, err, &configuration)
	require.Equal(t, "flags", configuration.Field)
}

// TestDefaultListenerCreatesPrivateDirectory checks automatic startup without privileged setup.
//
// Example: a default listener creates only an owner-accessible child and can reopen after shutdown.
func TestDefaultListenerCreatesPrivateDirectory(t *testing.T) {
	runtimeDirectory := unixListenerTestDir(t)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	configuration, err := parseOptions(nil, io.Discard)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(runtimeDirectory, "owon", "owond.sock"), configuration.Endpoint.Address)
	for range 2 {
		listener, err := openListener(t.Context(), configuration.Endpoint)
		require.NoError(t, err)
		info, statErr := os.Stat(filepath.Dir(configuration.Endpoint.Address))
		closeErr := listener.Close()
		require.NoError(t, statErr)
		require.NoError(t, closeErr)
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
		_, err = os.Stat(configuration.Endpoint.Address)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

// TestDefaultListenerRejectsNonPrivateDirectory preserves existing permissions instead of broadening trust.
//
// Example: a shared directory at the per-user default is rejected without creating a lock or socket.
func TestDefaultListenerRejectsNonPrivateDirectory(t *testing.T) {
	runtimeDirectory := unixListenerTestDir(t)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	parent := filepath.Join(runtimeDirectory, "owon")
	require.NoError(t, os.Mkdir(parent, 0o750))
	require.NoError(t, os.Chmod(parent, 0o750))
	configuration, err := parseOptions(nil, io.Discard)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), configuration.Endpoint)
	if listener != nil {
		require.NoError(t, listener.Close())
	}
	require.ErrorContains(t, err, "private")
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Empty(t, entries)
	info, err := os.Stat(parent)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o750), info.Mode().Perm())
}

// TestDefaultListenerRejectsDirectorySymlink prevents reuse of an indirect default ownership boundary.
//
// Example: a symlink to a private directory is preserved and no socket or lock appears in its target.
func TestDefaultListenerRejectsDirectorySymlink(t *testing.T) {
	runtimeDirectory := unixListenerTestDir(t)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	target := filepath.Join(runtimeDirectory, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	parent := filepath.Join(runtimeDirectory, "owon")
	require.NoError(t, os.Symlink(target, parent))
	configuration, err := parseOptions(nil, io.Discard)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), configuration.Endpoint)
	require.ErrorContains(t, err, "private")
	require.Nil(t, listener)
	entries, err := os.ReadDir(target)
	require.NoError(t, err)
	require.Empty(t, entries)
	info, err := os.Lstat(parent)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	require.ErrorIs(t, validatePrivateUnixSocketDirectory(filepath.Join(runtimeDirectory, "absent")), os.ErrNotExist)
}

// TestOpenListenerAppliesUnixPermissions verifies filesystem access is the local boundary.
//
// Example: a newly created daemon socket is mode 0660 and its parent is absolute.
func TestOpenListenerAppliesUnixPermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(unixListenerTestDir(t), "runtime", "owond.sock")
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), endpoint)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o660), info.Mode().Perm())
	require.NoError(t, listener.Close())
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestOpenListenerAppliesUnixPermissionsUnderUmask verifies path chmod restores the requested mode.
//
// Example: both restrictive and ordinary process umasks still produce a 0660 daemon socket.
func TestOpenListenerAppliesUnixPermissionsUnderUmask(t *testing.T) {
	for _, umask := range []int{0o022, 0o077} {
		// verifyUmaskCase checks one process umask without changing the requested socket mode.
		//
		// Example: a restrictive umask still leaves the socket at 0660 after explicit chmod.
		func() {
			previousUmask := syscall.Umask(umask)
			defer syscall.Umask(previousUmask)

			path := filepath.Join(unixListenerTestDir(t), "owond.sock")
			endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
			require.NoError(t, err)
			listener, err := openListener(t.Context(), endpoint)
			require.NoError(t, err)
			// closeTestListener releases the temporary Unix socket after assertions.
			//
			// Example: cleanup preserves the test's first failure while reporting close errors.
			defer func() {
				require.NoError(t, listener.Close())
			}()

			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o660), info.Mode().Perm())
		}()
	}
}

// TestOpenListenerRejectsWritableUnixParent verifies the trusted directory boundary.
//
// Example: a group/other-writable non-sticky directory cannot host a daemon socket.
func TestOpenListenerRejectsWritableUnixParent(t *testing.T) {
	t.Parallel()

	parent := unixListenerTestDir(t)
	require.NoError(t, os.Chmod(parent, 0o777))
	path := filepath.Join(parent, "owond.sock")
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)

	listener, err := openListener(t.Context(), endpoint)
	require.ErrorContains(t, err, "writable")
	require.Nil(t, listener)
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestOpenListenerRejectsWritableUnixParentContainingSymlink verifies lexical parents cannot be bypassed.
//
// Example: a symlink under an unsafe directory cannot redirect the socket into a safe target.
func TestOpenListenerRejectsWritableUnixParentContainingSymlink(t *testing.T) {
	t.Parallel()

	root := unixListenerTestDir(t)
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	unsafeParent := filepath.Join(root, "unsafe")
	require.NoError(t, os.Mkdir(unsafeParent, 0o700))
	require.NoError(t, os.Chmod(unsafeParent, 0o777))
	link := filepath.Join(unsafeParent, "link")
	require.NoError(t, os.Symlink(target, link))
	path := filepath.Join(link, "owond.sock")
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)

	listener, err := openListener(t.Context(), endpoint)
	require.ErrorContains(t, err, "writable")
	require.Nil(t, listener)
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestOwnBoundUnixListenerRemovesSocketOnIdentityFailure verifies setup failure does not strand an inode.
//
// Example: identity inspection failure closes a newly bound socket while unlink-on-close remains enabled.
func TestOwnBoundUnixListenerRemovesSocketOnIdentityFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	listenerValue, err := net.Listen("unix", path)
	require.NoError(t, err)
	listener, ok := listenerValue.(*net.UnixListener)
	require.True(t, ok)
	identityErr := errors.New("identity unavailable")
	owned, err := ownBoundUnixListener(listener, path, nil,
		// failIdentityInspection forces the post-bind identity error path.
		//
		// Example: a missing socket identity closes the newly bound listener.
		func(string) (os.FileInfo, error) {
			return nil, identityErr
		})
	require.ErrorIs(t, err, identityErr)
	require.Nil(t, owned)
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestOpenListenerRejectsActiveUnixSocket verifies a second daemon cannot unlink a live endpoint.
//
// Example: the first listener remains reachable after the second startup attempt fails.
func TestOpenListenerRejectsActiveUnixSocket(t *testing.T) {
	t.Parallel()

	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)
	first, err := openListener(t.Context(), endpoint)
	require.NoError(t, err)
	// closeFirstListener releases the first listener after the active-socket assertion.
	//
	// Example: the live endpoint remains available until the test completes.
	defer func() {
		require.NoError(t, first.Close())
	}()

	second, err := openListener(t.Context(), endpoint)
	require.ErrorContains(t, err, "already")
	require.Nil(t, second)
	probe, err := net.DialTimeout("unix", path, time.Second)
	require.NoError(t, err)
	require.NoError(t, probe.Close())
}

// TestOpenListenerReplacesStaleUnixSocket verifies crashed-daemon socket paths are recoverable.
//
// Example: a closed raw Unix listener leaves a stale inode that owond safely removes.
func TestOpenListenerReplacesStaleUnixSocket(t *testing.T) {
	t.Parallel()

	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	stale, err := net.Listen("unix", path)
	require.NoError(t, err)
	unixListener, ok := stale.(*net.UnixListener)
	require.True(t, ok)
	unixListener.SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	_, err = os.Lstat(path)
	require.NoError(t, err)
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), endpoint)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
}

// TestOwnedUnixListenerPreservesReplacementPath verifies cleanup cannot unlink a replacement inode.
//
// Example: an external replacement between shutdown phases remains available after the owner closes.
func TestOwnedUnixListenerPreservesReplacementPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	endpoint, err := owonrpc.ParseEndpoint("unix://" + path)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), endpoint)
	require.NoError(t, err)
	movedPath := path + ".moved"
	require.NoError(t, os.Rename(path, movedPath))
	replacement, err := net.Listen("unix", path)
	require.NoError(t, err)
	require.ErrorContains(t, listener.Close(), "replaced")
	_, err = os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, replacement.Close())
	require.NoError(t, os.Remove(movedPath))
}

// TestGRPCServeLoopStopsCleanly verifies daemon serving publishes its lifecycle result.
//
// Example: Stop causes the named serve loop to return nil without stranding shutdown.
func TestGRPCServeLoopStopsCleanly(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(owonserver.GRPCServerOptions()...)
	started := make(chan struct{})
	trackedListener := &acceptTrackingListener{Listener: listener, started: started}
	errorsChannel := make(chan error, 1)
	loop := grpcServeLoop{Server: server, Listener: trackedListener, Result: errorsChannel}
	observability.Go(context.Background(), loop.Run)
	<-started
	server.Stop()
	require.NoError(t, <-errorsChannel)
	require.NoError(t, closeListener(listener))
}

// unixListenerTestDir creates a socket-test root under sticky /tmp.
//
// Example: listener tests do not inherit an unsafe repository-local TMPDIR parent.
func unixListenerTestDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "owond-test-")
	require.NoError(t, err)
	t.Cleanup(
		// cleanupUnixListenerTestDir removes the isolated test root after the listener closes.
		//
		// Example: no socket or lock artifacts survive the test.
		func() {
			require.NoError(t, os.RemoveAll(directory))
		})

	return directory
}

// acceptTrackingListener reports when grpc.Server has entered its accept loop.
//
// Example: lifecycle tests avoid racing Stop against the initial Serve call.
type acceptTrackingListener struct {
	net.Listener
	started chan struct{}
	once    sync.Once
}

// Accept signals first admission before delegating to the real listener.
//
// Example: the test waits for started before calling grpc.Server.Stop.
func (listener *acceptTrackingListener) Accept() (net.Conn, error) {
	listener.once.Do(
		// signalAcceptLoop closes the one-shot lifecycle signal.
		//
		// Example: repeated Accept calls do not close the channel twice.
		func() {
			close(listener.started)
		})

	return listener.Listener.Accept()
}

// TestCloseListenerAcceptsAlreadyClosed verifies cleanup remains idempotent at call sites.
//
// Example: an error path can close a listener after grpc.Server already stopped serving.
func TestCloseListenerAcceptsAlreadyClosed(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	require.NoError(t, closeListener(listener))
}
