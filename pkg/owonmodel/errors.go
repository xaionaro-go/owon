package owonmodel

import (
	"fmt"
)

// ErrInvalidRequest identifies an invalid caller value.
//
// Example: a nil setter request returns an ErrInvalidRequest.
type ErrInvalidRequest struct {
	Reason string
}

// Error returns the invalid-request classification and optional detail.
//
// Example: `invalid OWON request: empty patch` explains a rejected mutation.
func (err *ErrInvalidRequest) Error() string {
	if err == nil || err.Reason == "" {
		return "invalid OWON request"
	}

	return fmt.Sprintf("invalid OWON request: %s", err.Reason)
}

// Unwrap returns no lower-level cause because this error is a leaf classification.
//
// Example: errors.As still matches ErrInvalidRequest through a wrapped operation error.
func (*ErrInvalidRequest) Unwrap() error {
	return nil
}
