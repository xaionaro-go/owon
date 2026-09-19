package owoncontrol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestErrorMessagesAndCauses verifies diagnostic text and cause ownership across public error classes.
//
// Example: a malformed response retains a parser cause while a validation leaf unwraps to nil.
func TestErrorMessagesAndCauses(t *testing.T) {
	t.Parallel()
	cause := errors.New("cause")
	for _, testCase := range []struct {
		Err   error
		Text  string
		Cause error
	}{
		{(*ErrUnavailable)(nil), "", nil},
		{&ErrUnavailable{Operation: "read"}, "read", nil},
		{&ErrUnavailable{Reason: "closed"}, "closed", nil},
		{&ErrUnavailable{Resource: "device", Reason: "busy"}, "device: busy", nil},
		{&ErrUnavailable{Operation: "read", Resource: "device", Reason: "is closed", Cause: cause}, "read: device is closed: cause", cause},
		{&ErrUnavailable{Cause: cause}, "cause", cause},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}

// requireErrorType verifies that an error chain contains one concrete error type.
//
// Example: requireErrorType[*ErrInvalidRequest](t, err) checks a validation result.
func requireErrorType[T error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed T
	require.ErrorAs(t, err, &typed)
}
