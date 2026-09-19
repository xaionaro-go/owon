package main

import (
	"context"
	"errors"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owoncontrol"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// instrumentBackend is the physical ownership and verified metadata needed for daemon composition.
//
// Example: the USB backend satisfies it without constructing any higher-level owner.
type instrumentBackend interface {
	owonsession.Backend
	// Identity returns descriptor metadata without live I/O.
	//
	// Example: USB vendor and product values enrich queried device identity.
	Identity() owonmodel.DeviceIdentity
}

// startupCleanup is a retryable resource owner retained when startup cleanup fails.
//
// Example: before session construction this is the physical backend itself.
type startupCleanup interface {
	// Close requests cleanup while preserving ownership until native work completes.
	//
	// Example: cancellation may end the caller's wait without losing the retained owner.
	Close(context.Context) error
}

// sessionCleanup translates daemon cleanup into the session's cancellation-aware wait operation.
//
// Example: startup after session construction never closes the backend behind its session.
type sessionCleanup struct{ Session *owonsession.Session }

// Close waits for the session-owned cleanup generation using the supplied context.
//
// Example: a caller can retry waiting after a prior cleanup deadline.
func (cleanup sessionCleanup) Close(ctx context.Context) error {
	return cleanup.Session.CloseContext(ctx)
}

// ErrStartup retains the sole cleanup owner when construction and cleanup both fail.
//
// Example: a supervisor can retry Close after a native device handle refused release.
type ErrStartup struct {
	Cause error
	mu    sync.Mutex
	owner startupCleanup
}

// Error reports the failed construction and initial cleanup causes.
//
// Example: session configuration failure remains visible with a simultaneous USB close error.
func (err *ErrStartup) Error() string {
	if err == nil || err.Cause == nil {
		return "OWON startup failed"
	}
	return "OWON startup failed: " + err.Cause.Error()
}

// Unwrap preserves construction and cleanup error matching.
//
// Example: errors.As still finds a server configuration rejection.
func (err *ErrStartup) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Close retries the retained owner and clears it only after successful release.
//
// Example: a second startup attempt cannot accidentally discard a still-owned device.
func (err *ErrStartup) Close(ctx context.Context) error {
	if err == nil {
		return nil
	}
	err.mu.Lock()
	defer err.mu.Unlock()
	if err.owner == nil {
		return nil
	}
	if closeErr := err.owner.Close(ctx); closeErr != nil {
		return closeErr
	}
	err.owner = nil
	return nil
}

// rejectStartup cleans the current owner and retains it if cleanup did not complete successfully.
//
// Example: backend ownership transfers to a typed error when session construction fails.
func rejectStartup(
	ctx context.Context,
	owner startupCleanup,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := owner.Close(ctx); err != nil {
		return &ErrStartup{Cause: errors.Join(cause, err), owner: owner}
	}
	return cause
}

// composeService hands physical ownership to a session, then assembles controller and RPC policy.
//
// Example: every rejected constructor cleans the owner acquired so far, retaining it on cleanup failure.
func composeService(
	ctx context.Context,
	backend instrumentBackend,
	sessionConfig owonsession.Config,
	serverConfig owonserver.ServerConfig,
) (_server *owonserver.Server, _session *owonsession.Session, _err error) {
	if ctx == nil {
		return nil, nil, &ErrConfiguration{Field: "context", Reason: "must not be nil"}
	}
	logger.Tracef(ctx, "composeService")
	// traceComposition records constructor failure while preserving its cleanup owner.
	//
	// Example: a rejected subscription limit remains visible after backend cleanup.
	defer func() { logger.Tracef(ctx, "/composeService: %v", _err) }()
	session, err := owonsession.New(backend, sessionConfig)
	if err != nil {
		if backend == nil {
			return nil, nil, err
		}
		return nil, nil, rejectStartup(ctx, backend, err)
	}
	controller, err := owoncontrol.New(session, backend.Identity())
	if err != nil {
		return nil, nil, rejectStartup(ctx, sessionCleanup{Session: session}, err)
	}
	server, err := owonserver.NewServerWithConfig(controller, serverConfig)
	if err != nil {
		return nil, nil, rejectStartup(ctx, sessionCleanup{Session: session}, err)
	}
	return server, session, nil
}
