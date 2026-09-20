package owonscpi

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// ShutdownTimer identifies one exact shutdown-time token observed from the instrument.
//
// Example: ShutdownTimerUnlimited is the only value accepted by the keep-awake readback.
type ShutdownTimer string

const (
	// ShutdownTimer10Minutes identifies the firmware's ten-minute shutdown token.
	//
	// Example: a ten-minute readback is eligible for one bounded Unlimited write.
	ShutdownTimer10Minutes ShutdownTimer = "10min"
	// ShutdownTimer30Minutes identifies the firmware's thirty-minute shutdown token.
	//
	// Example: a thirty-minute readback is eligible for one bounded Unlimited write.
	ShutdownTimer30Minutes ShutdownTimer = "30min"
	// ShutdownTimer60Minutes identifies the firmware's sixty-minute shutdown token.
	//
	// Example: a sixty-minute readback is eligible for one bounded Unlimited write.
	ShutdownTimer60Minutes ShutdownTimer = "60min"
	// ShutdownTimerUnlimited identifies the firmware's no-shutdown token.
	//
	// Example: an exact Unlimited preflight still receives the one-shot refresh write.
	ShutdownTimerUnlimited ShutdownTimer = "Unlimited"
)

// ShutdownTimerQuery selects the ASCII shutdown-time readback command.
//
// Example: startup policy reads the timer before issuing its one-shot setter.
func ShutdownTimerQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":SHUTdown:TIMe?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// ShutdownTimerUnlimitedCommand selects the one-shot no-shutdown setter.
//
// Example: the daemon sends this only after a valid timer readback.
func ShutdownTimerUnlimitedCommand() owonprotocol.Command {
	return owonprotocol.Command{Text: ":SHUTdown:TIMe UNLIMITED", ResponseMode: owonprotocol.ResponseModeNone}
}

// ParseShutdownTimer parses one exact firmware shutdown-time token.
//
// Example: malformed or mixed-case replies fail closed before any setter is permitted.
func ParseShutdownTimer(response []byte) (ShutdownTimer, error) {
	token := strings.TrimSpace(string(response))
	switch ShutdownTimer(token) {
	case ShutdownTimer10Minutes, ShutdownTimer30Minutes, ShutdownTimer60Minutes, ShutdownTimerUnlimited:
		return ShutdownTimer(token), nil
	default:
		return "", fmt.Errorf("parse shutdown timer %q: %w", token, &ErrMalformedResponse{Reason: "shutdown timer token is unsupported"})
	}
}
