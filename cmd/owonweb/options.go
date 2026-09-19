package main

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonweb"
)

const (
	// defaultHTTPListen keeps the browser bridge on loopback.
	//
	// Example: localhost browsers connect to port 8080.
	defaultHTTPListen = "127.0.0.1:8080"
)

// webOptions contains the executable's listener, connection, and logging settings.
//
// Example: remote gRPC requires a complete mTLS configuration.
type webOptions struct {
	HTTPListen string
	GRPCTarget string
	TLS        owonrpc.ClientTLSConfig
	LogLevel   logger.Level
}

// newCommand binds executable settings directly to Cobra's parser and generated help.
//
// Example: --help returns before runServer opens a connection or loads TLS files.
func newCommand(options *webOptions) *cobra.Command {
	command := &cobra.Command{
		Use:           "owonweb",
		Short:         "Serve a browser bridge to owond",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          options.runServer,
	}
	flags := command.Flags()
	flags.StringVar(&options.HTTPListen, "listen", defaultHTTPListen, "HTTP listen address (no authentication or encryption)")
	flags.StringVar(&options.GRPCTarget, "grpc", owonrpc.DefaultEndpoint(), "owond gRPC endpoint")
	flags.StringVar(&options.TLS.CAFile, "ca", "", "trusted CA PEM for remote TCP TLS")
	flags.StringVar(&options.TLS.CertificateFile, "cert", "", "client certificate PEM for remote mutual TLS")
	flags.StringVar(&options.TLS.PrivateKeyFile, "key", "", "client private key PEM for remote mutual TLS")
	flags.StringVar(&options.TLS.ServerName, "server-name", "", "expected remote server certificate name")
	options.LogLevel = logger.LevelInfo
	flags.Var(&options.LogLevel, "log-level", "logging level")
	return command
}

// run executes the command and preserves failures from Cobra's void help callback.
//
// Example: a short help write returns an error instead of reporting successful delivery.
func run(
	ctx context.Context,
	arguments []string,
	output io.Writer,
) error {
	recordedOutput, err := owonctl.NewDiagnosticWriter(output)
	if err != nil {
		return err
	}
	command := newCommand(new(webOptions))
	command.SetArgs(arguments)
	command.SetOut(recordedOutput)
	// Logrus owns log delivery; only generated help has a delivery-error contract.
	command.SetErr(output)
	return recordedOutput.Join(command.ExecuteContext(ctx))
}

// validateHTTPListen checks address syntax without restricting the operator's bind host.
//
// Example: 0.0.0.0:8080 deliberately exposes HTTP on all IPv4 interfaces.
func validateHTTPListen(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return &owonweb.ErrInvalidWebRequest{Reason: "--listen must not be empty"}
	}
	_, portText, err := net.SplitHostPort(value)
	if err != nil {
		return &owonweb.ErrInvalidWebRequest{Reason: "--listen must be a host:port address", Cause: err}
	}
	if _, err := strconv.ParseUint(portText, 10, 16); err != nil {
		return &owonweb.ErrInvalidWebRequest{Reason: "--listen port is invalid", Cause: err}
	}
	// Port zero supports ephemeral service managers and listener tests.
	return nil
}
