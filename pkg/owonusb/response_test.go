package owonusb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/gousb"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestEndpointSessionLengthFrames verifies fragmentation, bounds, truncation and surplus ownership.
//
// Example: a four-byte length split across transfers yields exactly its payload and rejects a second reply.
func TestEndpointSessionLengthFrames(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Chunks  [][]byte
		Want    []byte
		Failure string
	}{
		{[][]byte{{3, 0}, {0, 0, 'a'}, {'b', 'c'}}, []byte("abc"), ""},
		{[][]byte{{0, 0, 0, 0}}, nil, ""},
		{[][]byte{{3, 0}}, nil, "length prefix"},
		{[][]byte{{3, 0, 0, 0, 'a'}}, nil, "payload"},
		{[][]byte{{5, 0, 0, 0}}, nil, "configured limit"},
		{[][]byte{{1, 0, 0, 0, 'a', 'b'}}, nil, "surplus"},
	} {
		session, err := newEndpointSession(&endpointReader{Chunks: testCase.Chunks}, new(endpointWriter), 4)
		require.NoError(t, err)
		response, err := session.Exchange(t.Context(), owonprotocol.Command{Text: ":DATA?", ResponseMode: owonprotocol.ResponseModeLengthPrefixed})
		if testCase.Failure != "" {
			require.ErrorContains(t, err, testCase.Failure)
			require.Nil(t, response)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, testCase.Want, response)
		require.Empty(t, session.buffer)
	}
}

// TestResponseLimitRejectsValuesAboveHardCap verifies configuration cannot enlarge allocation.
//
// Example: zero, one, and sixteen MiB are allowed while max+1 and MaxUint32 fail.
func TestResponseLimitRejectsValuesAboveHardCap(t *testing.T) {
	t.Parallel()

	for _, maximum := range []uint32{0, 1, owonprotocol.DefaultMaximumResponseBytes} {
		_, err := owonprotocol.ReadResponse(bytes.NewReader([]byte("\n")), owonprotocol.ResponseModeASCII, maximum)
		require.NoError(t, err)
		_, err = newEndpointSession(new(endpointReader), new(endpointWriter), maximum)
		require.NoError(t, err)
	}
	for _, maximum := range []uint32{owonprotocol.DefaultMaximumResponseBytes + 1, ^uint32(0)} {
		_, err := owonprotocol.ReadResponse(bytes.NewReader([]byte("\n")), owonprotocol.ResponseModeASCII, maximum)
		requireErrorType[*owonprotocol.ErrInvalidResponseLimit](t, err)
		_, err = newEndpointSession(new(endpointReader), new(endpointWriter), maximum)
		requireErrorType[*owonprotocol.ErrInvalidResponseLimit](t, err)
	}
}

