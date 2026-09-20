package owonusb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/gousb"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

const (
	// endpointReadBytes is the USB transfer buffer size.
	//
	// Example: each bulk read accepts complete USB packets.
	endpointReadBytes = 4096
	// responseDrainLengthPrefixBytes is the fixed prefix on a length-prefixed response.
	//
	// Example: the preflight drain permits one configured maximum payload plus four prefix bytes.
	responseDrainLengthPrefixBytes = 4
	// noResponseProbeDuration bounds the quiet probe after setters.
	//
	// Example: setters probe for five milliseconds.
	noResponseProbeDuration = 5 * time.Millisecond
)

// BulkReader is the context-aware input operation required from a USB endpoint.
//
// Example: gousb.InEndpoint satisfies BulkReader.
type BulkReader interface {
	// ReadContext receives one USB transfer and honors cancellation.
	//
	// Example: endpointSession uses it while waiting for a response frame.
	ReadContext(
		context.Context,
		[]byte,
	) (int, error)
}

// BulkWriter is the context-aware output operation required from a USB endpoint.
//
// Example: gousb.OutEndpoint satisfies BulkWriter.
type BulkWriter interface {
	// WriteContext sends one USB transfer and may report a short write.
	//
	// Example: endpointSession loops until every command byte is accepted.
	WriteContext(
		context.Context,
		[]byte,
	) (int, error)
}

// endpointSession owns the buffered state for one pair of USB bulk endpoints.
//
// Example: Backend replaces the endpoint session after an ambiguous transaction.
type endpointSession struct {
	reader         BulkReader
	writer         BulkWriter
	maximumBytes   uint32
	buffer         []byte
	deferredErr    error
	needsIdleProbe bool
}

// responseDrainReport records bounded bytes and reads consumed by candidate preflight.
//
// Example: a stale 604-byte frame reports 604 discarded bytes and one quiet-probe read.
type responseDrainReport struct {
	DiscardedBytes int
	ReadCalls      int
}

// newEndpointSession validates endpoints and constructs their framing state.
//
// Example: deterministic tests inject fragmented readers and short writers.
func newEndpointSession(
	reader BulkReader,
	writer BulkWriter,
	maximumBytes uint32,
) (*endpointSession, error) {
	if reader == nil {
		return nil, &ErrUnavailable{Operation: "create endpoint session", Resource: "reader", Reason: "is nil"}
	}
	if writer == nil {
		return nil, &ErrUnavailable{Operation: "create endpoint session", Resource: "writer", Reason: "is nil"}
	}
	maximumBytes, err := owonprotocol.NormalizeResponseLimit(maximumBytes)
	if err != nil {
		return nil, err
	}

	return &endpointSession{reader: reader, writer: writer, maximumBytes: maximumBytes}, nil
}

// drainPending discards bounded candidate bytes and accepts only its own quiet-probe timeout.
// It does not replace verifyNoResponse or alter needsIdleProbe; a deferred endpoint error remains
// a failure, and callers use this method only for a newly opened candidate. The bounded window
// isolates observed bytes but does not prove that later hardware bytes are absent.
//
// Example: recovery drains a stale response before issuing the candidate identity query.
func (session *endpointSession) drainPending(
	ctx context.Context,
) (_report responseDrainReport, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "endpointSession.drainPending")
		defer
		// traceResult records bounded preflight completion without logging discarded bytes.
		//
		// Example: a rejected candidate retains its endpoint error for recovery diagnostics.
		func() { logger.Tracef(ctx, "/endpointSession.drainPending: %v", _err) }()
	}

	if session == nil {
		return _report, &ErrUnavailable{Operation: "drain endpoint session", Resource: "session", Reason: "is nil"}
	}
	if ctx == nil {
		return _report, &ErrUnavailable{Operation: "drain endpoint session", Resource: "context", Reason: "is nil"}
	}
	if session.reader == nil {
		return _report, &ErrUnavailable{Operation: "drain endpoint session", Resource: "reader", Reason: "is nil"}
	}
	if session.deferredErr != nil {
		return _report, fmt.Errorf("drain endpoint session with deferred error: %w", session.deferredErr)
	}

	maximumBytes, err := owonprotocol.NormalizeResponseLimit(session.maximumBytes)
	if err != nil {
		return _report, err
	}
	maximumDiscarded := uint64(maximumBytes) + responseDrainLengthPrefixBytes
	_report.DiscardedBytes = len(session.buffer)
	session.buffer = nil
	if uint64(_report.DiscardedBytes) > maximumDiscarded {
		return _report, fmt.Errorf(
			"drain endpoint session discarded %d bytes beyond cap %d: %w",
			_report.DiscardedBytes,
			maximumDiscarded,
			&owonprotocol.ErrResponseTooLarge{Size: uint64(_report.DiscardedBytes), Limit: maximumDiscarded},
		)
	}

	probeCtx, cancel := context.WithTimeout(ctx, noResponseProbeDuration)
	defer cancel()
	var chunk [endpointReadBytes]byte
	for {
		count, err := session.reader.ReadContext(probeCtx, chunk[:])
		_report.ReadCalls++
		if count < 0 || count > len(chunk) {
			return _report, errors.Join(
				fmt.Errorf("drain endpoint session returned invalid count %d", count),
				endpointContextError(ctx, err),
				ctx.Err(),
			)
		}
		if requestErr := ctx.Err(); requestErr != nil {
			return _report, errors.Join(
				fmt.Errorf("drain endpoint session canceled by caller: %w", requestErr),
				endpointContextError(ctx, err),
			)
		}
		if count > 0 {
			_report.DiscardedBytes += count
			if uint64(_report.DiscardedBytes) > maximumDiscarded {
				return _report, errors.Join(
					fmt.Errorf(
						"drain endpoint session discarded %d bytes beyond cap %d: %w",
						_report.DiscardedBytes,
						maximumDiscarded,
						&owonprotocol.ErrResponseTooLarge{Size: uint64(_report.DiscardedBytes), Limit: maximumDiscarded},
					),
					endpointContextError(ctx, err),
				)
			}
			if err != nil || probeCtx.Err() != nil {
				return _report, fmt.Errorf(
					"drain endpoint session received %d bytes with an error: %w",
					count,
					&owonprotocol.ErrSurplusResponse{Bytes: count, Cause: endpointContextError(ctx, err)},
				)
			}
			continue
		}
		if err == nil {
			return _report, fmt.Errorf("drain endpoint session: %w", io.ErrNoProgress)
		}
		if isNoResponseTimeout(probeCtx, ctx, err) {
			return _report, nil
		}

		return _report, fmt.Errorf("drain endpoint session: %w", endpointContextError(ctx, err))
	}
}

