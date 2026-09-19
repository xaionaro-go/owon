package owoncontrol

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetTrigger compiles the complete patch before holding admission for its ordered writes.
//
// Example: an invalid last field cannot leave earlier fields applied.
func (controller *Controller) SetTrigger(
	ctx context.Context,
	request *owonmodel.TriggerPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetTrigger")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetTrigger: %v", _err) }()
	}

	commands, err := owonscpi.CompileTrigger(request)
	if err != nil {
		return err
	}
	return controller.executeWrites(ctx, commands)
}