// TestEndpointSessionHandlesShortTransfers verifies exact command writes and fragmented replies.
//
// Example: one-byte writes and split reads still form one complete transaction.
func TestEndpointSessionHandlesShortTransfers(t *testing.T) {
	t.Parallel()

	reader := &endpointReader{Chunks: [][]byte{[]byte("OWON,"), []byte("HDS2202S\n")}}
	writer := &endpointWriter{MaximumWrite: 1}
	session, err := newEndpointSession(reader, writer, 1024)
	require.NoError(t, err)

	response, err := session.Exchange(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	require.NoError(t, err)
	require.Equal(t, []byte("OWON,HDS2202S"), response)
	require.Equal(t, []byte("*IDN?\n"), writer.Data)
}

// TestEndpointSessionRejectsNilContext verifies direct session callers cannot panic.
//
// Example: a malformed integration call receives a typed unavailable error before USB I/O.
func TestEndpointSessionRejectsNilContext(t *testing.T) {
	t.Parallel()

	session, err := newEndpointSession(new(endpointReader), new(endpointWriter), 1024)
	require.NoError(t, err)
	_, err = session.Exchange(nil, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	requireErrorType[*ErrUnavailable](t, err)
}

// TestEndpointSessionRejectsSurplusReply verifies stale bytes cannot satisfy a later request.
//
// Example: `first\nsecond\n` returned for one query is an ambiguous protocol failure.
func TestEndpointSessionRejectsSurplusReply(t *testing.T) {
	t.Parallel()

	reader := &endpointReader{Chunks: [][]byte{[]byte("first\nsecond\n")}}
	session, err := newEndpointSession(reader, &endpointWriter{}, 1024)
	require.NoError(t, err)

	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	var surplus *owonprotocol.ErrSurplusResponse
	require.ErrorAs(t, err, &surplus)
	require.Equal(t, 7, surplus.Bytes)
}

// TestEndpointSessionRejectsUnexpectedNoResponseBytes verifies setter replies poison the transaction.
//
// Example: an undocumented `OK\n` following a no-response command is never attributed to a later query.
func TestEndpointSessionRejectsUnexpectedNoResponseBytes(t *testing.T) {
	t.Parallel()

	reader := &immediateEndpointReader{Chunks: [][]byte{[]byte("OK\n")}}
	session, err := newEndpointSession(reader, &endpointWriter{}, 1024)
	require.NoError(t, err)

	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	var surplus *owonprotocol.ErrSurplusResponse
	require.ErrorAs(t, err, &surplus)
	require.Equal(t, 3, surplus.Bytes)
}

// TestEndpointSessionTreatsEOFAsDisconnect verifies EOF cannot prove no response.
//
// Example: a disconnected endpoint poisons a no-response setter transaction.
func TestEndpointSessionTreatsEOFAsDisconnect(t *testing.T) {
	t.Parallel()

	session, err := newEndpointSession(new(immediateEndpointReader), new(endpointWriter), 1024)
	require.NoError(t, err)
	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	require.ErrorIs(t, err, io.EOF)
}

// TestEndpointSessionAcceptsGousbProbeCancellation verifies the native timeout contract.
//
// Example: libusb's TransferCancelled proves only the probe's own quiet period expired.
func TestEndpointSessionAcceptsGousbProbeCancellation(t *testing.T) {
	t.Parallel()

	reader := new(probeCancellationReader)
	session, err := newEndpointSession(reader, new(endpointWriter), 1024)
	require.NoError(t, err)
	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	require.NoError(t, err)
}

// TestEndpointSessionMapsGousbCancellationToRequestContext verifies cancellation status is preserved.
//
// Example: an in-flight gousb cancellation becomes context.Canceled for gRPC mapping.
func TestEndpointSessionMapsGousbCancellationToRequestContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reader := &requestCancellationReader{Started: make(chan struct{})}
	session, err := newEndpointSession(reader, new(endpointWriter), 1024)
	require.NoError(t, err)
	result := make(chan error, 1)
	observability.Go(ctx, endpointExchangeRunner{Session: session, Result: result}.Run)
	<-reader.Started
	cancel()
	err = <-result
	require.ErrorIs(t, err, context.Canceled)
}

// endpointExchangeRunner publishes one in-flight endpoint exchange for cancellation tests.
//
// Example: the test cancels its context after ReadContext has started.
type endpointExchangeRunner struct {
	Session *endpointSession
	Result  chan<- error
}

// Run executes the exchange and publishes its terminal error.
//
// Example: TransferCancelled is normalized before this result is inspected.
func (runner endpointExchangeRunner) Run(ctx context.Context) {
	_, err := runner.Session.Exchange(ctx, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	runner.Result <- err
}

// TestEndpointSessionRejectsLateNoResponseBytes verifies a late setter reply is quarantined.
//
// Example: bytes arriving after the initial probe prevent the following identity write.
func TestEndpointSessionRejectsLateNoResponseBytes(t *testing.T) {
	t.Parallel()

	reader := new(lateResponseReader)
	writer := new(endpointWriter)
	session, err := newEndpointSession(reader, writer, 1024)
	require.NoError(t, err)
	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
	require.NoError(t, err)
	_, err = session.Exchange(context.Background(), owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	requireErrorType[*owonprotocol.ErrSurplusResponse](t, err)
	require.Equal(t, []byte(":RUN\n"), writer.Data)
}

// lateResponseReader reports an idle first probe and stale bytes on the second.
//
// Example: it deterministically models a no-response command whose reply arrives late.
type lateResponseReader struct {
	reads int
}

// ReadContext waits for the first probe's deadline and returns an unexpected reply on the next call.
//
// Example: the endpoint's pre-command idle probe receives `OK` on read two.
func (reader *lateResponseReader) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	reader.reads++
	if reader.reads == 1 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		<-ctx.Done()

		return 0, ctx.Err()
	}

	return copy(destination, []byte("OK\n")), nil
}

// endpointReader provides deterministic context-aware fragmented reads.
//
// Example: each configured chunk models one USB bulk transfer.
type endpointReader struct {
	Chunks [][]byte
}

// immediateEndpointReader returns its fixture without consulting the probe context.
//
// Example: EOF and surplus-byte tests remain deterministic even if a five-millisecond probe context is already expired.
type immediateEndpointReader struct {
	Chunks [][]byte
}

// ReadContext returns one configured fixture immediately.
//
// Example: a configured `OK` chunk is observed as surplus rather than mistaken for probe quiet.
func (reader *immediateEndpointReader) ReadContext(
	_ context.Context,
	destination []byte,
) (int, error) {
	if len(reader.Chunks) == 0 {
		return 0, io.EOF
	}
	chunk := reader.Chunks[0]
	reader.Chunks = reader.Chunks[1:]
	count := copy(destination, chunk)
	if count != len(chunk) {
		return count, errors.New("test destination was unexpectedly short")
	}

	return count, nil
}

// probeCancellationReader waits for the probe deadline before returning gousb's status.
//
// Example: the test distinguishes the probe timeout from an immediate device failure.
type probeCancellationReader struct{}

// ReadContext returns TransferCancelled only after the caller's probe context expires.
//
// Example: this mirrors gousb's cancellation contract for a quiet endpoint.
func (*probeCancellationReader) ReadContext(
	ctx context.Context,
	_ []byte,
) (int, error) {
	<-ctx.Done()

	return 0, gousb.TransferCancelled
}

// requestCancellationReader waits for the request context before returning native cancellation.
//
// Example: an in-flight USB read is canceled after the command has been written.
type requestCancellationReader struct {
	Started chan struct{}
}

// ReadContext blocks until its request is canceled and then reports gousb's status.
//
// Example: response code maps this status back to context.Canceled.
func (reader *requestCancellationReader) ReadContext(
	ctx context.Context,
	_ []byte,
) (int, error) {
	close(reader.Started)
	<-ctx.Done()

	return 0, gousb.TransferCancelled
}

// ReadContext returns the next configured fragment and honors cancellation.
//
// Example: an empty fixture returns io.EOF.
func (reader *endpointReader) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(reader.Chunks) == 0 {
		return 0, io.EOF
	}
	chunk := reader.Chunks[0]
	reader.Chunks = reader.Chunks[1:]
	count := copy(destination, chunk)
	if count != len(chunk) {
		return count, errors.New("test destination was unexpectedly short")
	}

	return count, nil
}

// endpointWriter captures writes and can deliberately accept short prefixes.
//
// Example: maximumWrite=1 models a one-byte-at-a-time endpoint.
type endpointWriter struct {
	Data         []byte
	MaximumWrite int
}

// WriteContext appends one accepted prefix and honors cancellation.
//
// Example: zero maximumWrite accepts the complete buffer.
func (writer *endpointWriter) WriteContext(
	ctx context.Context,
	source []byte,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count := len(source)
	if writer.MaximumWrite > 0 && writer.MaximumWrite < count {
		count = writer.MaximumWrite
	}
	writer.Data = append(writer.Data, source[:count]...)

	return count, nil
}
