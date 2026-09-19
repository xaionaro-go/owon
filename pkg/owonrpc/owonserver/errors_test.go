package owonserver

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
		{(*ErrInvalidInput)(nil), "invalid RPC server input", nil},
		{new(ErrInvalidInput), "invalid RPC server input", nil},
		{&ErrInvalidInput{Reason: "request", Cause: cause}, "request: cause", cause},
		{(*ErrInvalidConfig)(nil), "invalid RPC server configuration", nil},
		{new(ErrInvalidConfig), "invalid RPC server configuration", nil},
		{&ErrInvalidConfig{Reason: "limit", Cause: cause}, "limit: cause", cause},
		{(*ErrUnavailable)(nil), "RPC server resource unavailable", nil},
		{new(ErrUnavailable), "RPC server resource unavailable", nil},
		{&ErrUnavailable{Operation: "read", Resource: "queue", Reason: "closed", Cause: cause}, "read: queue closed: cause", cause},
		{(*ErrWaveformTooLarge)(nil), "waveform event exceeds RPC message limit", nil},
		{&ErrWaveformTooLarge{Size: 2, Limit: 1}, "waveform event is 2 bytes, maximum is 1", nil},
		{new(ErrCriticalBackpressure), "critical subscription capacity exhausted", nil},
		{new(ErrSubscriptionResourceExhausted), "subscription resource limit exhausted", nil},
	} {
		require.Equal(t, testCase.Text, testCase.Err.Error())
		require.Equal(t, testCase.Cause, errors.Unwrap(testCase.Err))
		if testCase.Cause != nil {
			require.ErrorIs(t, testCase.Err, cause)
		}
	}
}
