package owonmodel

import (
	"fmt"
	"math"
	"strings"
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
	// GeneratorWaveformAmpALT selects the vendor's AmpALT builtin.
	//
	// Example: select AmpALT without implying a formula from its name.
	GeneratorWaveformAmpALT
	// GeneratorWaveformAttALT selects the vendor's AttALT builtin.
	//
	// Example: select AttALT independently of AmpALT.
	GeneratorWaveformAttALT
	// GeneratorWaveformStairDown selects the StairDn builtin.
	//
	// Example: select the vendor's descending staircase waveform.
	GeneratorWaveformStairDown
	// GeneratorWaveformStairUpDown selects the StairUD builtin.
	//
	// Example: select the vendor's up/down staircase waveform.
	GeneratorWaveformStairUpDown
	// GeneratorWaveformStairUp selects the StairUp builtin.
	//
	// Example: select the vendor's ascending staircase waveform.
	GeneratorWaveformStairUp
	// GeneratorWaveformBesselJ selects the Besselj builtin.
	//
	// Example: preserve the vendor's named J variant.
	GeneratorWaveformBesselJ
	// GeneratorWaveformBesselY selects the Bessely builtin.
	//
	// Example: preserve the vendor's named Y variant.
	GeneratorWaveformBesselY
	// GeneratorWaveformSinc selects the vendor's Sinc builtin.
	//
	// Example: select Sinc without implicitly enabling output.
	GeneratorWaveformSinc
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
	AmplitudeVolts    *float64 // Volts peak-to-peak (Vpp).
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

// Validate checks generator membership, field relationships, and physical numeric representation.
// Waveform-dependent fields require an explicit waveform; frequency and period are exclusive.
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
		if err := validateEnum("generator waveform", *request.Waveform,
			GeneratorWaveformSine, GeneratorWaveformSquare, GeneratorWaveformRamp, GeneratorWaveformPulse,
			GeneratorWaveformAmpALT, GeneratorWaveformAttALT, GeneratorWaveformStairDown, GeneratorWaveformStairUpDown,
			GeneratorWaveformStairUp, GeneratorWaveformBesselJ, GeneratorWaveformBesselY, GeneratorWaveformSinc,
		); err != nil {
			return err
		}
	}
	if request.Load != nil {
		if err := validateEnum("generator load", *request.Load, GeneratorLoadOn, GeneratorLoadOff); err != nil {
			return err
		}
	}
	if request.FrequencyHz != nil && request.PeriodSeconds != nil {
		return &ErrInvalidRequest{Reason: "set frequency or period, not both"}
	}
	if err := request.validateWaveformCompanion(); err != nil {
		return err
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

// validateWaveformCompanion requires a waveform for fields whose meaning depends on it.
//
// Example: a period and duty request without a waveform reports both dependent fields.
func (request *GeneratorPatch) validateWaveformCompanion() error {
	if request.Waveform != nil {
		return nil
	}
	var dependentFields []string
	for _, field := range []struct {
		Name    string
		Present bool
	}{
		{"frequency", request.FrequencyHz != nil}, {"period", request.PeriodSeconds != nil},
		{"symmetry", request.SymmetryPercent != nil}, {"duty", request.DutyPercent != nil},
		{"pulse width", request.PulseWidthSeconds != nil}, {"rising", request.RisingSeconds != nil}, {"falling", request.FallingSeconds != nil},
	} {
		if !field.Present {
			continue
		}
		dependentFields = append(dependentFields, field.Name)
	}
	if len(dependentFields) != 0 {
		return &ErrInvalidRequest{Reason: "waveform is required when setting " + strings.Join(dependentFields, ", ")}
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
	AmplitudeVolts    float64 // Volts peak-to-peak (Vpp).
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
