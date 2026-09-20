package owonmodel

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratorWaveformMembership admits the twelve named variants and rejects unknown wire values.
//
// Example: builtin values five through twelve remain distinct semantic selections.
func TestGeneratorWaveformMembership(t *testing.T) {
	for waveform := GeneratorWaveform(1); waveform <= 12; waveform++ {
		require.NoError(t, (&GeneratorPatch{Waveform: &waveform}).Validate(), waveform)
	}
	for _, waveform := range []GeneratorWaveform{-1, 0, 13, 999} {
		var invalid *ErrInvalidRequest
		require.ErrorAs(t, (&GeneratorPatch{Waveform: &waveform}).Validate(), &invalid)
	}
}

// TestGeneratorDependentFieldsRequireWaveform makes the companion requirement intrinsic to the request.
//
// Example: a period-only request cannot bypass waveform-specific compiler limits.
func TestGeneratorDependentFieldsRequireWaveform(t *testing.T) {
	value, symmetry, waveform := 0.5, int32(50), GeneratorWaveformSine
	for _, test := range []struct {
		Name  string
		Patch GeneratorPatch
	}{
		{"frequency", GeneratorPatch{FrequencyHz: &value}},
		{"period", GeneratorPatch{PeriodSeconds: &value}},
		{"symmetry", GeneratorPatch{SymmetryPercent: &symmetry}},
		{"duty", GeneratorPatch{DutyPercent: &value}},
		{"pulse width", GeneratorPatch{PulseWidthSeconds: &value}},
		{"rising", GeneratorPatch{RisingSeconds: &value}},
		{"falling", GeneratorPatch{FallingSeconds: &value}},
	} {
		var invalid *ErrInvalidRequest
		require.ErrorAs(t, test.Patch.Validate(), &invalid, test.Name)
		require.Contains(t, invalid.Reason, test.Name)
		test.Patch.Waveform = &waveform
		require.NoError(t, test.Patch.Validate())
	}
	patch := &GeneratorPatch{PeriodSeconds: &value, DutyPercent: &value}
	require.ErrorContains(t, patch.Validate(), "period, duty")
	load, output := GeneratorLoadOff, false
	for _, patch := range []*GeneratorPatch{
		{AmplitudeVolts: &value}, {OffsetVolts: &value}, {HighVolts: &value}, {LowVolts: &value}, {Load: &load}, {Output: &output},
	} {
		require.NoError(t, patch.Validate())
	}
}

// TestGeneratorFrequencyAndPeriodAreExclusive rejects both timebase fields even when reciprocal.
//
// Example: 2 Hz with 0.5 seconds is still an ambiguous two-write request.
func TestGeneratorFrequencyAndPeriodAreExclusive(t *testing.T) {
	waveform, frequency := GeneratorWaveformSine, 2.0
	for _, period := range []float64{0.5, 1} {
		err := (&GeneratorPatch{Waveform: &waveform, FrequencyHz: &frequency, PeriodSeconds: &period}).Validate()
		var invalid *ErrInvalidRequest
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, "set frequency or period, not both", invalid.Reason)
	}
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, (&GeneratorPatch{Waveform: &waveform, PeriodSeconds: &value}).Validate())
		require.Error(t, (&GeneratorPatch{Waveform: &waveform, FrequencyHz: &value}).Validate())
	}
}
