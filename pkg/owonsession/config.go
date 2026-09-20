package owonsession

import (
	"time"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

const (
	// DefaultDeviceOperationTimeout sets a cancellation deadline for each exchange,
	// recovery phase, or post-admission transaction operation context.
	// This is application policy, not a native completion guarantee: ownership remains
	// held until cooperative backend cancellation actually returns.
	//
	// Example: a caller with no deadline still requests cancellation after ten seconds.
	DefaultDeviceOperationTimeout = 10 * time.Second
)

// Config identifies the instrument and sets a cooperative deadline per device
// exchange, recovery phase, or post-admission transaction operation context.
// Zero OperationTimeout selects the default; a negative value is invalid.
//
// Example: Config{ExpectedSerial: "25061855", OperationTimeout: time.Minute} allows slow captures.
type Config struct {
	ExpectedSerial   owonmodel.SerialNumber
	OperationTimeout time.Duration
}

// ErrInvalidConfig identifies a session configuration that cannot safely own an instrument.
//
// Example: negative operation timeouts and missing recovery identities are invalid.
type ErrInvalidConfig struct {
	Reason string
}

// Error describes the invalid session configuration.
//
// Example: a negative timeout explains why the session could not be constructed.
func (err *ErrInvalidConfig) Error() string {
	if err == nil || err.Reason == "" {
		return "invalid OWON session configuration"
	}
	return "invalid OWON session configuration: " + err.Reason
}

// Unwrap reports a leaf configuration rejection.
//
// Example: errors.As identifies this type through daemon startup diagnostics.
func (*ErrInvalidConfig) Unwrap() error { return nil }

// NormalizeOperationTimeout applies the shared USB and session timeout policy.
//
// Example: library zero values select the default cancellation deadline, not a native completion bound.
func NormalizeOperationTimeout(timeout time.Duration) (time.Duration, error) {
	switch {
	case timeout < 0:
		return 0, &ErrInvalidConfig{Reason: "device operation timeout must not be negative"}
	case timeout == 0:
		return DefaultDeviceOperationTimeout, nil
	default:
		return timeout, nil
	}
}
