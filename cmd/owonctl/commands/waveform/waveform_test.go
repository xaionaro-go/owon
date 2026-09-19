package waveform

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
)

// TestCommandRejectsUnsupportedChannelBeforeClientUse verifies channel validation is local.
//
// Example: channel three fails without opening a gRPC connection.
func TestCommandRejectsUnsupportedChannelBeforeClientUse(t *testing.T) {
	command := NewCommand(nil)
	command.SetOut(io.Discard)
	command.SetArgs([]string{"--channel", "3"})
	err := command.ExecuteContext(t.Context())
	var arguments *owonctl.ErrCommandArguments
	require.ErrorAs(t, err, &arguments)
}

// TestCommandHelpSucceedsWithoutClient verifies command discovery does not require hardware.
//
// Example: `waveform --help` exits successfully while the daemon is offline.
func TestCommandHelpSucceedsWithoutClient(t *testing.T) {
	command := NewCommand(nil)
	command.SetOut(io.Discard)
	command.SetArgs([]string{"--help"})
	err := command.ExecuteContext(t.Context())
	require.NoError(t, err)
}
