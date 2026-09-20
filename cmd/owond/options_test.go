package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"github.com/xaionaro-go/owon/pkg/owonusb"
)

// parseOptions exercises the real Cobra configuration hooks without hardware execution.
//
// Example: transport-policy tests inspect validated defaults without opening USB.
func parseOptions(
	arguments []string,
	output io.Writer,
) (options, error) {
	diagnostics, err := owonctl.NewDiagnosticWriter(output)
	if err != nil {
		return options{}, err
	}
	handler := &daemonCommand{LogOutput: io.Discard}
	command := handler.Command()
	command.SetOut(diagnostics)
	command.SetErr(diagnostics)
	command.SetArgs(arguments)
	command.RunE = configurationOnly
	err = command.ExecuteContext(context.Background())
	return handler.Config, diagnostics.Join(err)
}

// configurationOnly ends a configuration test after Cobra has validated the command.
//
// Example: a test can inspect TLS policy without loading certificate files.
func configurationOnly(
	_ *cobra.Command,
	_ []string,
) error {
	return nil
}

// brokenOutput represents a closed diagnostics destination.
//
// Example: --help must fail when its output cannot be written.
type brokenOutput struct{ Cause error }

// TestDaemonSupportsNativeHelpAndLogLevel keeps help independent of startup validation.
//
// Example: help with an unusable TLS path exits successfully without USB acquisition.
func TestDaemonSupportsNativeHelpAndLogLevel(t *testing.T) {
	var output bytes.Buffer
	err := run(t.Context(), []string{"--log-level", "trace", "--tls-cert", "/absent/cert", "--help"}, &output, io.Discard)
	require.NoError(t, err)
	require.Contains(t, output.String(), "Usage:")
	require.Contains(t, output.String(), "--log-level")
	require.NotContains(t, output.String(), "starting")
}

// Write reports the configured output failure without accepting bytes.
//
// Example: flag parser output retains a broken pipe cause.
func (output brokenOutput) Write(_ []byte) (int, error) { return 0, output.Cause }

// TestDaemonRejectsOperandsBeforeHardware prevents flag parsing from silently ignoring trailing input.
//
// Example: --serial example stray cannot open USB or listen.
func TestDaemonRejectsOperandsBeforeHardware(t *testing.T) {
	for _, arguments := range [][]string{{"--serial", "example", "stray"}, {"--serial", "example", "--", "stray"}} {
		err := run(context.Background(), arguments, io.Discard, io.Discard)
		var configuration *ErrConfiguration
		require.ErrorAs(t, err, &configuration)
		require.Equal(t, "arguments", configuration.Field)
	}
}

// TestDaemonOptionsRejectIncompleteConfiguration checks all validation before device discovery.
//
// Example: missing TLS files fail before opening a remote listener or instrument.
func TestDaemonOptionsRejectIncompleteConfiguration(t *testing.T) {
	for _, arguments := range [][]string{
		{"--serial", "example", "--model", " "},
		{"--serial", "example", "--listen", "invalid"},
		{"--serial", "example", "--listen", "tcp://192.0.2.1:1234"},
		{"--log-level", "invalid"},
	} {
		_, err := parseOptions(arguments, io.Discard)
		var configuration *ErrConfiguration
		require.ErrorAs(t, err, &configuration)
		require.NotEmpty(t, err.Error())
	}
	_, err := parseOptions(nil, nil)
	require.ErrorContains(t, err, "output")
	require.ErrorContains(t, run(nil, nil, io.Discard, io.Discard), "context")
	err = run(context.Background(), []string{"--serial", "example", "--listen", "tcp://192.0.2.1:1234", "--tls-cert", "/nonexistent/cert", "--tls-key", "/nonexistent/key", "--tls-client-ca", "/nonexistent/ca", "--tls-client-san", "scope"}, io.Discard, io.Discard)
	require.ErrorContains(t, err, "load server TLS credentials")
	require.NoError(t, closeSession(t.Context(), nil))
}

