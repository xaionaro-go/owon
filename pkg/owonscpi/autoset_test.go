package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestAutoCommandUsesTheSourceBackedCandidate verifies the exact candidate and
// its no-response framing without claiming that any physical firmware accepts it.
func TestAutoCommandUsesTheSourceBackedCandidate(t *testing.T) {
	command := AutoCommand()

	require.Equal(t, ":AUToseton", command.Text)
	require.Equal(t, owonprotocol.ResponseModeNone, command.ResponseMode)
}
