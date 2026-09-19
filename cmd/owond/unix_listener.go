package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

const (
	// unixSocketProbeTimeout bounds active-listener detection before stale cleanup.
	//
	// Example: a wedged peer cannot make daemon startup wait indefinitely.
	unixSocketProbeTimeout = 100 * time.Millisecond
	// unixSocketLockSuffix serializes daemon ownership of one Unix endpoint path.
	//
	// Example: a second owond cannot unlink the first daemon's live socket.
	unixSocketLockSuffix = ".lock"
	// unixSocketMode grants endpoint access only to its owner and selected group.
	//
	// Example: the socket and its ownership lock remain inaccessible to other users.
	unixSocketMode = 0o660
	// unixSocketDirectoryMode permits group traversal without allowing group replacement.
	//
	// Example: a daemon-created runtime directory holds the group-accessible socket safely.
	unixSocketDirectoryMode = 0o750
	// privateUnixSocketDirectoryMode restricts the per-user default to its effective owner.
	//
	// Example: a fallback socket under /tmp remains inaccessible to other users and groups.
	privateUnixSocketDirectoryMode = 0o700
)

// unixListenerPermissions applies one mode to a raw Unix listener descriptor.
//
// Example: net.ListenConfig invokes apply before binding the socket path.
type unixListenerPermissions struct {
	Mode   uint32
	Result *error
}

// fileInfoInspector reads the identity of a filesystem path during listener setup.
//
// Example: ownBoundUnixListener uses os.Lstat to avoid removing a replacement inode.
type fileInfoInspector func(string) (os.FileInfo, error)

// apply records the descriptor chmod result for the enclosing control call.
//
// Example: setRawUnixListenerPermissions checks the recorded error after Control returns.
func (permissions unixListenerPermissions) apply(fd uintptr) {
	*permissions.Result = syscall.Fchmod(int(fd), permissions.Mode)
}

// closeListener closes a listener while treating an already-closed socket as success.
//
// Example: a Serve failure path releases the Unix socket before closing USB.
func closeListener(listener net.Listener) error {
	if listener == nil {
		return nil
	}
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close listener: %w", err)
	}

	return nil
}

// setRawUnixListenerPermissions changes socket mode through a raw descriptor.
//
// Example: a ListenConfig control callback applies the mode before bind creates the pathname inode.
func setRawUnixListenerPermissions(
	rawConnection syscall.RawConn,
	mode uint32,
) error {
	if rawConnection == nil {
		return &ErrUnixListener{
			Operation: "set Unix listener permissions",
			Reason:    "descriptor is nil",
		}
	}
	var chmodErr error
	if err := rawConnection.Control(unixListenerPermissions{Mode: mode, Result: &chmodErr}.apply); err != nil {
		return fmt.Errorf("control Unix listener descriptor: %w", err)
	}
	if chmodErr != nil {
		return fmt.Errorf("chmod Unix listener descriptor: %w", chmodErr)
	}

	return nil
}

// setUnixListenerPermissionsControl applies a restrictive pre-bind mode to a Unix listener descriptor.
//
// Example: net.ListenConfig invokes it before bind creates the socket pathname.
func setUnixListenerPermissionsControl(
	_ string,
	_ string,
	rawConnection syscall.RawConn,
) error {
	return setRawUnixListenerPermissions(rawConnection, unixSocketMode)
}

// validateUnixSocketParent verifies the complete parent directory chain for a Unix endpoint.
//
// Example: /tmp is accepted for a test socket because its sticky bit protects peer removal.
func validateUnixSocketParent(path string) error {
	paths := []string{filepath.Clean(path)}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve Unix socket parent %q: %w", path, err)
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if resolvedPath != paths[0] {
		paths = append(paths, resolvedPath)
	}
	checked := make(map[string]struct{}, len(paths))
	for _, root := range paths {
		if err := validateUnixSocketParentChain(root, checked); err != nil {
			return err
		}
	}

	return nil
}

