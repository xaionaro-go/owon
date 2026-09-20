package owoncontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// EnsureKeepAwake refreshes the instrument's shutdown timer to Unlimited.
//
// Example: startup validates the timer, sends one setter, and verifies it with
// a fresh readback.
func (controller *Controller) EnsureKeepAwake(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.EnsureKeepAwake")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed policy operation retains its contextual diagnostic.
		func() { logger.Tracef(ctx, "/Controller.EnsureKeepAwake: %v", _err) }()
	}

	session, err := controller.sessionForOperation("ensure device awake")
	if err != nil {
		return err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close()

	operationContext, cancel, err := transaction.OperationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	response, err := transaction.Execute(operationContext, owonscpi.ShutdownTimerQuery())
	if err != nil {
		return fmt.Errorf("query shutdown timer: %w", err)
	}
	if _, err := owonscpi.ParseShutdownTimer(response); err != nil {
		return fmt.Errorf("query shutdown timer: %w", err)
	}

	if _, err := transaction.Execute(operationContext, owonscpi.ShutdownTimerUnlimitedCommand()); err != nil {
		return fmt.Errorf("set shutdown timer Unlimited: %w", err)
	}
	response, err = transaction.Execute(operationContext, owonscpi.ShutdownTimerQuery())
	if err != nil {
		return fmt.Errorf("verify shutdown timer Unlimited: %w", err)
	}
	timer, err := owonscpi.ParseShutdownTimer(response)
	if err != nil {
		return fmt.Errorf("verify shutdown timer Unlimited: %w", err)
	}
	if timer != owonscpi.ShutdownTimerUnlimited {
		return fmt.Errorf("verify shutdown timer Unlimited: got %q", timer)
	}

	return nil
}
