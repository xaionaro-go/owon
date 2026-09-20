package owonscpi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestShutdownTimerCommandsUseTheObservedDialect verifies the exact timer query and setter grammar.
//
// Example: the keep-awake policy uses one ASCII query and one no-response UNLIMITED write.
func TestShutdownTimerCommandsUseTheObservedDialect(t *testing.T) {
	require.Equal(t, ":SHUTdown:TIMe?", ShutdownTimerQuery().Text)
	require.Equal(t, owonprotocol.ResponseModeASCII, ShutdownTimerQuery().ResponseMode)
	require.Equal(t, ":SHUTdown:TIMe UNLIMITED", ShutdownTimerUnlimitedCommand().Text)
	require.Equal(t, owonprotocol.ResponseModeNone, ShutdownTimerUnlimitedCommand().ResponseMode)
}

// TestParseShutdownTimerAcceptsExactObservedTokens verifies the four firmware timer replies.
//
// Example: `Unlimited` is the only state that satisfies the keep-awake policy.
func TestParseShutdownTimerAcceptsExactObservedTokens(t *testing.T) {
	for _, testCase := range []struct {
		reply string
		want  ShutdownTimer
	}{
		{reply: "10min", want: ShutdownTimer10Minutes},
		{reply: "30min", want: ShutdownTimer30Minutes},
		{reply: "60min", want: ShutdownTimer60Minutes},
		{reply: "Unlimited", want: ShutdownTimerUnlimited},
	} {
		got, err := ParseShutdownTimer([]byte(testCase.reply))
		require.NoError(t, err, testCase.reply)
		require.Equal(t, testCase.want, got, testCase.reply)
	}
}

// TestParseShutdownTimerRejectsUnrecognizedTokens verifies fail-closed timer parsing.
//
// Example: mixed-case and empty replies never authorize a setter or an awake claim.
func TestParseShutdownTimerRejectsUnrecognizedTokens(t *testing.T) {
	for _, reply := range []string{"", "10MIN", "unlimited", "error", "90min", "Unlimited extra"} {
		_, err := ParseShutdownTimer([]byte(reply))
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, reply)
		require.Contains(t, err.Error(), "shutdown timer", reply)
	}
	_, err := ParseShutdownTimer(nil)
	var malformed *ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
	require.False(t, errors.Is(err, nil))
}
