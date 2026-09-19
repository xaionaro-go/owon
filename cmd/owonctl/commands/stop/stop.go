// Package stop implements the owonctl stop command.
//
// Example: `owonctl stop` stops acquisition and prints the result as JSON.
package stop

import (
	"context"
	"errors"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/owon/pkg/owonctl"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonclient"
)

// commandHandler holds command inputs and shared connection policy.
//
// Example: Cobra populates command values before execution.
type commandHandler struct {
	Config *owonctl.ClientConfig
}

// NewCommand constructs this independently registered Cobra command.
//
// Example: the executable adds the command to its root.
func NewCommand(config *owonctl.ClientConfig) *cobra.Command {
	handler := &commandHandler{Config: config}
	command := &cobra.Command{Use: "stop", Short: "Stop acquisition", Args: cobra.NoArgs, RunE: handler.run}

	return command
}

// run validates inputs, owns the command connection, and writes the result.
//
// Example: invalid requests fail before a connection is constructed.
func (handler *commandHandler) run(
	command *cobra.Command,
	arguments []string,
) (_err error) {
	config, err := handler.Config.ConnectionConfig()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(command.Context(), handler.Config.Timeout)
	defer cancel()
	ctx = belt.WithField(ctx, "command", "stop")
	logger.Tracef(ctx, "stop")
	// traceResult reports completion after command-owned cleanup.
	//
	// Example: operation and cleanup errors remain visible at trace level.
	defer func() { logger.Tracef(ctx, "/stop: %v", _err) }()

	connection, err := owonclient.NewConnection(ctx, config)
	if err != nil {
		return err
	}

	// closeConnection retains independent cleanup failures.
	//
	// Example: an RPC failure cannot hide a connection close failure.
	defer func() { _err = errors.Join(_err, connection.Close()) }()

	logger.Debugf(ctx, "executing stop")
	response, err := connection.Client().Stop(ctx)
	if err != nil {
		return err
	}

	return owonctl.PrintMessage(command.OutOrStdout(), response)
}
