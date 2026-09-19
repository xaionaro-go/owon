package owoncontrol

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetDMM compiles the complete multimeter patch before admitting any writes.
//
// Example: a function/current mismatch cannot partially change relative mode.
func (controller *Controller) SetDMM(
	ctx context.Context,
	request *owonmodel.DMMPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetDMM")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetDMM: %v", _err) }()
	}

	commands, err := owonscpi.CompileDMM(request)
	if err != nil {
		return err
	}
	return controller.executeWrites(ctx, commands)
}

// DMMMeasurement queries a scalar and timestamps its successfully interpreted observation.
//
// Example: malformed payloads produce no observation or capture timestamp.
func (controller *Controller) DMMMeasurement(ctx context.Context) (_result *owonmodel.DMMMeasurement, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.DMMMeasurement")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.DMMMeasurement: %v", _err) }()
	}

	session, err := controller.sessionForOperation("query DMM measurement")
	if err != nil {
		return nil, err
	}
	response, err := session.Execute(ctx, owonscpi.DMMMeasurementQuery())
	if err != nil {
		return nil, fmt.Errorf("query DMM measurement: %w", err)
	}
	measurement, err := owonscpi.ParseDMMMeasurement(response)
	if err != nil {
		return nil, err
	}
	measurement.CapturedAt = time.Now()
	return measurement, nil
}
