package owonctl

import (
	"errors"
	"fmt"
	"io"
)

// DiagnosticWriter records output failures from callbacks without error results.
//
// Example: Cobra's generated help retains failures through its void help callback.
type DiagnosticWriter struct {
	output io.Writer
	errors []error
}

// NewDiagnosticWriter validates the output sink before recording a parse's diagnostics.
//
// Example: a typed nil writer is rejected before flag parsing can invoke it.
func NewDiagnosticWriter(output io.Writer) (*DiagnosticWriter, error) {
	if output == nil || isNilWriter(output) {
		return nil, &ErrConfiguration{Field: "output", Reason: "must not be nil"}
	}

	return &DiagnosticWriter{output: output}, nil
}

// Write forwards bytes and records each actual failure, including invalid short writes.
//
// Example: two failing writes retain both independent causes even if intervening writes succeed.
func (writer *DiagnosticWriter) Write(data []byte) (int, error) {
	if writer == nil || writer.output == nil {
		return 0, &ErrConfiguration{Field: "diagnostic writer", Reason: "must be constructed with an output"}
	}
	count, err := writer.output.Write(data)
	if err == nil && count != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		err = fmt.Errorf("write diagnostics: %w", err)
		writer.errors = append(writer.errors, err)
	}

	return count, err
}

// Err returns the joined diagnostic history without changing it.
//
// Example: callers combine this result once with the independently classified parse failure.
func (writer *DiagnosticWriter) Err() error {
	if writer == nil || writer.output == nil {
		return &ErrConfiguration{Field: "diagnostic writer", Reason: "must be constructed with an output"}
	}

	return errors.Join(writer.errors...)
}

// Join adds only write events not already wrapped by the returned operation error.
//
// Example: one failed response write appears once, while separate failed writes remain distinct.
func (writer *DiagnosticWriter) Join(err error) error {
	var errs []error
	errs = append(errs, err)
	for _, writeErr := range writer.errors {
		if !errors.Is(err, writeErr) {
			errs = append(errs, writeErr)
		}
	}
	return errors.Join(errs...)
}
