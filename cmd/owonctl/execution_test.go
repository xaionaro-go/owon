package main

import (
	"context"
	"io"

	"github.com/xaionaro-go/owon/pkg/owonctl"
)

// testApplication runs each invocation through a fresh real Cobra command.
//
// Example: tests reuse one output sink without retaining parser state between invocations.
type testApplication struct{ Output io.Writer }

// newApplication validates the test's output destination.
//
// Example: a nil destination is rejected before command execution.
func newApplication(output io.Writer) (*testApplication, error) {
	if _, err := owonctl.NewDiagnosticWriter(output); err != nil {
		return nil, err
	}
	return &testApplication{Output: output}, nil
}

// Run exercises the public execution boundary with discarded diagnostics.
//
// Example: command tests inspect stdout independently of operational logging.
func (application *testApplication) Run(
	ctx context.Context,
	arguments []string,
) error {
	return executeCommand(ctx, arguments, application.Output, io.Discard)
}
