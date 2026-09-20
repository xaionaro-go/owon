package owoncontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetAcquisition compiles the complete patch before holding admission for its ordered writes.
//
// Example: an invalid last field cannot leave earlier fields applied.
func (controller *Controller) SetAcquisition(
	ctx context.Context,
	request *owonmodel.AcquisitionPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetAcquisition")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetAcquisition: %v", _err) }()
	}

	commands, err := owonscpi.CompileAcquisition(request)
	if err != nil {
		return err
	}
	if request.Mode == nil || *request.Mode != owonmodel.AcquisitionModeAverage {
		return controller.executeWrites(ctx, commands)
	}
	session, err := controller.sessionForOperation("set acquisition")
	if err != nil {
		return err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close()
	if err := controller.executeWritesWith(ctx, transaction, commands); err != nil {
		return err
	}
	response, err := transaction.Execute(ctx, owonscpi.AcquisitionModeQuery())
	if err != nil {
		return fmt.Errorf("verify acquisition mode: %w", err)
	}
	observed, err := owonscpi.ParseAcquisitionMode(response)
	if err != nil {
		return fmt.Errorf("verify acquisition mode: %w", err)
	}
	if observed != *request.Mode {
		return fmt.Errorf("verify acquisition mode: requested %d, observed %d: %w", *request.Mode, observed, &owonscpi.ErrUnsupportedControl{Control: "average mode on this firmware"})
	}
	return nil
}

// Run starts or resumes continuous acquisition.
//
// Example: the method emits exactly one `:RUN` command.
func (controller *Controller) Run(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Run")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Run: %v", _err) }()
	}

	return controller.executeWrites(ctx, []owonprotocol.Command{owonscpi.RunCommand()})
}

// Stop stops acquisition without changing its configuration.
//
// Example: the method emits exactly one `:STOP` command.
func (controller *Controller) Stop(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Stop")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Stop: %v", _err) }()
	}

	return controller.executeWrites(ctx, []owonprotocol.Command{owonscpi.StopCommand()})
}

// Single arms one acquisition using the current trigger configuration.
//
// Example: the method emits exactly one `:SINGLE` command.
func (controller *Controller) Single(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Single")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Single: %v", _err) }()
	}

	return controller.executeWrites(ctx, []owonprotocol.Command{owonscpi.SingleCommand()})
}

// Auto sends the source-backed autoset candidate without requesting readback.
//
// Example: completion means only that the no-response command reached the
// transport; physical firmware acceptance and resulting settings are unknown.
func (controller *Controller) Auto(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Auto")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Auto: %v", _err) }()
	}

	return controller.executeWrites(ctx, []owonprotocol.Command{owonscpi.AutoCommand()})
}
