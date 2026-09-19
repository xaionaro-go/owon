package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

// failedListenerClose simulates a listener that cannot release its descriptor.
//
// Example: owned socket cleanup must preserve its pathname when closure fails.
type failedListenerClose struct {
	net.Listener
	Cause error
}

// Close returns the underlying closure failure for ownership testing.
//
// Example: closeListener must retain a non-net.ErrClosed failure.
func (listener failedListenerClose) Close() error { return listener.Cause }

// TestUnixListenerBindFailureReleasesLock checks an unbindable pathname cannot strand endpoint ownership.
//
// Example: a filesystem-valid name exceeding the Unix socket address limit fails bind and releases its lock.
func TestUnixListenerBindFailureReleasesLock(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), strings.Repeat("s", 120))
	listener, err := openListener(t.Context(), owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: path})
	require.Nil(t, listener)
	require.ErrorContains(t, err, "listen on")
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	lock, err := os.OpenFile(path+unixSocketLockSuffix, os.O_RDWR, 0o600)
	require.NoError(t, err)
	t.Cleanup(
		// releaseBindLock releases the test's independent lock acquisition after the bind failure.
		//
		// Example: a failed assertion cannot strand an ownership descriptor.
		func() { require.NoError(t, unlockUnixSocket(lock)) },
	)
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}

// TestUnixListenerProbeFailurePreservesSocket checks non-refusal connection errors cannot authorize unlinking.
//
// Example: an existing datagram socket is preserved when a stream-socket probe fails with the wrong protocol.
func TestUnixListenerProbeFailurePreservesSocket(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), "datagram.sock")
	external, err := net.ListenPacket("unixgram", path)
	require.NoError(t, err)
	t.Cleanup(
		// closeDatagram releases the foreign socket after its identity has been verified.
		//
		// Example: a failed assertion still closes the descriptor before temporary-directory cleanup.
		func() { require.NoError(t, external.Close()) },
	)
	identity, err := os.Lstat(path)
	require.NoError(t, err)
	listener, err := openListener(t.Context(), owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: path})
	t.Cleanup(
		// closeUnexpectedListener releases any mistakenly admitted listener before reporting the failed invariant.
		//
		// Example: falsifying the probe guard does not leak the replacement listener.
		func() { require.NoError(t, closeListener(listener)) },
	)
	require.Nil(t, listener)
	require.ErrorIs(t, err, syscall.EPROTOTYPE)
	require.ErrorContains(t, err, "probe existing Unix socket")
	retained, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(identity, retained))
	lock, err := os.OpenFile(path+unixSocketLockSuffix, os.O_RDWR, 0o600)
	require.NoError(t, err)
	t.Cleanup(
		// releaseProbeLock closes the independently acquired lock even if the acquisition assertion fails.
		//
		// Example: successful acquisition confirms the failed probe released ownership.
		func() { require.NoError(t, unlockUnixSocket(lock)) },
	)
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}

// TestOwnedListenerPreservesSocketOnFailedClose checks cleanup does not destroy an active endpoint.
//
// Example: listener close failure releases its lock but keeps the owned path for diagnosis.
func TestOwnedListenerPreservesSocketOnFailedClose(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), "owned")
	require.NoError(t, os.WriteFile(path, []byte("owned"), 0o600))
	identity, err := os.Lstat(path)
	require.NoError(t, err)
	cause := errors.New("cannot close")
	listener := &ownedUnixListener{Listener: failedListenerClose{Cause: cause}, path: path, identity: identity}
	require.ErrorIs(t, listener.Close(), cause)
	require.ErrorIs(t, listener.Close(), cause)
	retained, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(identity, retained))
	require.ErrorIs(t, closeListener(failedListenerClose{Cause: cause}), cause)
	require.NoError(t, closeListener(nil))
	require.NoError(t, (*ownedUnixListener)(nil).Close())
}

// TestOwnedListenerRetainsIndependentCleanupFailures checks descriptor and lock failures survive one shutdown.
//
// Example: a failed socket close preserves its path even when unlocking and closing its lock also fail.
func TestOwnedListenerRetainsIndependentCleanupFailures(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), "owned")
	require.NoError(t, os.WriteFile(path, []byte("owned"), 0o600))
	identity, err := os.Lstat(path)
	require.NoError(t, err)
	lock, err := os.OpenFile(path+unixSocketLockSuffix, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
	cause := errors.New("listener remains open")
	listener := &ownedUnixListener{Listener: failedListenerClose{Cause: cause}, path: path, identity: identity, lock: lock}
	err = listener.Close()
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, syscall.EBADF)
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, "unlock Unix socket")
	require.ErrorContains(t, err, "close Unix socket lock")
	require.Same(t, err, listener.Close())
	retained, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(identity, retained))
}

// TestUnixListenerRejectsForeignObjectsAndReleasesLocks checks failed startup is recoverable.
//
// Example: a regular file is preserved, and removing it permits a later successful bind.
func TestUnixListenerRejectsForeignObjectsAndReleasesLocks(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	require.NoError(t, os.WriteFile(path, []byte("foreign"), 0o600))
	endpoint := owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: path}
	listener, err := openListener(t.Context(), endpoint)
	require.Nil(t, listener)
	require.ErrorContains(t, err, "non-socket")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(data))
	require.NoError(t, os.Remove(path))
	listener, err = openListener(t.Context(), endpoint)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	listener, err = openListener(t.Context(), owonrpc.Endpoint{Network: owonrpc.Network(99), Address: "invalid"})
	require.Nil(t, listener)
	require.ErrorContains(t, err, "endpoint network is unsupported")
	listener, err = openListener(t.Context(), owonrpc.Endpoint{Network: owonrpc.NetworkTCP, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	require.Error(t, validateUnixSocketParent(filepath.Join(path, "missing")))
	require.ErrorContains(t, setRawUnixListenerPermissions(nil, 0o660), "descriptor is nil")
}

// TestUnixListenerRejectsAnUnmanagedLiveSocket proves listener probing preserves external peers.
//
// Example: a live Unix listener without an owond lock must never be unlinked.
func TestUnixListenerRejectsAnUnmanagedLiveSocket(t *testing.T) {
	path := filepath.Join(unixListenerTestDir(t), "owond.sock")
	external, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(
		// closeExternal releases the unmanaged listener after ownership assertions.
		//
		// Example: the fixture socket is removed only by its own listener.
		func() { require.NoError(t, external.Close()) },
	)
	listener, err := openListener(t.Context(), owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: path})
	require.Nil(t, listener)
	require.ErrorContains(t, err, "already serving")
	connection, err := net.Dial("unix", path)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	lock, err := os.OpenFile(path+unixSocketLockSuffix, os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	require.NoError(t, unlockUnixSocket(lock))
	require.Error(t, unlockUnixSocket(lock))
}

// TestDaemonErrorsPreserveContextAndCauses verifies exported error chains.
//
// Example: errors.Is can identify an underlying socket failure without parsing text.
func TestDaemonErrorsPreserveContextAndCauses(t *testing.T) {
	cause := errors.New("underlying")
	for _, err := range []error{&ErrConfiguration{Cause: cause, Reason: "invalid"}, &ErrUnixListener{Cause: cause, Reason: "invalid"}} {
		require.ErrorIs(t, err, cause)
		require.Equal(t, "invalid: underlying", err.Error())
	}
	for _, err := range []error{(*ErrConfiguration)(nil), (*ErrUnixListener)(nil)} {
		require.Empty(t, err.Error())
		require.Nil(t, errors.Unwrap(err))
	}
}
