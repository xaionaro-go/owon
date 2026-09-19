package owonctl_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
)

// diagnosticFailures models failures at exact writes without assuming later writes also fail.
//
// Example: the initial parser diagnostic can fail while every usage write succeeds.
type diagnosticFailures struct {
	Failures map[int]error
	ShortAt  int
	Calls    int
	Output   bytes.Buffer
}

// Write applies the configured failure at a selected write and otherwise retains output.
//
// Example: a short write returns zero bytes with no error to exercise recorder normalization.
func (writer *diagnosticFailures) Write(data []byte) (int, error) {
	writer.Calls++
	if err := writer.Failures[writer.Calls]; err != nil {
		return 0, err
	}
	if writer.Calls == writer.ShortAt {
		return 0, nil
	}
	return writer.Output.Write(data)
}

// TestDiagnosticWriterValidatesItsSink checks construction and invalid zero-value access.
//
// Example: a typed nil bytes.Buffer cannot become an apparently usable diagnostic sink.
func TestDiagnosticWriterValidatesItsSink(t *testing.T) {
	var typedNil *bytes.Buffer
	for _, output := range []io.Writer{nil, typedNil} {
		writer, err := owonctl.NewDiagnosticWriter(output)
		require.Nil(t, writer)
		var configuration *owonctl.ErrConfiguration
		require.ErrorAs(t, err, &configuration)
	}
	for _, writer := range []*owonctl.DiagnosticWriter{nil, {}} {
		count, err := writer.Write([]byte("diagnostic"))
		require.Zero(t, count)
		require.Error(t, err)
		require.Error(t, writer.Err())
	}
}

// TestDiagnosticWriterRetainsOnlyActualFailures checks successful writes and immutable error reads.
//
// Example: a successful write between distinct failures adds no spurious cause to the history.
func TestDiagnosticWriterRetainsOnlyActualFailures(t *testing.T) {
	first := errors.New("first write failed")
	later := errors.New("later write failed")
	sink := &diagnosticFailures{Failures: map[int]error{1: first, 3: later}, ShortAt: 4}
	writer, err := owonctl.NewDiagnosticWriter(sink)
	require.NoError(t, err)
	require.NoError(t, writer.Err())
	for _, cause := range []error{first, nil, later, io.ErrShortWrite} {
		count, writeErr := writer.Write([]byte("text"))
		if cause == nil {
			require.NoError(t, writeErr)
			require.Equal(t, 4, count)
			continue
		}
		require.ErrorIs(t, writeErr, cause)
		require.Zero(t, count)
	}
	require.Equal(t, "text", sink.Output.String())
	for range 3 {
		err := writer.Err()
		for _, cause := range []error{first, later, io.ErrShortWrite} {
			require.ErrorIs(t, err, cause)
			require.Equal(t, 1, strings.Count(err.Error(), cause.Error()))
		}
	}
}

// TestDiagnosticWriterJoinsDistinctWriteEvents verifies exact per-write deduplication.
//
// Example: two writes returning one sentinel survive while the already-returned event appears once.
func TestDiagnosticWriterJoinsDistinctWriteEvents(t *testing.T) {
	cause := errors.New("closed destination")
	writer, err := owonctl.NewDiagnosticWriter(&diagnosticFailures{Failures: map[int]error{1: cause, 2: cause}})
	require.NoError(t, err)
	_, first := writer.Write([]byte("first"))
	_, second := writer.Write([]byte("second"))
	require.NotSame(t, first, second)
	combined := writer.Join(first)
	require.ErrorIs(t, combined, first)
	require.ErrorIs(t, combined, second)
	require.Equal(t, 2, strings.Count(combined.Error(), cause.Error()))
	require.Equal(t, 2, strings.Count(writer.Join(errors.Join(first, second)).Error(), cause.Error()))
}