// TestBuiltDaemonRejectsOperandsAndSupportsHelp exercises the compiled process before USB access.
//
// Example: stray operands exit nonzero without a USB-discovery diagnostic.
func TestBuiltDaemonRejectsOperandsAndSupportsHelp(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "owond")
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput()
	require.NoError(t, err, "%s", output)
	output, err = exec.CommandContext(t.Context(), binary, "--help").CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "Usage:")
	require.Contains(t, string(output), owonrpc.DefaultEndpoint())
	require.Contains(t, string(output), owonusb.DefaultModel)
	require.Contains(t, string(output), "default "+strconv.Itoa(owonserver.DefaultMaximumActiveSubscriptions))
	output, err = exec.CommandContext(t.Context(), binary, "--serial", "__owon_style_absent_serial__").CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "starting OWON daemon")
	require.Contains(t, string(output), "requested_serial=__owon_style_absent_serial__")
	require.Contains(t, string(output), "service=owond")
	require.NotContains(t, string(output), "gRPC listening")
	require.NotContains(t, string(output), "instrument closed")
	for _, arguments := range [][]string{{"--serial", "example", "stray"}, {"--serial", "example", "--", "stray"}, {"--unknown"}} {
		output, err = exec.CommandContext(t.Context(), binary, arguments...).CombinedOutput()
		require.Error(t, err)
		require.NotContains(t, string(output), "open OWON USB")
	}
	var help bytes.Buffer
	require.NoError(t, run(t.Context(), []string{"--help"}, &help, io.Discard))
	require.Contains(t, help.String(), "Usage:")
}

// TestDaemonDefaultsMatchCorePolicy checks the daemon consumes the common endpoint, model, and admission defaults.
//
// Example: no arguments selects automatic discovery and the shared service policy defaults.
func TestDaemonDefaultsMatchCorePolicy(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1234")
	configuration, err := parseOptions(nil, io.Discard)
	require.NoError(t, err)
	require.Empty(t, configuration.Serial)
	require.Equal(t, "/run/user/1234/owon/owond.sock", configuration.Endpoint.Address)
	require.Equal(t, owonusb.DefaultModel, configuration.Model)
	require.Equal(t, owonserver.DefaultMaximumActiveSubscriptions, configuration.MaximumSubscriptions)
	require.True(t, configuration.KeepAwake)
	require.Equal(t, logger.LevelInfo, configuration.LogLevel)
}

// TestDaemonKeepAwakeDefaultsEnabled verifies startup refresh is the default policy.
//
// Example: --no-keep-awake is the explicit opt-out from the startup refresh.
func TestDaemonKeepAwakeDefaultsEnabled(t *testing.T) {
	configuration, err := parseOptions(nil, io.Discard)
	require.NoError(t, err)
	require.True(t, configuration.KeepAwake)
	configuration, err = parseOptions([]string{"--serial", "example", "--no-keep-awake"}, io.Discard)
	require.NoError(t, err)
	require.False(t, configuration.KeepAwake)
	_, err = parseOptions([]string{"--serial", "example", "--keep-awake"}, io.Discard)
	require.Error(t, err)
}

// TestDaemonEndpointOverrideIgnoresRuntimeEnvironment preserves explicit connection configuration.
//
// Example: a systemd socket remains selectable even when the login runtime directory is unusable.
func TestDaemonEndpointOverrideIgnoresRuntimeEnvironment(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/unusable/runtime")
	for _, target := range []string{"unix:///run/owond/owond.sock", "tcp://127.0.0.1:50051"} {
		configuration, err := parseOptions([]string{"--listen", target}, io.Discard)
		require.NoError(t, err)
		endpoint, err := owonrpc.ParseEndpoint(target)
		require.NoError(t, err)
		require.Equal(t, endpoint, configuration.Endpoint)
	}
}

// TestDaemonSerialIsOptional accepts blank auto-selection and normalizes explicit selection.
//
// Example: -serial ' scope ' selects scope, while omitted or whitespace input discovers one device.
func TestDaemonSerialIsOptional(t *testing.T) {
	for _, test := range []struct {
		Arguments []string
		Serial    string
	}{
		{},
		{Arguments: []string{"--serial", " \t "}},
		{Arguments: []string{"--serial", " scope "}, Serial: "scope"},
	} {
		configuration, err := parseOptions(test.Arguments, io.Discard)
		require.NoError(t, err)
		require.EqualValues(t, test.Serial, configuration.Serial)
		require.EqualValues(t, test.Serial, configuration.usbConfig().Serial)
	}
}

