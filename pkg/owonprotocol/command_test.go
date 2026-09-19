package owonprotocol

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCommandLengthBoundary verifies the exact command allocation cap and rejects unsupported framing.
//
// Example: a maximum-length printable command is valid while one additional byte is rejected.
func TestCommandLengthBoundary(t *testing.T) {
	t.Parallel()
	command := Command{Text: strings.Repeat("A", DefaultMaximumCommandBytes), ResponseMode: ResponseModeNone}
	require.NoError(t, ValidateCommand(command))
	command.Text += "A"
	_, err := CommandBytes(command)
	requireErrorType[*ErrInvalidCommand](t, err)
	command = Command{Text: "*IDN?", ResponseMode: ResponseMode(999)}
	requireErrorType[*ErrInvalidCommand](t, ValidateCommand(command))
}

// TestValidateCommandRejectsAllNonPrintableBytes verifies raw commands stay ASCII-safe.
//
// Example: ESC, DEL, and non-ASCII bytes cannot reach the USB endpoint.
func TestValidateCommandRejectsAllNonPrintableBytes(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"*IDN?\x01", "*IDN?\x1b", "*IDN?\x7f", "*IDN?\u00e9", "\t*IDN?"} {
		requireErrorType[*ErrInvalidCommand](t, ValidateCommand(Command{Text: value, ResponseMode: ResponseModeASCII}))
	}
}

// TestCommandBytesKeepsPrintableSCPI verifies valid commands remain line framed.
//
// Example: surrounding ASCII spaces are normalized before the terminating LF is added.
func TestCommandBytesKeepsPrintableSCPI(t *testing.T) {
	t.Parallel()

	got, err := CommandBytes(Command{Text: " *IDN? ", ResponseMode: ResponseModeASCII})
	require.NoError(t, err)
	require.Equal(t, []byte("*IDN?\n"), got)
}
