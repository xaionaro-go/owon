package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestDeviceTimeoutFlagAcceptsPositiveDuration verifies operator policy is parsed before device access.
//
// Example: a two-second budget is a valid daemon configuration without opening USB.
func TestDeviceTimeoutFlagAcceptsPositiveDuration(t *testing.T) {
	configuration, err := parseOptions([]string{"--serial", "example", "--device-timeout", "2s"}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, 2*time.Second, configuration.DeviceTimeout)
	require.Equal(t, configuration.DeviceTimeout, configuration.usbConfig().OperationTimeout)
	require.EqualValues(t, "example", configuration.usbConfig().Serial)
	require.Equal(t, "HDS2202S", configuration.usbConfig().ExpectedModel)
	configuration, err = parseOptions([]string{"--serial", "example"}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, owonsession.DefaultDeviceOperationTimeout, configuration.DeviceTimeout)
	require.Equal(t, configuration.DeviceTimeout, configuration.usbConfig().OperationTimeout)
	for _, value := range []string{"0", "-1s", "bad"} {
		err := run(t.Context(), []string{"--serial", "example", "--device-timeout", value}, io.Discard, io.Discard)
		var configurationError *ErrConfiguration
		require.ErrorAs(t, err, &configurationError)
		require.Contains(t, err.Error(), "device-timeout")
	}
}

// TestBuiltDaemonDeviceTimeout verifies real process parsing and failed-help output without USB access.
//
// Example: positive durations reach endpoint validation, while zero durations fail their own policy.
func TestBuiltDaemonDeviceTimeout(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "owond")
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput()
	require.NoError(t, err, "%s", output)
	output, err = exec.CommandContext(t.Context(), binary, "--help").CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "--device-timeout")
	require.Contains(t, string(output), "default 10s")
	for _, value := range []string{"1ms", "30s", "2m"} {
		output, err := exec.CommandContext(t.Context(), binary, "--serial", "example", "--device-timeout", value, "--listen", "invalid").CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(output), "invalid endpoint")
		require.NotContains(t, string(output), "open OWON USB")
	}
	for _, value := range []string{"0", "-1s", "bad"} {
		output, err := exec.CommandContext(t.Context(), binary, "--serial", "example", "--device-timeout", value).CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(output), "device-timeout")
		require.NotContains(t, string(output), "open OWON USB")
	}
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	require.NoError(t, err)
	t.Cleanup(
		// closeFull releases the deliberately failing diagnostics destination.
		//
		// Example: failed help cannot leak the descriptor into later subprocesses.
		func() { require.NoError(t, full.Close()) },
	)
	var diagnostics bytes.Buffer
	command := exec.CommandContext(t.Context(), binary, "--help")
	command.Stdout = full
	command.Stderr = &diagnostics
	require.Error(t, command.Run())
	require.Contains(t, diagnostics.String(), "write diagnostics")
	require.Contains(t, diagnostics.String(), "no space left on device")
}
