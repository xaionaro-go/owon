package owonscpi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireErrorType verifies a concrete classification through causal wrapping.
//
// Example: malformed header fields retain a dialect corruption error.
func requireErrorType[Error error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed Error
	require.ErrorAs(t, err, &typed)
}

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
		{(*ErrUnsupportedControl)(nil), "unsupported OWON control", nil},
		{&ErrUnsupportedControl{Control: "average"}, "unsupported OWON control: average", nil},
		{(*ErrMalformedResponse)(nil), "malformed OWON response", nil},
		{&ErrMalformedResponse{}, "malformed OWON response", nil},
		{&ErrMalformedResponse{Cause: cause}, "malformed OWON response: cause", cause},
		{&ErrMalformedResponse{Reason: "frame"}, "malformed OWON response: frame", nil},
		{&ErrMalformedResponse{Reason: "frame", Cause: cause}, "malformed OWON response: frame: cause", cause},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}