// validateUnixSocketParentChain checks one lexical or resolved chain until it meets a checked ancestor.
//
// Example: a symlink's lexical parents and target parents share checks without skipping either boundary.
func validateUnixSocketParentChain(
	path string,
	checked map[string]struct{},
) error {
	for current := path; ; current = filepath.Dir(current) {
		if _, alreadyChecked := checked[current]; alreadyChecked {
			return nil
		}
		checked[current] = struct{}{}
		info, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("inspect Unix socket parent %q: %w", current, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("Unix socket parent %q is not a directory", current)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("unsafe Unix socket parent %q: group/other-writable directory lacks sticky bit", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

// ownBoundUnixListener captures endpoint identity and enables owned-path cleanup.
//
// Example: setup keeps unlink-on-close enabled until identity capture succeeds.
func ownBoundUnixListener(
	listener *net.UnixListener,
	path string,
	lock *os.File,
	inspect fileInfoInspector,
) (net.Listener, error) {
	identity, identityErr := inspect(path)
	if identityErr != nil {
		closeErr := listener.Close()
		return nil, errors.Join(fmt.Errorf("inspect created Unix socket: %w", identityErr), closeErr, unlockUnixSocket(lock))
	}
	listener.SetUnlinkOnClose(false)

	return &ownedUnixListener{Listener: listener, path: path, identity: identity, lock: lock}, nil
}

// ownedUnixListener removes only the socket inode created by this daemon.
//
// Example: a replaced path is never removed during shutdown.
type ownedUnixListener struct {
	net.Listener
	path      string
	identity  os.FileInfo
	lock      *os.File
	closeOnce sync.Once
	closeErr  error
}

// Close stops the listener, unlinks its owned socket, and releases the endpoint lock.
//
// Example: repeated shutdown paths return the same cleanup result without double-closing.
func (listener *ownedUnixListener) Close() error {
	if listener == nil {
		return nil
	}
	listener.closeOnce.Do(listener.closeOnceRun)

	return listener.closeErr
}

// closeOnceRun stores the one-time listener cleanup result for sync.Once.
//
// Example: repeated Close calls reuse the result from the first cleanup attempt.
func (listener *ownedUnixListener) closeOnceRun() {
	listener.closeErr = listener.closeOwned()
}

// closeOwned performs the one-time Unix listener cleanup in ownership order.
//
// Example: failed listener closure leaves the pathname in place for diagnosis.
func (listener *ownedUnixListener) closeOwned() error {
	listenerErr := listener.Listener.Close()
	var errs []error
	listenerClosed := listenerErr == nil || errors.Is(listenerErr, net.ErrClosed)
	if !listenerClosed {
		errs = append(errs, fmt.Errorf("close Unix listener: %w", listenerErr))
	}
	if listenerClosed && listener.identity != nil {
		errs = append(errs, listener.removeOwnedSocket())
	}
	errs = append(errs, unlockUnixSocket(listener.lock))

	return errors.Join(errs...)
}

// removeOwnedSocket removes a stopped listener's path only when its captured inode still matches.
//
// Example: an external replacement survives shutdown; an already removed path needs no further cleanup.
func (listener *ownedUnixListener) removeOwnedSocket() error {
	current, err := os.Lstat(listener.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect owned Unix socket: %w", err)
	case !os.SameFile(listener.identity, current):
		return fmt.Errorf("refuse to remove replaced Unix socket path %q", listener.path)
	}
	if err := os.Remove(listener.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Unix socket: %w", err)
	}

	return nil
}

// openListener creates the configured listener without replacing unrelated filesystem objects.
//
// Example: a stale Unix socket is removed, while a regular file at that path is preserved and rejected.
func openListener(
	ctx context.Context,
	endpoint owonrpc.Endpoint,
) (_listener net.Listener, _err error) {
	if ctx == nil {
		return nil, &ErrConfiguration{Field: "context", Reason: "must not be nil"}
	}
	logger.Tracef(ctx, "openListener")
	// traceListener records binding errors without claiming readiness.
	//
	// Example: an occupied port reports its failure before any listening message.
	defer func() { logger.Tracef(ctx, "/openListener: %v", _err) }()
	if _, err := endpoint.RequiresTLS(); err != nil {
		return nil, err
	}
	var lock *os.File
	if endpoint.Network == owonrpc.NetworkUnix {
		var err error
		lock, err = acquireUnixSocketPath(endpoint.Address)
		if err != nil {
			return nil, err
		}
	}
	listenConfig := net.ListenConfig{}
	if endpoint.Network == owonrpc.NetworkUnix {
		listenConfig.Control = setUnixListenerPermissionsControl
	}
	listener, err := listenConfig.Listen(ctx, endpoint.Network.String(), endpoint.Address)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("listen on %s %s: %w", endpoint.Network, endpoint.Address, err), unlockUnixSocket(lock))
	}
	if endpoint.Network != owonrpc.NetworkUnix {
		return listener, nil
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		closeErr := listener.Close()
		return nil, errors.Join(&ErrUnixListener{
			Operation: "open Unix listener",
			Reason:    "endpoint did not create a Unix listener",
		}, closeErr, unlockUnixSocket(lock))
	}
	if err := os.Chmod(endpoint.Address, unixSocketMode); err != nil {
		closeErr := listener.Close()
		return nil, errors.Join(fmt.Errorf("chmod Unix socket path: %w", err), closeErr, unlockUnixSocket(lock))
	}

	return ownBoundUnixListener(unixListener, endpoint.Address, lock, os.Lstat)
}

// acquireUnixSocketPath validates the parent, locks the endpoint, and clears only an inactive socket.
//
// Example: binding starts only after the daemon holds the lock and rejects any live or foreign path.
func acquireUnixSocketPath(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	directoryMode := os.FileMode(unixSocketDirectoryMode)
	target, err := (owonrpc.Endpoint{Network: owonrpc.NetworkUnix, Address: path}).Target()
	if err != nil {
		return nil, err
	}
	private := target == owonrpc.DefaultEndpoint()
	if private {
		directoryMode = privateUnixSocketDirectoryMode
	}
	if err := os.MkdirAll(parent, directoryMode); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if private {
		if err := validatePrivateUnixSocketDirectory(parent); err != nil {
			return nil, err
		}
	}
	if err := validateUnixSocketParent(parent); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+unixSocketLockSuffix, os.O_CREATE|os.O_RDWR, unixSocketMode)
	if err != nil {
		return nil, fmt.Errorf("open Unix socket lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.Join(&ErrUnixListener{
				Operation: "open Unix listener",
				Reason:    "socket is already owned by another owond",
			}, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("lock Unix socket: %w", err), closeErr)
	}
	if err := prepareUnixSocketPath(path); err != nil {
		return nil, errors.Join(err, unlockUnixSocket(lock))
	}

	return lock, nil
}