// Exchange writes one command and reads its explicitly selected response frame.
//
// Example: ResponseModeNone verifies a bounded quiet period after the complete command write.
func (session *endpointSession) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "endpointSession.Exchange")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/endpointSession.Exchange: %v", _err) }()
	}

	if session == nil {
		return nil, &ErrUnavailable{Operation: "exchange endpoint session", Resource: "session", Reason: "is nil"}
	}
	if ctx == nil {
		return nil, &ErrUnavailable{Operation: "exchange endpoint session", Resource: "context", Reason: "is nil"}
	}
	request, err := owonprotocol.CommandBytes(command)
	if err != nil {
		return nil, err
	}
	if len(session.buffer) != 0 || session.deferredErr != nil {
		return nil, fmt.Errorf("endpoint contains %d buffered bytes before command: %w", len(session.buffer), &owonprotocol.ErrSurplusResponse{Bytes: len(session.buffer), Cause: session.deferredErr})
	}
	if session.needsIdleProbe {
		if err := session.verifyNoResponse(ctx); err != nil {
			return nil, fmt.Errorf("verify idle endpoint before command %q: %w", command.Text, err)
		}
		session.needsIdleProbe = false
	}
	if err := writeAll(ctx, session.writer, request); err != nil {
		return nil, fmt.Errorf("write command %q: %w", command.Text, err)
	}
	if command.ResponseMode == owonprotocol.ResponseModeNone {
		if err := session.verifyNoResponse(ctx); err != nil {
			return nil, err
		}
		session.needsIdleProbe = true

		return nil, nil
	}
	response, err := session.readResponse(ctx, command.ResponseMode)
	if err != nil {
		return nil, err
	}
	if len(session.buffer) != 0 {
		return nil, fmt.Errorf("response for %q left %d bytes: %w", command.Text, len(session.buffer), &owonprotocol.ErrSurplusResponse{Bytes: len(session.buffer)})
	}

	return response, nil
}

// verifyNoResponse observes a bounded quiet period after a documented no-response command.
//
// Example: only the probe's own deadline is quiet; EOF and unrelated errors poison the session.
func (session *endpointSession) verifyNoResponse(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "endpointSession.verifyNoResponse")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/endpointSession.verifyNoResponse: %v", _err) }()
	}

	probeCtx, cancel := context.WithTimeout(ctx, noResponseProbeDuration)
	defer cancel()
	chunk := make([]byte, endpointReadBytes)
	count, err := session.reader.ReadContext(probeCtx, chunk)
	if count < 0 || count > len(chunk) {
		return errors.Join(fmt.Errorf("no-response probe returned invalid count %d", count), err, ctx.Err())
	}
	if count > 0 {
		return fmt.Errorf("no-response command produced %d bytes: %w", count, &owonprotocol.ErrSurplusResponse{Bytes: count, Cause: errors.Join(err, ctx.Err())})
	}
	if requestErr := ctx.Err(); requestErr != nil {
		return fmt.Errorf("no-response probe canceled with request: %w", errors.Join(err, requestErr))
	}
	if err != nil {
		// Classify the native result before normalization adds another context cause.
		if isNoResponseTimeout(probeCtx, ctx, err) {
			return nil
		}
		return fmt.Errorf("probe no-response command: %w", endpointContextError(probeCtx, err))
	}

	return io.ErrNoProgress
}

