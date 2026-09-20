package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonserver"
	"github.com/xaionaro-go/owon/pkg/owonsession"
	"github.com/xaionaro-go/owon/pkg/owonusb"
)

// options contains validated daemon command-line configuration.
//
// Example: non-loopback TCP options include both TLS certificate paths.
type options struct {
	Endpoint             owonrpc.Endpoint
	Serial               owonmodel.SerialNumber
	Model                string
	TLSCert              string
	TLSKey               string
	ClientCA             string
	ClientSANs           []string
	ClientFingerprints   []string
	MaximumSubscriptions int
	DeviceTimeout        time.Duration
	KeepAwake            bool
	LogLevel             logger.Level
}

// daemonCommand owns native Cobra flags and their validated daemon policy.
//
// Example: Cobra validates configuration before Execute acquires an instrument.
type daemonCommand struct {
	Config             options
	NoKeepAwake        bool
	Listen             string
	Serial             string
	ClientSANs         string
	ClientFingerprints string
	LogOutput          io.Writer
}

// Command constructs a native Cobra command with hardware-independent help.
//
// Example: --help prints defaults without TLS validation or USB discovery.
func (handler *daemonCommand) Command() *cobra.Command {
	command := &cobra.Command{
		Use:           "owond",
		Short:         "Expose one OWON instrument through gRPC",
		Args:          validateArguments,
		PreRunE:       handler.Validate,
		RunE:          handler.Execute,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	command.SetFlagErrorFunc(flagConfigurationError)
	flags := command.Flags()
	flags.StringVar(&handler.Listen, "listen", owonrpc.DefaultEndpoint(), "gRPC endpoint: unix:///absolute/path or tcp://host:port")
	flags.StringVar(&handler.Serial, "serial", "", "USB serial number (auto-detect when exactly one matching device is connected)")
	flags.StringVar(&handler.Config.Model, "model", owonusb.DefaultModel, "required expected SCPI model")
	flags.StringVar(&handler.Config.TLSCert, "tls-cert", "", "server certificate for TCP TLS")
	flags.StringVar(&handler.Config.TLSKey, "tls-key", "", "server private key for TCP TLS")
	flags.StringVar(&handler.Config.ClientCA, "tls-client-ca", "", "CA PEM used to verify remote client certificates")
	flags.StringVar(&handler.ClientSANs, "tls-client-san", "", "comma-separated allowed client certificate SANs")
	flags.StringVar(&handler.ClientFingerprints, "tls-client-sha256", "", "comma-separated allowed client certificate SHA-256 fingerprints")
	flags.IntVar(&handler.Config.MaximumSubscriptions, "max-subscriptions", owonserver.DefaultMaximumActiveSubscriptions, fmt.Sprintf("maximum concurrent Subscribe streams (1-%d)", owonserver.MaximumActiveSubscriptions))
	flags.DurationVar(&handler.Config.DeviceTimeout, "device-timeout", owonsession.DefaultDeviceOperationTimeout, "positive timeout for each device exchange or recovery phase")
	flags.BoolVar(&handler.NoKeepAwake, "no-keep-awake", false, "do not refresh the instrument shutdown timer during startup")
	handler.Config.LogLevel = logger.LevelInfo
	flags.Var(&handler.Config.LogLevel, "log-level", "logging level: trace, debug, info, warning, error, fatal")

	return command
}

// validateArguments preserves typed diagnostics around Cobra's operand policy.
//
// Example: a stray operand is rejected before opening hardware.
func validateArguments(
	command *cobra.Command,
	arguments []string,
) error {
	if err := cobra.NoArgs(command, arguments); err != nil {
		return &ErrConfiguration{Field: "arguments", Reason: "does not accept positional arguments", Cause: err}
	}

	return nil
}

// flagConfigurationError classifies native pflag failures without replacing parsing.
//
// Example: an invalid log level remains a daemon configuration error.
func flagConfigurationError(
	_ *cobra.Command,
	err error,
) error {
	return &ErrConfiguration{Field: "flags", Reason: "contain invalid values", Cause: err}
}

// Validate enforces daemon configuration before TLS, USB, or listener I/O.
//
// Example: a remote listener without mutual TLS is rejected before acquisition.
func (handler *daemonCommand) Validate(
	_ *cobra.Command,
	_ []string,
) error {
	config := &handler.Config
	config.KeepAwake = !handler.NoKeepAwake
	config.Serial = owonmodel.SerialNumber(strings.TrimSpace(handler.Serial))
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" {
		return &ErrConfiguration{Field: "--model", Reason: "is required"}
	}
	if config.MaximumSubscriptions < 1 || config.MaximumSubscriptions > owonserver.MaximumActiveSubscriptions {
		return &ErrConfiguration{Field: "--max-subscriptions", Reason: fmt.Sprintf("must be in [1,%d]", owonserver.MaximumActiveSubscriptions)}
	}
	if config.DeviceTimeout <= 0 {
		return &ErrConfiguration{Field: "--device-timeout", Reason: "must be positive"}
	}

	endpoint, err := owonrpc.ParseEndpoint(handler.Listen)
	if err != nil {
		return &ErrConfiguration{Field: "--listen", Reason: "invalid endpoint", Cause: err}
	}
	requiresTLS, err := endpoint.RequiresTLS()
	if err != nil {
		return err
	}
	tlsValuesPresent := config.TLSCert != "" || config.TLSKey != "" || config.ClientCA != "" || handler.ClientSANs != "" || handler.ClientFingerprints != ""
	if !requiresTLS && tlsValuesPresent {
		return &ErrConfiguration{Field: "TLS flags", Reason: "are rejected for Unix and loopback endpoints"}
	}
	if requiresTLS && (config.TLSCert == "" || config.TLSKey == "" || config.ClientCA == "") {
		return &ErrConfiguration{Field: "non-loopback TCP", Reason: "requires --tls-cert, --tls-key, and --tls-client-ca"}
	}
	config.ClientSANs = splitList(handler.ClientSANs)
	config.ClientFingerprints = splitList(handler.ClientFingerprints)
	if requiresTLS && len(config.ClientSANs) == 0 && len(config.ClientFingerprints) == 0 {
		return &ErrConfiguration{Field: "non-loopback TCP", Reason: "requires --tls-client-san or --tls-client-sha256"}
	}
	config.Endpoint = endpoint

	return nil
}

// usbConfig maps daemon options to device-selection and operation policy.
//
// Example: one timeout reaches initial identity and later transport exchanges.
func (config options) usbConfig() owonusb.Config {
	return owonusb.Config{Serial: config.Serial, ExpectedModel: config.Model, OperationTimeout: config.DeviceTimeout}
}

// splitList trims and removes empty comma-separated configuration values.
//
// Example: collector.example, ,spiffe://owon/client yields two SAN values.
func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}

	return result
}

// ErrConfiguration identifies an invalid owond command-line option.
//
// Example: an empty expected model returns an ErrConfiguration for --model.
type ErrConfiguration struct {
	Field  string
	Reason string
	Cause  error
}

// Error returns the option-specific configuration diagnostic.
//
// Example: --model: is required explains a rejected daemon invocation.
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

// Unwrap returns an underlying endpoint or TLS parsing failure.
//
// Example: callers can inspect an endpoint failure with errors.As.
func (err *ErrConfiguration) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
