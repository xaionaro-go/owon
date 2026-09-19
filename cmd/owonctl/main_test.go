package main

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
)

// TestNewApplicationRegistersEverySubcommand verifies the binary wiring catalog.
//
// Example: top-level help lists all eight independently implemented commands.
func TestNewApplicationRegistersEverySubcommand(t *testing.T) {
	var output bytes.Buffer
	application, err := newApplication(&output)
	require.NoError(t, err)
	require.NoError(t, application.Run(context.Background(), []string{"--help"}))
	for _, name := range []string{"info", "state", "execute", "run", "stop", "single", "dmm", "waveform"} {
		require.Contains(t, output.String(), name)
	}
}

// TestApplicationParsesHelpAtTheCommandBoundary rejects help-like operands and flag values.
//
// Example: execute -- --help sends literal bytes; dmm -range --help rejects an invalid range.
func TestApplicationParsesHelpAtTheCommandBoundary(t *testing.T) {
	application, err := newApplication(io.Discard)
	require.NoError(t, err)
	err = application.Run(context.Background(), []string{"--address", "invalid", "execute", "--", "--help"})
	var configuration *owonctl.ErrConfiguration
	require.ErrorAs(t, err, &configuration)
	require.Equal(t, "address", configuration.Field)
	err = application.Run(context.Background(), []string{"--address", "invalid", "dmm", "--range", "--help"})
	var arguments *owonctl.ErrCommandArguments
	require.ErrorAs(t, err, &arguments)
	require.Contains(t, err.Error(), "range")
	err = application.Run(context.Background(), []string{"--address", "invalid", "info", "--help"})
	require.NoError(t, err)
}
