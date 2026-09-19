package owonscpi

import (
	"fmt"
)

// ErrInvalidSetting identifies a valid domain value outside the HDS writable matrix.
//
// Example: an otherwise valid 3X probe has no supported HDS write token.
type ErrInvalidSetting struct{ Reason string }

// Error describes why the device-specific setting cannot be compiled.
//
// Example: an unsupported voltage scale is distinct from an unknown channel enum.
func (err *ErrInvalidSetting) Error() string {
	if err == nil || err.Reason == "" {
		return "invalid OWON setting"
	}
	return "invalid OWON setting: " + err.Reason
}

// Unwrap reports that a dialect setting rejection has no lower-level cause.
//
// Example: errors.As identifies this leaf beneath operation diagnostics.
func (*ErrInvalidSetting) Unwrap() error { return nil }

// ErrMalformedResponse identifies a device payload that cannot be interpreted by the HDS dialect.
//
// Example: a valid frame containing an invalid screen-header token is a dialect failure.
type ErrMalformedResponse struct {
	Reason string
	Cause  error
}

// Error describes the malformed payload and preserves the causal detail for diagnostics.
//
// Example: a nonnumeric DMM reading reports its parsing cause.
func (err *ErrMalformedResponse) Error() string {
	if err == nil {
		return "malformed OWON response"
	}
	message := "malformed OWON response"
	if err.Reason != "" {
		message += ": " + err.Reason
	}
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	return message
}

// Unwrap exposes the original parse failure without changing its classification.
//
// Example: errors.As can inspect a strconv.NumError below this dialect error.
func (err *ErrMalformedResponse) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// ErrUnsupportedControl identifies a control absent from the verified device surface.
//
// Example: requesting acquisition averaging returns an ErrUnsupportedControl.
type ErrUnsupportedControl struct {
	Control string
}

// Error returns the unsupported-control classification and optional control name.
//
// Example: `unsupported OWON control: averaging` names the unavailable feature.
func (err *ErrUnsupportedControl) Error() string {
	if err == nil || err.Control == "" {
		return "unsupported OWON control"
	}

	return fmt.Sprintf("unsupported OWON control: %s", err.Control)
}

// Unwrap returns no lower-level cause because this error is a leaf classification.
//
// Example: errors.As still matches ErrUnsupportedControl through an RPC operation error.
func (*ErrUnsupportedControl) Unwrap() error {
	return nil
}