// TestDaemonRetainsFlagOutputErrors distinguishes successful help from failed diagnostics.
//
// Example: malformed flags preserve both invocation and output causes.
func TestDaemonRetainsFlagOutputErrors(t *testing.T) {
	cause := errors.New("closed output")
	_, err := parseOptions([]string{"--help"}, brokenOutput{Cause: cause})
	require.ErrorIs(t, err, cause)
	_, err = parseOptions([]string{"--unknown"}, brokenOutput{Cause: cause})
	require.NotErrorIs(t, err, cause)
	var configuration *ErrConfiguration
	require.ErrorAs(t, err, &configuration)
}

// diagnosticOutput fails selected writes so the parser cannot rely on the last output result.
//
// Example: an initial failed diagnostic followed by successful help must still fail parsing.
type diagnosticOutput struct {
	Failures map[int]error
	ShortAt  int
	Calls    int
}

// Write models independent output failures without introducing a permanently broken sink.
//
// Example: writes one and two can fail with different inspectable causes.
func (output *diagnosticOutput) Write(data []byte) (int, error) {
	output.Calls++
	if err := output.Failures[output.Calls]; err != nil {
		return 0, err
	}
	if output.Calls == output.ShortAt {
		return 0, nil
	}
	return len(data), nil
}

// TestDaemonRetainsIndependentDiagnosticFailures checks every actual help write.
//
// Example: first-only failures cannot disappear after the usage header succeeds.
func TestDaemonRetainsIndependentDiagnosticFailures(t *testing.T) {
	first := errors.New("first daemon diagnostic failed")
	later := errors.New("later daemon diagnostic failed")
	for _, test := range []struct {
		Failures map[int]error
		ShortAt  int
		Causes   []error
	}{
		{Failures: map[int]error{1: first}, Causes: []error{first}},
		{Failures: map[int]error{2: later}, Causes: []error{later}},
		{Failures: map[int]error{1: first, 2: later}, Causes: []error{first, later}},
		{ShortAt: 1, Causes: []error{io.ErrShortWrite}},
	} {
		_, err := parseOptions([]string{"--help"}, &diagnosticOutput{Failures: test.Failures, ShortAt: test.ShortAt})
		for _, cause := range test.Causes {
			require.ErrorIs(t, err, cause)
			require.Equal(t, 1, strings.Count(err.Error(), cause.Error()))
		}
	}
	var typedNil *bytes.Buffer
	_, err := parseOptions(nil, typedNil)
	require.ErrorContains(t, err, "output")
}

// TestDaemonSubscriptionLimitBounds checks signed input against the shared runtime policy.
//
// Example: negative and above-limit counts fail before any listener or USB access.
func TestDaemonSubscriptionLimitBounds(t *testing.T) {
	for _, count := range []int{-1, 0, owonserver.MaximumActiveSubscriptions + 1} {
		_, err := parseOptions([]string{"--serial", "example", "--max-subscriptions", strconv.Itoa(count)}, io.Discard)
		var configuration *ErrConfiguration
		require.ErrorAs(t, err, &configuration)
		require.Equal(t, "--max-subscriptions", configuration.Field)
	}
	for _, count := range []int{1, owonserver.MaximumActiveSubscriptions} {
		configuration, err := parseOptions([]string{"--serial", "example", "--max-subscriptions", strconv.Itoa(count)}, io.Discard)
		require.NoError(t, err)
		require.Equal(t, count, configuration.MaximumSubscriptions)
	}
}

// TestDaemonEndpointDiagnosticIncludesItsCauseOnce checks the listen context does not repeat parser text.
//
// Example: invalid endpoints remain inspectable through the daemon configuration wrapper.
func TestDaemonEndpointDiagnosticIncludesItsCauseOnce(t *testing.T) {
	_, err := parseOptions([]string{"--serial", "example", "--listen", "invalid"}, io.Discard)
	var endpoint *owonrpc.ErrEndpoint
	require.ErrorAs(t, err, &endpoint)
	require.Equal(t, 1, strings.Count(err.Error(), endpoint.Error()))
}
