package owoncontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// State captures identity, requested measurements, and optional screen metadata.
//
// Example: subscribers request only the measurements they publish to downstream systems.
func (controller *Controller) State(
	ctx context.Context,
	request *owonmodel.StateRequest,
) (_result *owonmodel.StateSnapshot, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.State")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.State: %v", _err) }()
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}
	session, err := controller.sessionForOperation("get OWON state")
	if err != nil {
		return nil, err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer transaction.Close()
	info, err := controller.deviceInfo(ctx, transaction)
	if err != nil {
		return nil, err
	}
	state := &owonmodel.StateSnapshot{Device: info, CapturedAt: time.Now()}
	for _, selector := range request.Measurements {
		measurement, measurementErr := controller.measure(ctx, transaction, selector)
		if measurementErr != nil {
			return nil, fmt.Errorf("capture requested measurement: %w", measurementErr)
		}
		state.Measurements = append(state.Measurements, measurement)
	}
	if !request.IncludeScreenHeader && !request.IncludeControls {
		return state, nil
	}
	header, err := transaction.Execute(ctx, owonscpi.ScreenHeaderQuery())
	if err != nil {
		return nil, fmt.Errorf("capture screen header: %w", err)
	}
	if request.IncludeScreenHeader {
		state.ScreenHeaderJSON = header
	}
	if !request.IncludeControls {
		return state, nil
	}
	parsed, err := owonscpi.ParseScreenHeader(header)
	if err != nil {
		return nil, fmt.Errorf("decode screen controls: %w", err)
	}
	controls, err := parsed.Controls()
	if err != nil {
		return nil, fmt.Errorf("decode screen controls: %w", err)
	}
	state.Channels = controls.Channels
	state.Acquisition = controls.Acquisition
	state.Horizontal = controls.Horizontal
	state.Trigger = controls.Trigger
	state.DMM = &owonmodel.DMMState{}
	rangeResponse, rangeErr := transaction.Execute(ctx, owonscpi.DMMRangeQuery())
	if rangeErr != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("capture DMM range: %w", rangeErr)
		}
		return state, nil
	}
	rangeValue, err := owonscpi.ParseDMMRange(rangeResponse)
	if err != nil {
		var malformed *owonscpi.ErrMalformedResponse
		if !errors.As(err, &malformed) {
			return nil, fmt.Errorf("decode DMM range: %w", err)
		}
		rangeValue = owonmodel.DMMRangeUnspecified
	}
	state.DMM.Range = rangeValue
	state.DMM.ObservedRangeToken = strings.TrimSpace(string(rangeResponse))

	return state, nil
}
