package owoncontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// Measure queries one typed measurement and retains its raw representation.
//
// Example: a channel-one frequency selector issues `:MEASUREMENT:CH1:FREQUENCY?`.
func (controller *Controller) Measure(
	ctx context.Context,
	selector *owonmodel.MeasurementSelector,
) (_result *owonmodel.Measurement, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Measure")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Measure: %v", _err) }()
	}

	session, err := controller.sessionForOperation("measure OWON value")
	if err != nil {
		return nil, err
	}

	return controller.measure(ctx, session, selector)
}

// measure queries one selector through the caller's command admission owner.
//
// Example: State supplies its transaction to keep all snapshot reads adjacent.
func (controller *Controller) measure(
	ctx context.Context,
	executor commandExecutor,
	selector *owonmodel.MeasurementSelector,
) (_result *owonmodel.Measurement, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.measure")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.measure: %v", _err) }()
	}

	query, err := owonscpi.MeasurementQuery(selector)
	if err != nil {
		return nil, err
	}
	response, err := executor.Execute(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query measurement: %w", err)
	}
	return owonscpi.ParseMeasurement(selector, response)
}

// SetMeasurement compiles the complete patch before holding admission for its ordered writes.
//
// Example: an invalid last field cannot leave earlier fields applied.
func (controller *Controller) SetMeasurement(
	ctx context.Context,
	request *owonmodel.MeasurementPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetMeasurement")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetMeasurement: %v", _err) }()
	}

	commands, err := owonscpi.CompileMeasurement(request)
	if err != nil {
		return err
	}
	return controller.executeWrites(ctx, commands)
}
