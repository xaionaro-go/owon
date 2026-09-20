package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

const (
	// cleanupMarker identifies the test's retained caller context value.
	//
	// Example: startup rejection must keep this value while detaching cancellation.
	cleanupMarker cleanupContextKey = iota
)

// startupBackend records cleanup generations independently of native USB discovery.
//
// Example: two successive cleanup attempts return an injected failure and then success.
type startupBackend struct {
	CloseErrors   []error
	Closes        atomic.Int32
	Exchanges     atomic.Int32
	CloseContexts chan cleanupContextRecord
}

// cleanupContextKey names the caller value that cleanup must preserve.
//
// Example: cancellation must not erase a cleanup operation's correlation value.
type cleanupContextKey int

// cleanupContextRecord captures context observations without retaining a context.
//
// Example: cleanup records its marker and cancellation state for the caller.
type cleanupContextRecord struct {
	Marker string
	Error  error
}

// Exchange returns deterministic identity for a composed service.
//
// Example: startup itself must not query again after physical identity validation.
func (backend *startupBackend) Exchange(
	_ context.Context,
	_ owonprotocol.Command,
) ([]byte, error) {
	backend.Exchanges.Add(1)
	return []byte("OWON,HDS2202S,serial,firmware"), nil
}

// ReopenAndValidate acknowledges the fixture's fixed identity without changing ownership.
//
// Example: this fixture never triggers recovery while testing construction.
func (*startupBackend) ReopenAndValidate(
	_ context.Context,
	_ owonmodel.SerialNumber,
) error {
	return nil
}

// Identity returns independently verified descriptor metadata.
//
// Example: the composed controller receives both identifiers without I/O.
func (*startupBackend) Identity() owonmodel.DeviceIdentity {
	return owonmodel.DeviceIdentity{VendorID: 1, ProductID: 2}
}

// Close records each attempt and returns its immutable scripted result.
//
// Example: a session retry invokes the backend again only after the previous generation returns.
func (backend *startupBackend) Close(ctx context.Context) error {
	if backend.CloseContexts != nil {
		marker, _ := ctx.Value(cleanupMarker).(string)
		backend.CloseContexts <- cleanupContextRecord{Marker: marker, Error: ctx.Err()}
	}
	index := int(backend.Closes.Add(1)) - 1
	if index < len(backend.CloseErrors) {
		return backend.CloseErrors[index]
	}
	return nil
}

// TestDaemonCleanupRetainsCallerValues separates ownership cleanup from cancellation.
//
// Example: session shutdown and either startup rejection retain the same caller marker.
func TestDaemonCleanupRetainsCallerValues(t *testing.T) {
	for _, stage := range []string{"backend", "session", "shutdown"} {
		ctx, cancel := context.WithCancel(context.WithValue(t.Context(), cleanupMarker, "caller"))
		cancel()
		backend := &startupBackend{CloseContexts: make(chan cleanupContextRecord, 1)}
		sessionConfig := owonsession.Config{ExpectedSerial: "serial"}
		serverConfig := owonserver.ServerConfig{}
		switch stage {
		case "backend":
			sessionConfig.ExpectedSerial = ""
		case "session":
			serverConfig.MaxActiveSubscriptions = -1
		}
		_, session, err := composeService(ctx, backend, sessionConfig, serverConfig)
		if stage == "shutdown" {
			require.NoError(t, err)
			require.NoError(t, closeSession(ctx, session))
		} else {
			require.Error(t, err)
			require.Nil(t, session)
		}
		record := <-backend.CloseContexts
		require.Equal(t, "caller", record.Marker, stage)
		require.NoError(t, record.Error, stage)
		require.EqualValues(t, 1, backend.Closes.Load(), stage)
	}
}

