package owonmodel

import (
	"fmt"
	"math"
)

const (
	// GeneratorWaveformUnspecified identifies the unspecified generator waveform value.
	//
	// Example: compare a GeneratorWaveform value with GeneratorWaveformUnspecified.
	GeneratorWaveformUnspecified GeneratorWaveform = iota
	// GeneratorWaveformSine identifies the sine generator waveform value.
	//
	// Example: compare a GeneratorWaveform value with GeneratorWaveformSine.
	GeneratorWaveformSine
	// GeneratorWaveformSquare identifies the square generator waveform value.
	//
	// Example: compare a GeneratorWaveform value with GeneratorWaveformSquare.
	GeneratorWaveformSquare
	// GeneratorWaveformRamp identifies the ramp generator waveform value.
	//
	// Example: compare a GeneratorWaveform value with GeneratorWaveformRamp.
	GeneratorWaveformRamp
	// GeneratorWaveformPulse identifies the pulse generator waveform value.
	//
	// Example: compare a GeneratorWaveform value with GeneratorWaveformPulse.
	GeneratorWaveformPulse
)

const (
	// GeneratorLoadUnspecified identifies the unspecified generator load value.
	//
	// Example: compare a GeneratorLoad value with GeneratorLoadUnspecified.
	GeneratorLoadUnspecified GeneratorLoad = iota
	// GeneratorLoadOn identifies the on generator load value.
	//
	// Example: compare a GeneratorLoad value with GeneratorLoadOn.
	GeneratorLoadOn
	// GeneratorLoadOff identifies the off generator load value.
	//
	// Example: compare a GeneratorLoad value with GeneratorLoadOff.
	GeneratorLoadOff
)

// GeneratorWaveform identifies a device generator waveform value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: GeneratorWaveformSine selects the corresponding documented setting.
type GeneratorWaveform int32

// GeneratorLoad identifies a device generator load value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: GeneratorLoadOn selects the corresponding documented setting.
type GeneratorLoad int32

// GeneratorPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: setting Waveform to GeneratorWaveformSine with FrequencyHz configures a sine output frequency.
type GeneratorPatch struct {
	Waveform          *GeneratorWaveform
	FrequencyHz       *float64
	PeriodSeconds     *float64
	AmplitudeVolts    *float64
	OffsetVolts       *float64
	HighVolts         *float64
	LowVolts          *float64
	SymmetryPercent   *int32
	PulseWidthSeconds *float64
	RisingSeconds     *float64
	FallingSeconds    *float64
	DutyPercent       *float64
	Load              *GeneratorLoad
	Output            *bool
}

// Validate checks generator membership and physical numeric representation, not device limits.
//
// Example: a finite positive frequency is valid independently of a model's maximum output frequency.
func (request *GeneratorPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "generator patch is nil"}
	}
	if request.Waveform == nil && request.FrequencyHz == nil && request.PeriodSeconds == nil && request.AmplitudeVolts == nil && request.OffsetVolts == nil && request.HighVolts == nil && request.LowVolts == nil && request.SymmetryPercent == nil && request.PulseWidthSeconds == nil && request.RisingSeconds == nil && request.FallingSeconds == nil && request.DutyPercent == nil && request.Load == nil && request.Output == nil {
		return &ErrInvalidRequest{Reason: "generator patch is empty"}
	}
	if request.Waveform != nil {
		if err := validateEnum("generator waveform", *request.Waveform, GeneratorWaveformSine, GeneratorWaveformSquare, GeneratorWaveformRamp, GeneratorWaveformPulse); err != nil {
			return err
		}
	}
	if request.Load != nil {
		if err := validateEnum("generator load", *request.Load, GeneratorLoadOn, GeneratorLoadOff); err != nil {
			return err
		}
	}
	for _, value := range []*float64{request.FrequencyHz, request.PeriodSeconds, request.PulseWidthSeconds, request.RisingSeconds, request.FallingSeconds} {
		if value == nil {
			continue
		}
		if err := requireFinitePositive("generator timing", *value); err != nil {
			return err
		}
	}
	for _, value := range []*float64{request.AmplitudeVolts, request.OffsetVolts, request.HighVolts, request.LowVolts, request.DutyPercent} {
		if value == nil {
			continue
		}
		if err := requireFinite("generator value", *value); err != nil {
			return err
		}
	}
	return nil
}

// GeneratorState contains observed signal-generator settings in explicit physical units.
//
// Example: frequency in Hz and period in seconds occupy independent fields.
type GeneratorState struct {
	Waveform          GeneratorWaveform
	FrequencyHz       float64
	PeriodSeconds     float64
	AmplitudeVolts    float64
	OffsetVolts       float64
	HighVolts         float64
	LowVolts          float64
	SymmetryPercent   float64
	PulseWidthSeconds float64
	RisingSeconds     float64
	FallingSeconds    float64
	DutyPercent       float64
	Load              GeneratorLoad
	Output            bool
}

// requireFinite rejects values that cannot represent a finite physical quantity.
//
// Example: NaN cannot be sent as a trigger level.
func requireFinite(
	name string,
	value float64,
) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be finite: %w", name, &ErrInvalidRequest{Reason: "value is not finite"})
	}

	return nil
}

// requireFinitePositive rejects non-finite and non-positive numeric settings.
//
// Example: a probe attenuation of zero is rejected.
func requireFinitePositive(
	name string,
	value float64,
) error {
	if err := requireFinite(name, value); err != nil {
		return err
	}
	if value <= 0 {
		return fmt.Errorf("%s must be positive: %w", name, &ErrInvalidRequest{Reason: "value is not positive"})
	}

	return nil
}
