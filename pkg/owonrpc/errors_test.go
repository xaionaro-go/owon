package owonrpc

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
		{(*ErrInvalidInput)(nil), "invalid RPC input", nil},
		{new(ErrInvalidInput), "invalid RPC input", nil},
		{&ErrInvalidInput{Reason: "request", Cause: cause}, "request: cause", cause},
		{(*ErrEndpoint)(nil), "", nil},
		{&ErrEndpoint{Reason: "invalid"}, "invalid", nil},
		{&ErrEndpoint{Value: "tcp://", Reason: "invalid", Cause: cause}, "endpoint \"tcp://\": invalid: cause", cause},
		{(*ErrTLSConfiguration)(nil), "invalid TLS configuration", nil},
		{&ErrTLSConfiguration{}, "invalid TLS configuration", nil},
		{&ErrTLSConfiguration{Cause: cause}, "invalid TLS configuration: cause", cause},
		{&ErrTLSConfiguration{Reason: "certificate"}, "certificate", nil},
		{&ErrTLSConfiguration{Reason: "certificate", Cause: cause}, "certificate: cause", cause},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}
