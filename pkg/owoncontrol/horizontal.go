package owoncontrol

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetHorizontal compiles the complete patch before holding admission for its ordered writes.
//
// Example: an invalid last field cannot leave earlier fields applied.
func (controller *Controller) SetHorizontal(
	ctx context.Context,
	request *owonmodel.HorizontalPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetHorizontal")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetHorizontal: %v", _err) }()
	}

	commands, err := owonscpi.CompileHorizontal(request)
	if err != nil {
		return err
	}
	return controller.executeWrites(ctx, commands)
}
