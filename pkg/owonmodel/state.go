package owonmodel

import (
	"time"
)

// StateRequest selects the device data to acquire.
//
// Example: IncludeControls selects observed settings without requesting raw screen-header exposure.
type StateRequest struct {
	Measurements        []*MeasurementSelector
	IncludeScreenHeader bool
	IncludeControls     bool
}

// Validate checks the complete requested selector set before any state acquisition.
//
// Example: duplicate selectors are rejected before identity is queried.
func (request *StateRequest) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "state request is nil"}
	}
	return ValidateMeasurementSelectors(request.Measurements)
}

// StateSnapshot combines identity, selected measurements, and optional observed controls.
//
// Example: a request without controls leaves nested control snapshots nil.
type StateSnapshot struct {
	Device           *DeviceInfo
	Measurements     []*Measurement
	ScreenHeaderJSON []byte
	CapturedAt       time.Time
	Channels         []*ChannelState
	Acquisition      *AcquisitionState
	Horizontal       *HorizontalState
	Trigger          *TriggerState
	Generator        *GeneratorState
	DMM              *DMMState
}