// TestCompositionRetainsCleanupAcrossHandoffs exercises both physical and session-owned startup failure.
//
// Example: a failed cleanup remains retryable regardless of which constructor rejected startup.
func TestCompositionRetainsCleanupAcrossHandoffs(t *testing.T) {
	for _, beforeSession := range []bool{true, false} {
		closeCause := errors.New("device retained")
		backend := &startupBackend{CloseErrors: []error{closeCause}}
		sessionConfig := owonsession.Config{ExpectedSerial: "serial"}
		serverConfig := owonserver.ServerConfig{}
		if beforeSession {
			sessionConfig.ExpectedSerial = ""
		} else {
			serverConfig.MaxActiveSubscriptions = -1
		}
		service, session, err := composeService(t.Context(), backend, sessionConfig, serverConfig)
		require.Nil(t, service)
		require.Nil(t, session)
		require.ErrorIs(t, err, closeCause)
		var retained *ErrStartup
		require.ErrorAs(t, err, &retained)
		require.NotNil(t, retained.owner)
		require.EqualValues(t, 1, backend.Closes.Load())
		require.Zero(t, backend.Exchanges.Load())
		require.NoError(t, retained.Close(t.Context()))
		require.Nil(t, retained.owner)
		require.EqualValues(t, 2, backend.Closes.Load())
		require.NoError(t, retained.Close(t.Context()))
		require.EqualValues(t, 2, backend.Closes.Load())
	}
}

// TestCompositionPublishesOnlyCompleteOwners verifies successful handoff and failures needing no retained cleanup.
//
// Example: after success only the returned session owns physical cleanup.
func TestCompositionPublishesOnlyCompleteOwners(t *testing.T) {
	backend := new(startupBackend)
	service, session, err := composeService(t.Context(), backend, owonsession.Config{ExpectedSerial: "serial"}, owonserver.ServerConfig{})
	require.NoError(t, err)
	require.NotNil(t, service)
	require.NotNil(t, session)
	require.Zero(t, backend.Closes.Load())
	require.Zero(t, backend.Exchanges.Load())
	require.NoError(t, session.CloseContext(t.Context()))
	require.EqualValues(t, 1, backend.Closes.Load())
	backend = new(startupBackend)
	_, _, err = composeService(t.Context(), backend, owonsession.Config{}, owonserver.ServerConfig{})
	var invalid *owonsession.ErrInvalidConfig
	require.ErrorAs(t, err, &invalid)
	var retained *ErrStartup
	require.False(t, errors.As(err, &retained))
	require.EqualValues(t, 1, backend.Closes.Load())
	_, _, err = composeService(t.Context(), nil, owonsession.Config{}, owonserver.ServerConfig{})
	require.Error(t, err)
}

// TestKeepAwakeStartupFailureCleansSession verifies policy failure before listener startup retains ownership cleanup.
//
// Example: a malformed timer response returns an error and the session closes exactly once.
func TestKeepAwakeStartupFailureCleansSession(t *testing.T) {
	backend := new(startupBackend)
	service, session, err := composeService(t.Context(), backend, owonsession.Config{ExpectedSerial: "serial"}, owonserver.ServerConfig{})
	require.NoError(t, err)
	policyErr := service.EnsureKeepAwake(t.Context())
	require.Error(t, policyErr)
	require.Zero(t, backend.Closes.Load())
	require.ErrorIs(t, rejectStartup(t.Context(), sessionCleanup{Session: session}, policyErr), policyErr)
	require.EqualValues(t, 1, backend.Closes.Load())
}

// TestDaemonRejectsNilContextBeforeLogging keeps invalid calls from panicking or losing owners.
//
// Example: rejecting cleanup with a nil context leaves the valid session available to close.
func TestDaemonRejectsNilContextBeforeLogging(t *testing.T) {
	backend := new(startupBackend)
	_, _, err := composeService(nil, backend, owonsession.Config{ExpectedSerial: "serial"}, owonserver.ServerConfig{})
	require.ErrorContains(t, err, "context")
	require.Zero(t, backend.Closes.Load())
	_, session, err := composeService(t.Context(), backend, owonsession.Config{ExpectedSerial: "serial"}, owonserver.ServerConfig{})
	require.NoError(t, err)
	require.ErrorContains(t, closeSession(nil, session), "context")
	require.Zero(t, backend.Closes.Load())
	require.NoError(t, closeSession(t.Context(), session))
	require.EqualValues(t, 1, backend.Closes.Load())
	listener, err := openListener(nil, options{}.Endpoint)
	require.Nil(t, listener)
	require.ErrorContains(t, err, "context")
	require.ErrorContains(t, serveGRPC(nil, nil, options{}), "context")
}
