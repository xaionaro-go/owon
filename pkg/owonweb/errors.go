package owonweb

import (
	"fmt"
)

// ErrInvalidWebRequest identifies browser or process configuration input rejected before an RPC is sent.
//
// Example: an invalid subscribe interval returns an ErrInvalidWebRequest with a useful reason.
type ErrInvalidWebRequest struct {
	Reason string
	Cause  error
}

// Error returns the request classification and optional validation detail.
//
// Example: `invalid OWON web request: interval is too short` explains a 400 response.
func (err *ErrInvalidWebRequest) Error() string {
	if err == nil {
		return "invalid OWON web request"
	}
	if err.Reason == "" {
		if err.Cause == nil {
			return "invalid OWON web request"
		}

		return fmt.Sprintf("invalid OWON web request: %v", err.Cause)
	}
	if err.Cause == nil {
		return fmt.Sprintf("invalid OWON web request: %s", err.Reason)
	}

	return fmt.Sprintf("invalid OWON web request: %s: %v", err.Reason, err.Cause)
}

// Unwrap returns the lower-level parser or operating-system failure.
//
// Example: callers can inspect a duration parser error beneath the web request classification.
func (err *ErrInvalidWebRequest) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}

// ErrInvalidWebProtocol identifies an impossible frame generated at the browser protocol boundary.
//
// Example: a newline in an SSE event name is rejected before it can corrupt framing.
type ErrInvalidWebProtocol struct {
	Reason string
	Cause  error
}

// Error returns the protocol classification and optional framing detail.
//
// Example: `invalid OWON web protocol: event name contains a newline` identifies a malformed frame.
func (err *ErrInvalidWebProtocol) Error() string {
	if err == nil {
		return "invalid OWON web protocol"
	}
	if err.Reason == "" {
		if err.Cause == nil {
			return "invalid OWON web protocol"
		}

		return fmt.Sprintf("invalid OWON web protocol: %v", err.Cause)
	}
	if err.Cause == nil {
		return fmt.Sprintf("invalid OWON web protocol: %s", err.Reason)
	}

	return fmt.Sprintf("invalid OWON web protocol: %s: %v", err.Reason, err.Cause)
}

// Unwrap returns the lower-level framing failure.
//
// Example: errors.As can retain a transport cause beneath a protocol classification.
func (err *ErrInvalidWebProtocol) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
