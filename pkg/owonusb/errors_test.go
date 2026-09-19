package owonusb

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
		{(*ErrUSBOpen)(nil), "open OWON USB instrument failed", nil},
		{&ErrUSBOpen{}, "open OWON USB instrument failed", nil},
		{&ErrUSBOpen{cause: cause}, "cause", cause},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}
