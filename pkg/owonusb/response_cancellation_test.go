package owonusb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/gousb"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owoncontrol"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"github.com/xaionaro-go/owon/pkg/owonsession"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cancellationEndpoint returns native failures at a selected context boundary.
//
// Example: a quiet probe can return surplus bytes and an independent failure together.
type cancellationEndpoint struct {
	Cause  error
	Wait   bool
	Cancel context.CancelFunc
	Data   []byte
}

// ReadContext models one native completion without discarding its original cause.
//
// Example: Wait makes the transfer complete only after its operation deadline.
func (endpoint cancellationEndpoint) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	if endpoint.Cancel != nil {
		endpoint.Cancel()
	}
	if endpoint.Wait {
		<-ctx.Done()
	}
	return copy(destination, endpoint.Data), endpoint.Cause
}

// WriteContext returns the same controlled completion on the output boundary.
//
// Example: native cancellation after a partial write remains an ambiguous failure.
func (endpoint cancellationEndpoint) WriteContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	return endpoint.ReadContext(ctx, destination)
}

// TestEndpointCancellationPreservesCauses verifies native and context identities on both boundaries.
//
// Example: a joined endpoint failure must survive normalization to DeadlineExceeded.
func TestEndpointCancellationPreservesCauses(t *testing.T) {
	synctest.Test(t,
		// verifyCancellation tests canceled, expired and live callers without wall-clock waits.
		//
		// Example: an unrelated native failure is not relabeled merely because its caller ended.
		func(t *testing.T) {
			independent := errors.New("endpoint failed")
			for _, ended := range []string{"live", "canceled", "deadline"} {
				for _, cause := range []error{gousb.TransferCancelled, fmt.Errorf("native: %w", gousb.TransferCancelled), independent, errors.Join(gousb.TransferCancelled, independent)} {
					verifyEndpointCancellation(t, ended, cause)
				}
			}
		})
}

// verifyEndpointCancellation compares input and output normalization for one native result.
//
// Example: a live-context TransferCancelled retains native identity without inventing cancellation.
func verifyEndpointCancellation(
	t *testing.T,
	ended string,
	cause error,
) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	switch ended {
	case "canceled":
		cancel()
	case "deadline":
		<-ctx.Done()
	}
	endpoint := cancellationEndpoint{Cause: cause}
	session, err := newEndpointSession(endpoint, endpoint, 1024)
	require.NoError(t, err)
	want := codes.Canceled
	if ended == "deadline" {
		want = codes.DeadlineExceeded
	}
	for _, err := range []error{session.fill(ctx), writeAll(ctx, endpoint, []byte("X"))} {
		require.ErrorIs(t, err, cause)
		if ctx.Err() != nil && errors.Is(cause, gousb.TransferCancelled) {
			require.ErrorIs(t, err, ctx.Err())
			deviceSession, sessionErr := owonsession.New(&cancellationErrorBackend{Cause: err}, owonsession.Config{ExpectedSerial: "fixture"})
			require.NoError(t, sessionErr)
			instrument, instrumentErr := owoncontrol.New(deviceSession, owonmodel.DeviceIdentity{})
			require.NoError(t, instrumentErr)
			server, serverErr := owonserver.NewServer(instrument)
			require.NoError(t, serverErr)
			_, rpcErr := server.GetDeviceInfo(context.Background(), nil)
			require.Equal(t, want, status.Code(rpcErr))
			require.NoError(t, deviceSession.Close())
			continue
		}
		require.NotErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, context.DeadlineExceeded)
	}
}

// cancellationErrorBackend carries an already observed native failure across the public server boundary.
//
// Example: a wrapped native deadline retains its DeadlineExceeded gRPC classification.
type cancellationErrorBackend struct{ Cause error }

// Exchange returns the captured endpoint failure without replacing any of its causes.
//
// Example: the production server classifies the same joined error inspected by the native test.
func (backend *cancellationErrorBackend) Exchange(
	_ context.Context,
	_ owonprotocol.Command,
) ([]byte, error) {
	return nil, backend.Cause
}

// ReopenAndValidate has no resources to reopen for this single-failure fixture.
//
// Example: the one request under test never enters recovery.
func (*cancellationErrorBackend) ReopenAndValidate(
	context.Context,
	owonmodel.SerialNumber,
) error {
	return nil
}

// Close releases no resources because the fixture stores only an error.
//
// Example: session cleanup completes synchronously after classification.
func (*cancellationErrorBackend) Close(context.Context) error { return nil }

