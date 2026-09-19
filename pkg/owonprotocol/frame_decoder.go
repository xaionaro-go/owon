package owonprotocol

import (
	"bytes"
	"encoding/binary"
)

const (
	// initialASCIICapacity amortizes fragmented line growth, subject to the configured cap.
	//
	// Example: a short identity response usually needs only one allocation.
	initialASCIICapacity = 256
)

// FrameProgress reports consumption from this fragment and completion of the whole frame.
//
// Example: an already complete decoder consumes zero bytes from a following response.
type FrameProgress struct {
	Consumed int
	Complete bool
}

// FrameDecoder incrementally owns one bounded response, excluding any following bytes.
// Fields are private because changing framing state would bypass bounds and completion checks.
//
// Example: USB feeds arbitrary transfers and keeps fragment bytes beyond Consumed as surplus.
type FrameDecoder struct {
	mode         ResponseMode
	maximumBytes uint32
	prefix       [4]byte
	prefixBytes  int
	payload      []byte
	length       int
	complete     bool
	err          error
}

// NewFrameDecoder validates framing and allocation limits before accepting bytes.
//
// Example: zero maximumBytes selects the fixed sixteen-MiB cap.
func NewFrameDecoder(
	mode ResponseMode,
	maximumBytes uint32,
) (*FrameDecoder, error) {
	maximumBytes, err := NormalizeResponseLimit(maximumBytes)
	if err != nil {
		return nil, err
	}
	switch mode {
	case ResponseModeASCII, ResponseModeLengthPrefixed:
		return &FrameDecoder{mode: mode, maximumBytes: maximumBytes}, nil
	default:
		return nil, &ErrMalformedResponse{Reason: "response mode does not contain a readable frame"}
	}
}

// Feed copies only bytes belonging to the first frame and never rescans earlier fragments.
// Completion and terminal errors are sticky; later fragments remain unconsumed.
//
// Example: feeding `one\ntwo\n` consumes four bytes and completes payload `one`.
func (decoder *FrameDecoder) Feed(fragment []byte) (FrameProgress, error) {
	if decoder == nil {
		return FrameProgress{}, &ErrMalformedResponse{Reason: "frame decoder is nil"}
	}
	if decoder.err != nil || decoder.complete {
		return FrameProgress{Complete: decoder.complete}, decoder.err
	}
	var consumed int
	switch decoder.mode {
	case ResponseModeASCII:
		consumed = decoder.feedASCII(fragment)
	case ResponseModeLengthPrefixed:
		consumed = decoder.feedLengthPrefixed(fragment)
	default:
		decoder.err = &ErrMalformedResponse{Reason: "response mode does not contain a readable frame"}
	}
	return FrameProgress{Consumed: consumed, Complete: decoder.complete}, decoder.err
}

// Payload returns the decoder-owned bytes only after successful completion.
// The returned storage remains stable; callers must not modify it.
//
// Example: an empty complete binary response succeeds while a partial prefix returns an error.
func (decoder *FrameDecoder) Payload() ([]byte, error) {
	if decoder == nil {
		return nil, &ErrMalformedResponse{Reason: "frame decoder is nil"}
	}
	if decoder.err != nil {
		return nil, decoder.err
	}
	if decoder.complete {
		return decoder.payload, nil
	}
	var reason string
	switch decoder.mode {
	case ResponseModeASCII:
		reason = "ASCII frame is incomplete"
	case ResponseModeLengthPrefixed:
		reason = "payload is incomplete"
		if decoder.prefixBytes < len(decoder.prefix) {
			reason = "length prefix is incomplete"
		}
	default:
		reason = "response mode does not contain a readable frame"
	}
	return nil, &ErrMalformedResponse{Reason: reason}
}

// feedASCII scans only the new bounded fragment and grows storage geometrically.
// CR counts against the pre-LF bound and is removed only after completion.
//
// Example: `abc\r\n` fits a four-byte limit while `abcd\r\n` does not.
func (decoder *FrameDecoder) feedASCII(fragment []byte) int {
	remaining := int(decoder.maximumBytes) - len(decoder.payload)
	// Inspect at most the allowed bytes plus the possible terminating LF.
	fragment = fragment[:min(len(fragment), remaining+1)]
	lineEnd := bytes.IndexByte(fragment, '\n')
	size := len(fragment)
	if lineEnd >= 0 {
		size = lineEnd
	}
	if size > remaining {
		decoder.err = &ErrResponseTooLarge{Size: uint64(decoder.maximumBytes) + 1, Limit: uint64(decoder.maximumBytes)}
		return size
	}
	needed := len(decoder.payload) + size
	if needed > cap(decoder.payload) {
		capacity := min(int(decoder.maximumBytes), max(needed, max(initialASCIICapacity, 2*cap(decoder.payload))))
		payload := make([]byte, len(decoder.payload), capacity)
		copy(payload, decoder.payload)
		decoder.payload = payload
	}
	decoder.payload = append(decoder.payload, fragment[:size]...)
	if lineEnd < 0 {
		return size
	}
	decoder.complete = true
	if needed > 0 && decoder.payload[needed-1] == '\r' {
		decoder.payload = decoder.payload[:needed-1]
	}
	return size + 1
}

// feedLengthPrefixed validates the fixed prefix before allocating exactly the declared payload.
//
// Example: a corrupt four-GiB declaration consumes only its prefix and allocates no payload.
func (decoder *FrameDecoder) feedLengthPrefixed(fragment []byte) int {
	consumed := 0
	if decoder.prefixBytes < len(decoder.prefix) {
		consumed = copy(decoder.prefix[decoder.prefixBytes:], fragment)
		decoder.prefixBytes += consumed
		if decoder.prefixBytes < len(decoder.prefix) {
			return consumed
		}
		length := binary.LittleEndian.Uint32(decoder.prefix[:])
		if length > decoder.maximumBytes {
			decoder.err = &ErrResponseTooLarge{Size: uint64(length), Limit: uint64(decoder.maximumBytes)}
			return consumed
		}
		decoder.length = int(length)
		if length > 0 {
			decoder.payload = make([]byte, 0, decoder.length)
		}
	}
	size := min(len(fragment)-consumed, decoder.length-len(decoder.payload))
	decoder.payload = append(decoder.payload, fragment[consumed:consumed+size]...)
	decoder.complete = len(decoder.payload) == decoder.length
	return consumed + size
}

// nextReadSize bounds a synchronous read so it cannot consume a following frame.
//
// Example: ASCII reads one byte while binary reads the remaining prefix or payload.
func (decoder *FrameDecoder) nextReadSize() int {
	if decoder.mode == ResponseModeASCII {
		return 1
	}
	if decoder.prefixBytes < len(decoder.prefix) {
		return len(decoder.prefix) - decoder.prefixBytes
	}
	return decoder.length - len(decoder.payload)
}