// validatePrivateUnixSocketDirectory rejects existing shared or foreign default directories without modifying them.
//
// Example: a pre-existing /tmp/owon-1000 must be a real directory owned by uid 1000 with mode 0700.
func validatePrivateUnixSocketDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private socket directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != privateUnixSocketDirectoryMode || !ok || int(stat.Uid) != os.Geteuid() {
		return &ErrUnixListener{
			Operation: "validate private socket directory",
			Reason:    fmt.Sprintf("%q must be a directory owned by uid %d with mode 0700", path, os.Geteuid()),
		}
	}

	return nil
}

// prepareUnixSocketPath preserves live or foreign paths and removes only an inactive socket.
//
// Example: a crashed daemon's socket is removed after connection refusal, while probe errors abort startup.
func prepareUnixSocketPath(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect Unix socket path: %w", err)
	case info.Mode()&os.ModeSocket == 0:
		return fmt.Errorf("refuse to replace non-socket path %q", path)
	}
	probe, probeErr := net.DialTimeout("unix", path, unixSocketProbeTimeout)
	if probeErr == nil {
		return errors.Join(&ErrUnixListener{
			Operation: "open Unix listener",
			Reason:    "socket is already serving",
		}, probe.Close())
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) && !errors.Is(probeErr, syscall.ENOENT) {
		return fmt.Errorf("probe existing Unix socket: %w", probeErr)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket: %w", err)
	}

	return nil
}

// unlockUnixSocket releases and closes an endpoint lock after setup failure or listener shutdown.
//
// Example: a chmod or bind error never leaves the endpoint lock held.
func unlockUnixSocket(lock *os.File) error {
	if lock == nil {
		return nil
	}
	var errs []error
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		errs = append(errs, fmt.Errorf("unlock Unix socket: %w", err))
	}
	if err := lock.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close Unix socket lock: %w", err))
	}

	return errors.Join(errs...)
}

// ErrUnixListener identifies a Unix listener lifecycle failure.
//
// Example: a stale socket path reports its ownership decision through this type.
type ErrUnixListener struct {
	Operation string
	Reason    string
	Cause     error
}

// Error returns the Unix listener operation and failure reason.
//
// Example: `open Unix listener: socket is already serving` is operator-readable.
func (err *ErrUnixListener) Error() string {
	if err == nil {
		return ""
	}
	detail := err.Reason
	if err.Cause != nil {
		detail = fmt.Sprintf("%s: %v", detail, err.Cause)
	}
	if err.Operation == "" {
		return detail
	}

	return fmt.Sprintf("%s: %s", err.Operation, detail)
}

// Unwrap returns an underlying filesystem or socket failure.
//
// Example: callers can inspect a lock failure with errors.As.
func (err *ErrUnixListener) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