// TestQuietProbePreservesIndependentCauses rejects mixed timeouts and keeps surplus diagnostics.
//
// Example: only a single-cause own-probe timeout is proof of quiet.
func TestQuietProbePreservesIndependentCauses(t *testing.T) {
	synctest.Test(t,
		// verifyQuietCases advances probe timers virtually and checks parent cancellation separately.
		//
		// Example: a canceled parent plus an endpoint failure exposes both identities.
		func(t *testing.T) {
			independent := errors.New("independent endpoint failure")
			for _, test := range []struct {
				Cause  error
				Cancel bool
				Data   []byte
				Quiet  bool
			}{
				{Cause: gousb.TransferCancelled, Quiet: true},
				{Cause: fmt.Errorf("wrapped: %w", gousb.TransferCancelled), Quiet: true},
				{Cause: context.DeadlineExceeded, Quiet: true},
				{Cause: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), Quiet: true},
				{Cause: errors.Join(gousb.TransferCancelled, independent)},
				{Cause: errors.Join(context.DeadlineExceeded, independent)},
				{Cause: fmt.Errorf("wrapped: %w", errors.Join(gousb.TransferCancelled, independent))},
				{Cause: errors.Join(gousb.TransferCancelled, context.DeadlineExceeded)},
				{Cause: errors.Join(gousb.TransferCancelled)},
				{Cause: context.Canceled},
				{Cause: io.EOF},
				{Cause: independent, Cancel: true},
				{Cause: independent, Cancel: true, Data: []byte("X")},
				{Cause: independent, Data: []byte("X")},
				{Cause: errors.Join(gousb.TransferCancelled, independent), Cancel: true, Data: []byte("X")},
			} {
				ctx, cancel := context.WithCancel(context.Background())
				endpoint := cancellationEndpoint{Cause: test.Cause, Wait: !test.Cancel, Data: test.Data}
				if test.Cancel {
					endpoint.Cancel = cancel
				}
				session, err := newEndpointSession(endpoint, new(endpointWriter), 1024)
				require.NoError(t, err)
				err = session.verifyNoResponse(ctx)
				cancel()
				if test.Quiet {
					require.NoError(t, err)
					continue
				}
				require.ErrorIs(t, err, test.Cause)
				if test.Cancel {
					require.ErrorIs(t, err, context.Canceled)
				}
				if len(test.Data) != 0 {
					requireErrorType[*owonprotocol.ErrSurplusResponse](t, err)
				}
			}
		})
}

// TestEndpointCompletedFrameRetainsMixedCancellation rejects an EOF sibling hiding a failed transfer.
//
// Example: a complete line returned with native cancellation still preserves cancellation and device errors.
func TestEndpointCompletedFrameRetainsMixedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	independent := errors.New("device failed with bytes")
	cause := errors.Join(gousb.TransferCancelled, io.EOF, independent)
	endpoint := cancellationEndpoint{Cause: cause, Data: []byte("OK\n"), Cancel: cancel}
	session, err := newEndpointSession(endpoint, new(endpointWriter), 1024)
	require.NoError(t, err)
	response, err := session.Exchange(ctx, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.Nil(t, response)
	require.ErrorIs(t, err, gousb.TransferCancelled)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, independent)
	require.ErrorIs(t, err, io.EOF)
}

// TestEndpointCompletedFrameDefersSingleEOF preserves buffered response data at a clean EOF boundary.
//
// Example: plain and ordinarily wrapped EOF permit the complete line but reject a subsequent command.
func TestEndpointCompletedFrameDefersSingleEOF(t *testing.T) {
	for _, cause := range []error{io.EOF, fmt.Errorf("endpoint ended: %w", io.EOF)} {
		reader := cancellationEndpoint{Cause: cause, Data: []byte("OK\n")}
		writer := new(endpointWriter)
		session, err := newEndpointSession(reader, writer, 1024)
		require.NoError(t, err)
		command := owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}
		response, err := session.Exchange(t.Context(), command)
		require.NoError(t, err)
		require.Equal(t, []byte("OK"), response)
		response, err = session.Exchange(t.Context(), command)
		require.Nil(t, response)
		require.ErrorIs(t, err, cause)
		requireErrorType[*owonprotocol.ErrSurplusResponse](t, err)
		require.Equal(t, []byte("*IDN?\n"), writer.Data)
	}
}

// TestQuietProbeParentDeadlineRetainsEndpointFailure distinguishes an earlier operation budget from quiet.
//
// Example: a parent deadline ending before five milliseconds preserves both native and context causes.
func TestQuietProbeParentDeadlineRetainsEndpointFailure(t *testing.T) {
	synctest.Test(t,
		// expireParent makes the earlier caller deadline observable without real-time sleeps.
		//
		// Example: an independent endpoint failure cannot be erased by the earlier deadline.
		func(t *testing.T) {
			for _, cause := range []error{gousb.TransferCancelled, errors.New("endpoint failed at parent deadline")} {
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
				reader := cancellationEndpoint{Cause: cause, Wait: true}
				session, err := newEndpointSession(reader, new(endpointWriter), 1024)
				require.NoError(t, err)
				err = session.verifyNoResponse(ctx)
				cancel()
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.ErrorIs(t, err, cause)
			}
		},
	)
}
