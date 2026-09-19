package owonsession

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// requireErrorType verifies that an error chain contains one concrete error type.
//
// Example: requireErrorType[*ErrInvalidConfig](t, err) checks a validation result.
func requireErrorType[T error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed T
	require.ErrorAs(t, err, &typed)
}

// TestSessionErrorsExposeOwnedClassifications checks session failures independently of instrument types.
//
// Example: an unconstructed session fails with ErrUnavailable rather than a domain request error.
func TestSessionErrorsExposeOwnedClassifications(t *testing.T) {
	_, err := New(new(scriptedBackend), Config{})
	var invalid *ErrInvalidConfig
	require.ErrorAs(t, err, &invalid)
	require.Nil(t, invalid.Unwrap())
	require.Contains(t, invalid.Error(), "empty expected serial")
	require.Equal(t, "invalid OWON session configuration", (*ErrInvalidConfig)(nil).Error())
	require.Equal(t, "invalid OWON session configuration", (&ErrInvalidConfig{}).Error())
	_, err = (&Session{}).Begin(t.Context())
	var unavailable *ErrUnavailable
	require.ErrorAs(t, err, &unavailable)
	require.Nil(t, unavailable.Unwrap())
	require.Contains(t, unavailable.Error(), "backend is unavailable")
	require.Equal(t, "OWON session unavailable", (*ErrUnavailable)(nil).Error())
}
