package owonmodel

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// validatable is the intrinsic-value contract under test, not a dialect compiler.
//
// Example: typed nil patch pointers still report invalid input through this method.
type validatable interface {
	// Validate checks intrinsic values without device-specific support policy.
	//
	// Example: a domain-valid external trigger can subsequently be unsupported by the dialect.
	Validate() error
}

// TestDomainValidityExcludesDialectPolicy proves intrinsic values do not inherit HDS limitations.
//
// Example: a 3X probe and an external trigger are valid model values despite unsupported HDS writes.
func TestDomainValidityExcludesDialectPolicy(t *testing.T) {
	probe, scale, frequency := 3.0, "3us", 1e9
	average, external, ground := AcquisitionModeAverage, TriggerSourceExternal, CouplingGround
	manual, symmetry, replacement := false, int32(101), true
	for _, request := range []validatable{
		&ChannelPatch{Channel: Channel1, ProbeAttenuation: &probe},
		&AcquisitionPatch{Mode: &average},
		&HorizontalPatch{Scale: &scale},
		&TriggerPatch{Source: &external, Coupling: &ground},
		&GeneratorPatch{FrequencyHz: &frequency, SymmetryPercent: &symmetry},
		&DMMPatch{AutoRange: &manual},
		&MeasurementPatch{ReplaceVisible: &replacement},
		&WaveformRequest{Channel: Channel2},
		&StateRequest{},
	} {
		require.NoError(t, request.Validate())
	}
}

// TestDomainRejectsAbsentUnknownAndNonfiniteValues exercises rejection without compilation or connection setup.
//
// Example: every optional enum distinguishes an absent pointer from an explicitly unknown value.
func TestDomainRejectsAbsentUnknownAndNonfiniteValues(t *testing.T) {
	mode, depth, source, coupling, slope, sweep := AcquisitionMode(99), AcquisitionMemoryDepth(99), TriggerSource(99), Coupling(99), TriggerSlope(99), TriggerSweep(99)
	function, current, dmmRange, waveform, load := DMMFunction(99), DMMCurrentType(99), DMMRange(99), GeneratorWaveform(99), GeneratorLoad(99)
	nan, infinity, zero := math.NaN(), math.Inf(1), 0.0
	requests := []validatable{
		(*ChannelPatch)(nil), (*AcquisitionPatch)(nil), (*HorizontalPatch)(nil), (*TriggerPatch)(nil), (*MeasurementPatch)(nil), (*GeneratorPatch)(nil), (*DMMPatch)(nil), (*StateRequest)(nil), (*WaveformRequest)(nil), (*MeasurementSelector)(nil),
		&ChannelPatch{}, &AcquisitionPatch{}, &HorizontalPatch{}, &TriggerPatch{}, &MeasurementPatch{}, &GeneratorPatch{}, &DMMPatch{},
		&ChannelPatch{Channel: Channel1, Coupling: &coupling}, &ChannelPatch{Channel: Channel2, ProbeAttenuation: &zero},
		&AcquisitionPatch{Mode: &mode}, &AcquisitionPatch{MemoryDepth: &depth},
		&TriggerPatch{Source: &source}, &TriggerPatch{Coupling: &coupling}, &TriggerPatch{Slope: &slope}, &TriggerPatch{Sweep: &sweep}, &TriggerPatch{LevelVolts: &nan},
		&DMMPatch{Function: &function}, &DMMPatch{CurrentType: &current}, &DMMPatch{Range: &dmmRange},
		&GeneratorPatch{Waveform: &waveform}, &GeneratorPatch{Load: &load}, &GeneratorPatch{PeriodSeconds: &zero}, &GeneratorPatch{AmplitudeVolts: &infinity},
		&WaveformRequest{Channel: Channel(99)}, &MeasurementSelector{Channel: Channel1, Kind: MeasurementKindUnknown},
	}
	for _, request := range requests {
		var invalid *ErrInvalidRequest
		require.ErrorAs(t, request.Validate(), &invalid, "%T", request)
	}
}
