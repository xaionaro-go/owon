package owonusb

import (
	"context"
	"sync"
)

// ErrUSBOpen retains a backend whose native cleanup still needs retrying.
//
// Example: callers can type-assert an Open error and call Close again
// instead of losing a partially acquired libusb owner.
type ErrUSBOpen struct {
	mu      sync.Mutex
	cause   error
	backend *Backend
}

// Error returns the opening failure and any cleanup failure.
//
// Example: daemon logs retain the original identity or enumeration cause.
func (err *ErrUSBOpen) Error() string {
	if err == nil || err.cause == nil {
		return "open OWON USB instrument failed"
	}

	return err.cause.Error()
}

// Unwrap exposes the opening failure to errors.Is and errors.As.
//
// Example: callers can still detect context cancellation or invalid configuration.
func (err *ErrUSBOpen) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.cause
}

// Close retries cleanup of the retained backend owner.
//
// Example: a supervisor can retry after a transient native close error.
func (err *ErrUSBOpen) Close(ctx context.Context) error {
	if err == nil {
		return nil
	}
	err.mu.Lock()
	defer err.mu.Unlock()
	if err.backend == nil {
		return nil
	}
	if closeErr := err.backend.Close(ctx); closeErr != nil {
		return closeErr
	}
	err.backend = nil

	return nil
}