// isNoResponseTimeout recognizes the transport-specific cancellation emitted
// when the probe's own quiet-period deadline expires.
//
// Example: gousb returns TransferCancelled rather than context.DeadlineExceeded
// after ReadContext cancels a libusb transfer for the probe deadline.
func isNoResponseTimeout(
	probeCtx context.Context,
	requestCtx context.Context,
	err error,
) bool {
	if err == nil || requestCtx.Err() != nil || probeCtx.Err() != context.DeadlineExceeded {
		return false
	}

	cause := endpointSingleCause(err)
	return cause == context.DeadlineExceeded || cause == gousb.TransferCancelled
}

// endpointSingleCause returns the leaf of ordinary wrapping, or nil for an ambiguous error tree.
//
// Example: wrapped EOF is a clean framing boundary; EOF joined with cancellation is not.
func endpointSingleCause(err error) error {
	for err != nil {
		// Even expected-only joins are ambiguous; endpoint success requires one cause.
		if _, multiple := err.(endpointErrorGroup); multiple {
			return nil
		}
		cause := errors.Unwrap(err)
		if cause == nil {
			return err
		}
		err = cause
	}

	return nil
}

// endpointErrorGroup identifies an error tree that cannot prove a single quiet-probe outcome.
//
// Example: a timeout joined with a device failure must not be accepted as silence.
type endpointErrorGroup interface {
	// Unwrap exposes all independent causes retained by the error.
	//
	// Example: errors.Join produces this shape even when nested inside an ordinary wrapper.
	Unwrap() []error
}

// endpointContextError preserves native cancellation while adding the caller's ended context.
//
// Example: a canceled USB write retains TransferCancelled and an independent joined failure.
func endpointContextError(
	ctx context.Context,
	err error,
) error {
	if requestErr := ctx.Err(); requestErr != nil && errors.Is(err, gousb.TransferCancelled) {
		return errors.Join(err, requestErr)
	}

	return err
}

// readResponse feeds validated endpoint transfers into the shared frame decoder.
//
// Example: a fragment following a completed frame remains buffered as surplus.
func (session *endpointSession) readResponse(
	ctx context.Context,
	mode owonprotocol.ResponseMode,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "endpointSession.readResponse")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/endpointSession.readResponse: %v", _err) }()
	}

	decoder, err := owonprotocol.NewFrameDecoder(mode, session.maximumBytes)
	if err != nil {
		return nil, err
	}
	for {
		progress, err := decoder.Feed(session.buffer)
		if err != nil {
			session.buffer = session.buffer[progress.Consumed:]
			return nil, err
		}
		if progress.Complete {
			session.buffer = session.buffer[progress.Consumed:]
			return decoder.Payload()
		}
		// Reuse the bounded transfer buffer; accumulated payload belongs only to the decoder.
		session.buffer = session.buffer[:0]
		if err := session.fill(ctx); err != nil {
			_, frameErr := decoder.Payload()
			return nil, fmt.Errorf("read response: %w", errors.Join(frameErr, err))
		}
	}
}

// fill appends one validated endpoint transfer to the persistent buffer.
//
// Example: bytes returned with io.EOF remain available before EOF is surfaced.
func (session *endpointSession) fill(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "endpointSession.fill")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/endpointSession.fill: %v", _err) }()
	}

	if session.deferredErr != nil {
		return session.deferredErr
	}
	chunk := make([]byte, endpointReadBytes)
	count, err := session.reader.ReadContext(ctx, chunk)
	if count < 0 || count > len(chunk) {
		// Reject unsafe counts without discarding simultaneous native or cancellation causes.
		return errors.Join(fmt.Errorf("endpoint reader returned invalid count %d", count), endpointContextError(ctx, err))
	}
	if count > 0 {
		session.buffer = append(session.buffer, chunk[:count]...)
	}
	if err != nil {
		err = endpointContextError(ctx, err)
		session.deferredErr = err
		if endpointSingleCause(err) != io.EOF {
			return err
		}
	}
	if count == 0 {
		switch {
		case err != nil:
			return err
		default:
			return io.ErrNoProgress
		}
	}

	return nil
}

// writeAll handles context-aware short writes and rejects zero progress.
//
// Example: a one-byte writer still receives the complete newline-terminated command.
func writeAll(
	ctx context.Context,
	writer BulkWriter,
	payload []byte,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "writeAll")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/writeAll: %v", _err) }()
	}

	for len(payload) > 0 {
		count, err := writer.WriteContext(ctx, payload)
		if count < 0 || count > len(payload) {
			// Preserve completion causes while rejecting the count before advancing the write.
			return errors.Join(fmt.Errorf("endpoint writer returned invalid count %d for %d bytes", count, len(payload)), endpointContextError(ctx, err))
		}
		payload = payload[count:]
		if err != nil {
			return endpointContextError(ctx, err)
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}

	return nil
}
