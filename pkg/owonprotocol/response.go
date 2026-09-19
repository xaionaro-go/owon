package owonprotocol

import (
	"errors"
	"fmt"
	"io"
)

const (
	// DefaultMaximumResponseBytes bounds one decoded instrument response.
	//
	// Example: a zero response limit selects sixteen MiB.
	DefaultMaximumResponseBytes uint32 = 16 << 20
	// responseReadBytes bounds temporary storage for synchronous response reads.
	//
	// Example: a large binary payload is fed to its decoder in four-KiB fragments.
	responseReadBytes = 4096
)

// ErrInvalidResponseLimit identifies a configured frame bound above the protocol allocation cap.
//
// Example: a limit of seventeen MiB is rejected before reading any response bytes.
type ErrInvalidResponseLimit struct {
	Limit uint32
}

// Error describes the invalid configured frame limit.
//
// Example: the message distinguishes configuration failure from an oversized device reply.
func (err *ErrInvalidResponseLimit) Error() string {
	if err == nil {
		return "invalid OWON response limit"
	}
	return fmt.Sprintf("invalid OWON response limit %d: maximum is %d", err.Limit, DefaultMaximumResponseBytes)
}

// Unwrap returns nil because invalid configuration has no underlying failure.
//
// Example: errors.As identifies the invalid response limit through a caller's wrapping error.
func (*ErrInvalidResponseLimit) Unwrap() error {
	return nil
}

// ReadResponse decodes exactly one response frame from a synchronous reader.
//
// Example: tests decode captured length-prefixed waveform data without USB hardware.
func ReadResponse(
	reader io.Reader,
	mode ResponseMode,
	maximumBytes uint32,
) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("read OWON response: %w", &ErrMalformedResponse{Reason: "reader is nil"})
	}
	decoder, err := NewFrameDecoder(mode, maximumBytes)
	if err != nil {
		return nil, err
	}
	var chunk [responseReadBytes]byte
	for {
		// Let the decoder choose exact reads so framing has only one authoritative implementation.
		count, readErr := io.ReadFull(reader, chunk[:min(len(chunk), decoder.nextReadSize())])
		progress, err := decoder.Feed(chunk[:count])
		if err != nil {
			return nil, err
		}
		if progress.Complete {
			return decoder.Payload()
		}
		if readErr != nil {
			_, frameErr := decoder.Payload()
			return nil, fmt.Errorf("read OWON response: %w", errors.Join(frameErr, readErr))
		}
	}
}

// NormalizeResponseLimit applies the fixed allocation cap to caller configuration.
//
// Example: zero selects sixteen MiB while sixteen MiB plus one is rejected.
func NormalizeResponseLimit(maximumBytes uint32) (uint32, error) {
	if maximumBytes == 0 {
		return DefaultMaximumResponseBytes, nil
	}
	if maximumBytes > DefaultMaximumResponseBytes {
		return 0, fmt.Errorf("maximum response bytes %d exceeds hard cap %d: %w", maximumBytes, DefaultMaximumResponseBytes, &ErrInvalidResponseLimit{Limit: maximumBytes})
	}

	return maximumBytes, nil
}

// ErrMalformedResponse identifies a truncated or structurally invalid device reply.
//
// Example: an ASCII response without LF returns an ErrMalformedResponse.
type ErrMalformedResponse struct {
	Reason string
	Cause  error
}

// Error returns the malformed-response classification and optional detail.
//
// Example: `malformed OWON response: missing newline` describes a framing defect.
func (err *ErrMalformedResponse) Error() string {
	if err == nil {
		return "malformed OWON response"
	}
	if err.Reason == "" {
		if err.Cause == nil {
			return "malformed OWON response"
		}

		return fmt.Sprintf("malformed OWON response: %v", err.Cause)
	}
	if err.Cause != nil {
		return fmt.Sprintf("malformed OWON response: %s: %v", err.Reason, err.Cause)
	}

	return fmt.Sprintf("malformed OWON response: %s", err.Reason)
}

// Unwrap returns an underlying parser or transport failure.
//
// Example: callers can inspect an EOF beneath malformed framing.
func (err *ErrMalformedResponse) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}

// ErrResponseTooLarge identifies a response beyond the configured allocation bound.
//
// Example: a corrupt length prefix produces an ErrResponseTooLarge.
type ErrResponseTooLarge struct {
	Size  uint64
	Limit uint64
}

// Error returns the response-size classification.
//
// Example: the error reports both declared size and configured limit when available.
func (err *ErrResponseTooLarge) Error() string {
	if err == nil || err.Limit == 0 {
		return "OWON response exceeds configured limit"
	}

	return fmt.Sprintf("OWON response size %d exceeds configured limit %d", err.Size, err.Limit)
}

// Unwrap returns no lower-level cause because this error is a leaf classification.
//
// Example: errors.As still matches ErrResponseTooLarge through a framing error.
func (*ErrResponseTooLarge) Unwrap() error {
	return nil
}

// ErrSurplusResponse identifies unexplained bytes following one complete response.
//
// Example: two replies received for one query produce an ErrSurplusResponse.
type ErrSurplusResponse struct {
	Bytes int
	Cause error
}

// Error returns the surplus-response classification.
//
// Example: the error includes the buffered byte count when available.
func (err *ErrSurplusResponse) Error() string {
	if err == nil {
		return "surplus OWON response bytes"
	}
	detail := "surplus OWON response bytes"
	if err.Bytes > 0 {
		detail = fmt.Sprintf("%s: %d", detail, err.Bytes)
	}
	if err.Cause != nil {
		return fmt.Sprintf("%s: %v", detail, err.Cause)
	}

	return detail
}

// Unwrap returns an underlying deferred endpoint failure.
//
// Example: EOF observed with a complete frame remains inspectable on the next exchange.
func (err *ErrSurplusResponse) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
