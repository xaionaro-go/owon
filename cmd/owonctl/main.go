// Command owonctl is the operational client for an owond service.
//
// Example: owonctl info prints instrument identity as JSON.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/dmm"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/execute"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/info"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/run"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/single"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/state"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/stop"
	"github.com/xaionaro-go/owon/cmd/owonctl/commands/waveform"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonlog"
)

// commandLogging describes the process logging configuration.
//
// Example: the persistent hook installs the requested log level for each command.
type commandLogging struct {
	Config owonlog.Config
	Output io.Writer
}

// configure installs a contextual logger after flags and before command execution.
//
// Example: help exits before this hook, without opening connections.
func (logging *commandLogging) configure(
	command *cobra.Command,
	_ []string,
) error {
	ctx, err := owonlog.WithContext(command.Context(), logging.Config, logging.Output)
	if err != nil {
		return err
	}
	command.SetContext(ctx)
	return nil
}

// showHelp renders Cobra's generated catalog for an empty invocation.
//
// Example: owonctl without arguments succeeds without contacting the daemon.
func showHelp(
	command *cobra.Command,
	_ []string,
) error {
	return command.Help()
}

// flagError retains a typed argument classification around Cobra's parser error.
//
// Example: invalid flag values remain distinguishable from transport failures.
func flagError(
	command *cobra.Command,
	err error,
) error {
	return &owonctl.ErrFlagParse{Command: command.CommandPath(), Cause: err}
}

// newCommand assembles the maintained parser and independent subcommand packages.
//
// Example: persistent connection flags work on either side of the command name.
func newCommand(
	output io.Writer,
	diagnostics io.Writer,
) *cobra.Command {
	config := new(owonctl.ClientConfig)
	logging := &commandLogging{Config: owonlog.Config{Service: "owonctl", Level: logger.LevelInfo}, Output: diagnostics}
	command := &cobra.Command{Use: owonctl.DefaultProgram, Short: "Control an OWON instrument through owond", Args: cobra.NoArgs, RunE: showHelp, PersistentPreRunE: logging.configure, SilenceUsage: true, SilenceErrors: true}
	command.SetOut(output)
	command.SetErr(diagnostics)
	command.SetFlagErrorFunc(flagError)
	command.CompletionOptions.DisableDefaultCmd = true
	config.RegisterFlags(command.PersistentFlags())
	command.PersistentFlags().Var(&logging.Config.Level, "log-level", "logging level: trace, debug, info, warning, error, fatal")
	command.AddCommand(info.NewCommand(config), state.NewCommand(config), execute.NewCommand(config), run.NewCommand(config), stop.NewCommand(config), single.NewCommand(config), dmm.NewCommand(config), waveform.NewCommand(config))
	return command
}

// executeCommand owns output error recording around Cobra's execution boundary.
//
// Example: a failed generated help write returns its exact cause once.
func executeCommand(
	ctx context.Context,
	arguments []string,
	output io.Writer,
	diagnostics io.Writer,
) error {
	if ctx == nil {
		return &owonctl.ErrConfiguration{Field: "context", Reason: "must not be nil"}
	}
	recorded, err := owonctl.NewDiagnosticWriter(output)
	if err != nil {
		return err
	}
	command := newCommand(recorded, diagnostics)
	command.SetArgs(arguments)
	return recorded.Join(command.ExecuteContext(ctx))
}

// main owns process streams, signals, and nonzero error exits.
//
// Example: JSON goes only to stdout, and trace diagnostics go only to stderr.
func main() {
	ctx, err := owonlog.WithContext(context.Background(), owonlog.Config{Service: "owonctl", Level: logger.LevelInfo}, os.Stderr)
	if err != nil {
		panic(err)
	}
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := executeCommand(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		logger.Fatalf(ctx, "%v", err)
	}
}
