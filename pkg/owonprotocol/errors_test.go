package owonprotocol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireErrorType verifies that an error chain contains one concrete error type.
//
// Example: requireErrorType[*ErrInvalidCommand](t, err) checks a validation result.
func requireErrorType[T error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed T
	require.ErrorAs(t, err, &typed)
}

// TestResponseLimitErrorOwnsConfigurationFailure distinguishes caller policy from device data loss.
//
// Example: rejecting an excessive limit happens before the reader consumes any bytes.
func TestResponseLimitErrorOwnsConfigurationFailure(t *testing.T) {
	_, err := NormalizeResponseLimit(DefaultMaximumResponseBytes + 1)
	var invalid *ErrInvalidResponseLimit
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, DefaultMaximumResponseBytes+1, invalid.Limit)
	require.Nil(t, invalid.Unwrap())
	require.Contains(t, invalid.Error(), "invalid OWON response limit")
	require.Equal(t, "invalid OWON response limit", (*ErrInvalidResponseLimit)(nil).Error())
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
		{(*ErrInvalidCommand)(nil), "invalid OWON command", nil},
		{&ErrInvalidCommand{Reason: "empty"}, "invalid OWON command: empty", nil},
		{(*ErrMalformedResponse)(nil), "malformed OWON response", nil},
		{&ErrMalformedResponse{}, "malformed OWON response", nil},
		{&ErrMalformedResponse{Cause: cause}, "malformed OWON response: cause", cause},
		{&ErrMalformedResponse{Reason: "frame"}, "malformed OWON response: frame", nil},
		{&ErrMalformedResponse{Reason: "frame", Cause: cause}, "malformed OWON response: frame: cause", cause},
		{(*ErrResponseTooLarge)(nil), "OWON response exceeds configured limit", nil},
		{&ErrResponseTooLarge{Size: 2, Limit: 1}, "OWON response size 2 exceeds configured limit 1", nil},
		{(*ErrSurplusResponse)(nil), "surplus OWON response bytes", nil},
		{&ErrSurplusResponse{}, "surplus OWON response bytes", nil},
		{&ErrSurplusResponse{Bytes: 2, Cause: cause}, "surplus OWON response bytes: 2: cause", cause},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}

// TestValidationErrorsExposeConcreteTypes verifies caller-visible error classification.
//
// Example: invalid command content and frame allocation policy use distinct error types.
func TestValidationErrorsExposeConcreteTypes(t *testing.T) {
	err := ValidateCommand(Command{Text: "", ResponseMode: ResponseModeASCII})
	var invalidCommand *ErrInvalidCommand
	require.ErrorAs(t, err, &invalidCommand)

	_, err = NormalizeResponseLimit(DefaultMaximumResponseBytes + 1)
	var invalidRequest *ErrInvalidResponseLimit
	require.ErrorAs(t, err, &invalidRequest)
}
