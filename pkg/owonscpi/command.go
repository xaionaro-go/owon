package owonscpi

import (
	"strconv"

	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// writeCommands assigns the no-response envelope to an already compiled ordered command batch.
//
// Example: typed setters emit no speculative response reads.
func writeCommands(texts []string) []owonprotocol.Command {
	commands := make([]owonprotocol.Command, len(texts))
	for index, text := range texts {
		commands[index] = owonprotocol.Command{Text: text, ResponseMode: owonprotocol.ResponseModeNone}
	}
	return commands
}

// onOff converts a boolean into OWON's control token.
//
// Example: false maps to `OFF`.
func onOff(value bool) string {
	if value {
		return "ON"
	}

	return "OFF"
}

// formatNumber formats finite numbers without unnecessary trailing zeros.
//
// Example: zero is formatted as `0`.
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
