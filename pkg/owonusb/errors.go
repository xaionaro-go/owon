package owonusb

import (
	"fmt"
)

// ErrUnavailable identifies a native USB resource that cannot perform the requested operation.
//
// Example: exchanging after endpoint cleanup reports unavailable USB ownership.
type ErrUnavailable struct {
	Operation string
	Resource  string
	Reason    string
	Cause     error
}

// Error names the unavailable USB resource and any underlying native failure.
//
// Example: failed device cleanup identifies the handle retained for retry.
func (err *ErrUnavailable) Error() string {
	if err == nil {
		return "OWON USB resource unavailable"
	}
	message := fmt.Sprintf("%s: %s %s", err.Operation, err.Resource, err.Reason)
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	return message
}

// Unwrap exposes the native failure without losing its classification.
//
// Example: cancellation remains inspectable beneath a resource diagnostic.
func (err *ErrUnavailable) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// ErrInvalidConfig identifies invalid physical connection selection or limits.
//
// Example: an identifier exceeding the USB descriptor width is rejected before enumeration.
type ErrInvalidConfig struct{ Reason string }

// Error describes a rejected USB configuration value.
//
// Example: an invalid USB identifier is distinguished from device-domain request validation.
func (err *ErrInvalidConfig) Error() string {
	if err == nil {
		return "invalid OWON USB configuration"
	}
	return "invalid OWON USB configuration: " + err.Reason
}

// Unwrap reports a leaf physical-configuration classification.
//
// Example: errors.As matches this type through Open diagnostics.
func (*ErrInvalidConfig) Unwrap() error { return nil }
