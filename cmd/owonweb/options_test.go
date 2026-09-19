package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

// TestCommandDefaults keeps endpoint and logging defaults aligned with the service.
//
// Example: no flags select a rootless Unix daemon and loopback HTTP with Info logs.
func TestCommandDefaults(t *testing.T) {
	for _, runtimeDirectory := range []string{"/run/user/1234", ""} {
		t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
		options := new(webOptions)
		command := newCommand(options)
		require.NoError(t, command.ParseFlags(nil))
		require.Equal(t, owonrpc.DefaultEndpoint(), options.GRPCTarget)
		require.Equal(t, defaultHTTPListen, options.HTTPListen)
		require.Equal(t, logger.LevelInfo, options.LogLevel)
	}
}

// TestCommandGeneratedHelpPrecedesStartup proves help neither opens TLS files nor binds HTTP.
//
// Example: nonexistent certificates and an invalid listen address cannot prevent --help.
func TestCommandGeneratedHelpPrecedesStartup(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), []string{"--grpc", "tcp://scope.example:50051", "--ca", "/missing/ca", "--cert", "/missing/cert", "--key", "/missing/key", "--server-name", "scope.example", "--listen", "invalid", "--help"}, &output)
	require.NoError(t, err)
	require.Contains(t, output.String(), "Usage:")
	require.Contains(t, output.String(), "--log-level")
	require.Contains(t, output.String(), "(default info)")
	require.NotContains(t, output.String(), "HTTP listening")
	require.NotContains(t, output.String(), "level=")
}

// TestCommandRejectsMalformedInputs verifies parsing fails before startup side effects.
//
// Example: a trailing operand or unknown flag cannot launch a listener.
func TestCommandRejectsMalformedInputs(t *testing.T) {
	for _, arguments := range [][]string{{"--unknown"}, {"operand"}, {"--", "operand"}, {"operand", "--listen", "0.0.0.0:80"}, {"--log-level", "invalid"}, {"--listen"}, {"--grpc", "bad://endpoint"}} {
		var output bytes.Buffer
		require.Error(t, run(context.Background(), arguments, &output))
		require.NotContains(t, output.String(), "HTTP listening")
	}
}

// failingOutput reports one stable output cause at the command output boundary.
//
// Example: help against a closed output must fail.
type failingOutput struct{ Cause error }

// Write reports the injected output failure without accepting bytes.
//
// Example: generated help cannot hide a broken pipe.
func (output failingOutput) Write(
	data []byte,
) (int, error) {
	return 0, output.Cause
}

// shortOutput simulates a writer that violates the full-write contract without an error.
//
// Example: help detects a truncated pipe write through io.ErrShortWrite.
type shortOutput struct{}

// Write deliberately accepts no bytes to exercise output-contract validation.
//
// Example: callers must not report successful help when its output was lost.
func (shortOutput) Write([]byte) (int, error) { return 0, nil }

// TestCommandPreservesHelpOutputFailures distinguishes successful help from failed delivery.
//
// Example: nil, broken and short-writing sinks return errors without opening a daemon.
func TestCommandPreservesHelpOutputFailures(t *testing.T) {
	var nilOutput *bytes.Buffer
	require.Error(t, run(context.Background(), []string{"--help"}, nilOutput))
	require.Error(t, run(context.Background(), nil, nil))
	cause := errors.New("output closed")
	require.ErrorIs(t, run(context.Background(), []string{"--help"}, failingOutput{Cause: cause}), cause)
	require.ErrorIs(t, run(context.Background(), []string{"--help"}, shortOutput{}), io.ErrShortWrite)
}

// TestCommandRejectsInvalidStartup covers public startup failures before device access.
//
// Example: invalid TLS policy and an occupied HTTP port return specific errors.
func TestCommandRejectsInvalidStartup(t *testing.T) {
	for _, arguments := range [][]string{
		{"--listen", ""},
		{"--listen", "missing-port"},
		{"--listen", "127.0.0.1:not-a-port"},
		{"--grpc", "unix:///tmp/owond.sock", "--ca", "ca.pem"},
		{"--grpc", "tcp://scope.example:50051"},
		{"--grpc", "tcp://scope.example:50051", "--ca", "missing", "--cert", "missing", "--key", "missing", "--server-name", "scope.example"},
	} {
		require.Error(t, run(context.Background(), arguments, io.Discard))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(
		// closeListener releases only the port owned by this test.
		//
		// Example: the occupied-port assertion cannot retain a listener afterward.
		func() { require.NoError(t, listener.Close()) })
	require.ErrorContains(t, run(context.Background(), []string{"--listen", listener.Addr().String()}, io.Discard), "listen HTTP")
}

// TestMainHelpSucceedsWithoutDaemon executes the process boundary's successful help path.
//
// Example: help prints usage without opening a daemon connection or exiting unsuccessfully.
func TestMainHelpSucceedsWithoutDaemon(t *testing.T) {
	originalArguments, originalStderr := os.Args, os.Stderr
	output, err := os.CreateTemp(t.TempDir(), "help")
	require.NoError(t, err)
	// restoreProcess keeps test-local process state from escaping this assertion.
	//
	// Example: later tests retain the original argument and output handles.
	defer func() { os.Args = originalArguments; os.Stderr = originalStderr; require.NoError(t, output.Close()) }()
	os.Args = []string{"owonweb", "--help"}
	os.Stderr = output
	main()
	data, err := os.ReadFile(output.Name())
	require.NoError(t, err)
	require.Contains(t, string(data), "Usage:")
	require.NotContains(t, string(data), "HTTP listening")
}
