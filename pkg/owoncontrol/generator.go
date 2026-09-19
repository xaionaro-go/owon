package owoncontrol

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetGenerator compiles the complete patch before holding admission for its ordered writes.
//
// Example: an invalid last field cannot leave earlier fields applied.
func (controller *Controller) SetGenerator(
	ctx context.Context,
	request *owonmodel.GeneratorPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetGenerator")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetGenerator: %v", _err) }()
	}

	commands, err := owonscpi.CompileGenerator(request)
	if err != nil {
		return err
	}
	return controller.executeWrites(ctx, commands)
}
