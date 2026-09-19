package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// invalidInvocations describes domain-invalid requests accepted by the flag grammar.
//
// Example: voltage without an AC/DC selection is syntactically valid but cannot execute.
func invalidInvocations() [][]string {
	return [][]string{
		{"dmm", "--function", "voltage"},
		{"dmm", "--function", "current"},
		{"dmm", "--current-type", "dc"},
		{"dmm", "--function", "resistance", "--current-type", "dc"},
		{"dmm", "--auto-range=false"},
		{"dmm", "--range", "invalid"},
		{"execute", ""},
		{"execute", "*IDN?;:RUN"},
		{"execute", "*IDN?\n"},
		{"execute", ":\xff"},
		{"execute", strings.Repeat("X", owonprotocol.DefaultMaximumCommandBytes+1)},
	}
}

// TestPreparationRejectsDomainErrorsBeforeEndpointAndRPC tests validation at the actual application boundary.
//
// Example: an invalid request must outrank a malformed address or missing remote TLS files.
func TestPreparationRejectsDomainErrorsBeforeEndpointAndRPC(t *testing.T) {
	service := &commandService{Calls: make(chan rpcInvocation, 32)}
	endpoint := startCommandService(t, service)
	application, err := newApplication(io.Discard)
	require.NoError(t, err)
	for globalIndex, global := range [][]string{
		{"--address", "invalid"},
		{"--address", "tcp://192.0.2.1:50051", "--ca", "/nonexistent/ca", "--cert", "/nonexistent/cert", "--key", "/nonexistent/key", "--server-name", "scope"},
		{"--address", endpoint},
	} {
		for invocationIndex, invocation := range invalidInvocations() {
			t.Run(fmt.Sprintf("endpoint-%d/%s-%d", globalIndex, invocation[0], invocationIndex),
				// rejectBeforeDial checks each invalid invocation independently across transport configurations.
				//
				// Example: a separator-bearing raw command must fail even when TLS credentials are absent.
				func(t *testing.T) {
					err := application.Run(t.Context(), append(append([]string(nil), global...), invocation...))
					require.Error(t, err, "%q", invocation)
					var configuration *owonctl.ErrConfiguration
					require.False(t, errors.As(err, &configuration), "%v", err)
					require.NotContains(t, err.Error(), "load client TLS")
					switch invocation[0] {
					case "execute":
						var invalid *owonprotocol.ErrInvalidCommand
						require.ErrorAs(t, err, &invalid)
					case "dmm":
						var invalid *owonmodel.ErrInvalidRequest
						var unsupported *owonscpi.ErrUnsupportedControl
						var spelling *owonctl.ErrCommandArguments
						require.True(t, errors.As(err, &invalid) || errors.As(err, &unsupported) || errors.As(err, &spelling), "%v", err)
					}
					require.Empty(t, service.Calls, "invalid request must issue no RPC")
					require.Zero(t, service.Connections.Load(), "invalid request must open no connection")
				},
			)
		}
	}
}
