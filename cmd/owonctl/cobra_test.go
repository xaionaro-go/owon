package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRootWithoutArgumentsPrintsHelp verifies ordinary command discovery succeeds.
//
// Example: invoking owonctl without arguments never needs a daemon.
func TestRootWithoutArgumentsPrintsHelp(t *testing.T) {
	var output bytes.Buffer
	application, err := newApplication(&output)
	require.NoError(t, err)
	require.NoError(t, application.Run(t.Context(), nil))
	require.Contains(t, output.String(), "Available Commands:")
}

// TestPersistentFlagsWorkOnEitherSide verifies Cobra owns shared flag placement.
//
// Example: info --address ENDPOINT and --address ENDPOINT info issue the same RPC.
func TestPersistentFlagsWorkOnEitherSide(t *testing.T) {
	service := &commandService{Calls: make(chan rpcInvocation, 4)}
	endpoint := startCommandService(t, service)
	for _, arguments := range [][]string{
		{"--address", endpoint, "info"},
		{"info", "--address", endpoint},
	} {
		var output bytes.Buffer
		require.NoError(t, executeCommand(t.Context(), arguments, &output, io.Discard))
		require.Equal(t, "GetDeviceInfo", (<-service.Calls).Method)
		require.Contains(t, output.String(), "fixture")
		require.Empty(t, service.Calls)
	}
}

// TestHelpAndResponseWriteFailuresAppearOnce verifies generated help and data delivery.
//
// Example: one failed output write is not duplicated by the recording boundary.
func TestHelpAndResponseWriteFailuresAppearOnce(t *testing.T) {
	cause := errors.New("destination closed")
	for _, arguments := range [][]string{nil, {"--help"}, {"info", "--help"}} {
		writer := &countedFailure{Cause: cause}
		err := executeCommand(t.Context(), arguments, writer, io.Discard)
		require.ErrorIs(t, err, cause)
		require.Positive(t, writer.Calls)
		require.Equal(t, writer.Calls, strings.Count(err.Error(), cause.Error()))
	}
	service := &commandService{Calls: make(chan rpcInvocation, 1)}
	endpoint := startCommandService(t, service)
	err := executeCommand(t.Context(), []string{"info", "--address", endpoint}, commandOutputFailure{Cause: cause}, io.Discard)
	require.ErrorIs(t, err, cause)
	require.Equal(t, 1, strings.Count(err.Error(), cause.Error()))
	require.Equal(t, "GetDeviceInfo", (<-service.Calls).Method)
	require.Empty(t, service.Calls)
}

// countedFailure counts actual failed writes without collapsing shared causes.
//
// Example: generated help may make several distinct writes to the same closed sink.
type countedFailure struct {
	Cause error
	Calls int
}

// Write records and rejects one actual output attempt.
//
// Example: the returned sentinel is shared, but each call is a separate write event.
func (writer *countedFailure) Write(_ []byte) (int, error) {
	writer.Calls++
	return 0, writer.Cause
}

// TestCobraRejectsUnknownCommandsAndOperands verifies malformed invocations stay local.
//
// Example: neither a typo nor an extra operand can silently execute the info operation.
func TestCobraRejectsUnknownCommandsAndOperands(t *testing.T) {
	service := &commandService{Calls: make(chan rpcInvocation, 2)}
	endpoint := startCommandService(t, service)
	for _, arguments := range [][]string{{"unknown"}, {"info", "stray"}, {"info", "--unknown"}, {"-address", endpoint, "info"}} {
		var output bytes.Buffer
		err := executeCommand(t.Context(), append([]string{"--address", endpoint}, arguments...), &output, io.Discard)
		require.Error(t, err)
		require.Empty(t, output.String())
		require.Empty(t, service.Calls)
		require.Zero(t, service.Connections.Load())
	}
}
