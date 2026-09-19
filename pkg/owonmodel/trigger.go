package owonmodel

import (
	"fmt"
)

const (
	// TriggerSourceUnspecified identifies the unspecified trigger source value.
	//
	// Example: compare a TriggerSource value with TriggerSourceUnspecified.
	TriggerSourceUnspecified TriggerSource = iota
	// TriggerSourceChannel1 identifies the channel one trigger source value.
	//
	// Example: compare a TriggerSource value with TriggerSourceChannel1.
	TriggerSourceChannel1
	// TriggerSourceChannel2 identifies the channel two trigger source value.
	//
	// Example: compare a TriggerSource value with TriggerSourceChannel2.
	TriggerSourceChannel2
	// TriggerSourceExternal identifies the external trigger source value.
	//
	// Example: compare a TriggerSource value with TriggerSourceExternal.
	TriggerSourceExternal
)

const (
	// TriggerSlopeUnspecified identifies the unspecified trigger slope value.
	//
	// Example: compare a TriggerSlope value with TriggerSlopeUnspecified.
	TriggerSlopeUnspecified TriggerSlope = iota
	// TriggerSlopeRising identifies the rising trigger slope value.
	//
	// Example: compare a TriggerSlope value with TriggerSlopeRising.
	TriggerSlopeRising
	// TriggerSlopeFalling identifies the falling trigger slope value.
	//
	// Example: compare a TriggerSlope value with TriggerSlopeFalling.
	TriggerSlopeFalling
)

const (
	// TriggerSweepUnspecified identifies the unspecified trigger sweep value.
	//
	// Example: compare a TriggerSweep value with TriggerSweepUnspecified.
	TriggerSweepUnspecified TriggerSweep = iota
	// TriggerSweepAuto identifies the auto trigger sweep value.
	//
	// Example: compare a TriggerSweep value with TriggerSweepAuto.
	TriggerSweepAuto
	// TriggerSweepNormal identifies the normal trigger sweep value.
	//
	// Example: compare a TriggerSweep value with TriggerSweepNormal.
	TriggerSweepNormal
	// TriggerSweepSingle identifies the single trigger sweep value.
	//
	// Example: compare a TriggerSweep value with TriggerSweepSingle.
	TriggerSweepSingle
)

// TriggerSource identifies a device trigger source value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: TriggerSourceChannel1 selects the corresponding documented setting.
type TriggerSource int32

// TriggerSlope identifies a device trigger slope value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: TriggerSlopeRising selects the corresponding documented setting.
type TriggerSlope int32

// TriggerSweep identifies a device trigger sweep value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: TriggerSweepAuto selects the corresponding documented setting.
type TriggerSweep int32

// TriggerPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: a present zero LevelVolts value sets a zero-volt trigger threshold without changing its source.
type TriggerPatch struct {
	Source     *TriggerSource
	Coupling   *Coupling
	Slope      *TriggerSlope
	LevelVolts *float64
	Sweep      *TriggerSweep
}

// Validate checks all trigger enums and finite voltage before dialect support is considered.
//
// Example: an unknown slope remains invalid when an external source is also requested.
func (request *TriggerPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "trigger patch is nil"}
	}
	if request.Source == nil && request.Coupling == nil && request.Slope == nil && request.LevelVolts == nil && request.Sweep == nil {
		return &ErrInvalidRequest{Reason: "trigger patch is empty"}
	}
	if request.Source != nil {
		if err := validateEnum("trigger source", *request.Source, TriggerSourceChannel1, TriggerSourceChannel2, TriggerSourceExternal); err != nil {
			return err
		}
	}
	if request.Coupling != nil {
		if err := validateEnum("trigger coupling", *request.Coupling, CouplingAC, CouplingDC, CouplingGround); err != nil {
			return err
		}
	}
	if request.Slope != nil {
		if err := validateEnum("trigger slope", *request.Slope, TriggerSlopeRising, TriggerSlopeFalling); err != nil {
			return err
		}
	}
	if request.LevelVolts != nil {
		if err := requireFinite("trigger level", *request.LevelVolts); err != nil {
			return err
		}
	}
	if request.Sweep != nil {
		return validateEnum("trigger sweep", *request.Sweep, TriggerSweepAuto, TriggerSweepNormal, TriggerSweepSingle)
	}
	return nil
}

// TriggerState contains trigger configuration and acquisition status observed in a screen header.
//
// Example: a rising CH1 edge is independent of whether acquisition is stopped.
type TriggerState struct {
	Source   TriggerSource
	Coupling Coupling
	Slope    TriggerSlope
	Level    float64
	Sweep    TriggerSweep
	Status   string
}

// validateEnum requires a member of the explicitly named domain set.
//
// Example: an unknown wire integer is rejected instead of becoming an unspecified setting.
func validateEnum[Value comparable](
	name string,
	value Value,
	allowed ...Value,
) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return &ErrInvalidRequest{Reason: fmt.Sprintf("unknown %s %v", name, value)}
}
