package owonctl

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonclient"
)

const (
	// DefaultProgram names the operational client.
	//
	// Example: generated help identifies owonctl.
	DefaultProgram = "owonctl"
	// DefaultTimeout bounds an individual command's RPC work.
	//
	// Example: an unavailable instrument cannot block automation forever.
	DefaultTimeout = 10 * time.Second
)

// ClientConfig describes connection flags and command execution policy.
//
// Example: each command shares the root's address and timeout values.
type ClientConfig struct {
	Address string
	TLS     owonrpc.ClientTLSConfig
	Timeout time.Duration
}

// RegisterFlags installs shared client settings on the maintained parser.
//
// Example: the root's persistent flags work before or after a subcommand.
func (config *ClientConfig) RegisterFlags(flags *pflag.FlagSet) {
	flags.StringVar(&config.Address, "address", owonrpc.DefaultEndpoint(), "gRPC endpoint")
	flags.StringVar(&config.TLS.CAFile, "ca", "", "trusted CA PEM for remote TCP TLS")
	flags.StringVar(&config.TLS.CertificateFile, "cert", "", "client certificate PEM for remote mutual TLS")
	flags.StringVar(&config.TLS.PrivateKeyFile, "key", "", "client private key PEM for remote mutual TLS")
	flags.StringVar(&config.TLS.ServerName, "server-name", "", "expected remote server certificate name")
	flags.DurationVar(&config.Timeout, "timeout", DefaultTimeout, "unary RPC timeout")
}

// ConnectionConfig validates command policy and derives reusable transport settings.
//
// Example: request validation runs before this method loads any client credentials.
func (config *ClientConfig) ConnectionConfig() (owonclient.ConnectionConfig, error) {
	if config == nil {
		return owonclient.ConnectionConfig{}, &ErrConfiguration{Field: "client", Reason: "configuration is required"}
	}
	if config.Timeout <= 0 {
		return owonclient.ConnectionConfig{}, &ErrConfiguration{Field: "timeout", Reason: "must be positive"}
	}
	endpoint, err := owonrpc.ParseEndpoint(config.Address)
	if err != nil {
		return owonclient.ConnectionConfig{}, &ErrConfiguration{Field: "address", Reason: "invalid endpoint", Cause: err}
	}
	connection := owonclient.ConnectionConfig{Endpoint: endpoint, TLS: config.TLS}
	if err := connection.Validate(); err != nil {
		return owonclient.ConnectionConfig{}, err
	}
	return connection, nil
}

// ErrConfiguration identifies an invalid application configuration or option.
//
// Example: a non-positive timeout produces an ErrConfiguration for `timeout`.
type ErrConfiguration struct {
	Field  string
	Reason string
	Cause  error
}

// Error returns the field-specific configuration failure.
//
// Example: `timeout: must be positive` is rendered for a bad timeout.
func (err *ErrConfiguration) Error() string {
	if err == nil {
		return ""
	}
	detail := err.Reason
	if err.Cause != nil {
		detail = fmt.Sprintf("%s: %v", detail, err.Cause)
	}
	if err.Field == "" {
		return detail
	}

	return fmt.Sprintf("%s: %s", err.Field, detail)
}

// Unwrap returns the underlying parsing or validation failure.
//
// Example: callers can inspect an endpoint parser error with errors.As.
func (err *ErrConfiguration) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
