package owonscpi

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// TestGeneratorWaveformTokens preserves exact vendor builtin spelling and omission semantics.
//
// Example: Besselj selects one waveform without resetting frequency or enabling output.
func TestGeneratorWaveformTokens(t *testing.T) {
	for index, token := range []string{"SINE", "SQUARE", "RAMP", "PULSE", "AmpALT", "AttALT", "StairDn", "StairUD", "StairUp", "Besselj", "Bessely", "Sinc"} {
		waveform := owonmodel.GeneratorWaveform(index + 1)
		commands, err := CompileGenerator(&owonmodel.GeneratorPatch{Waveform: &waveform})
		require.NoError(t, err, token)
		require.Len(t, commands, 1)
		require.Equal(t, ":FUNCTION "+token, commands[0].Text)
	}
}

// TestGeneratorFrequencyPeriodBounds checks equivalent inclusive ranges and the adjacent rejected floats.
//
// Example: every builtin accepts 200 ns through 10 s, while sine additionally accepts 40 ns.
func TestGeneratorFrequencyPeriodBounds(t *testing.T) {
	for index, maximum := range []float64{25e6, 5e6, 1e6, 5e6, 5e6, 5e6, 5e6, 5e6, 5e6, 5e6, 5e6, 5e6} {
		waveform := owonmodel.GeneratorWaveform(index + 1)
		requireGeneratorTimebaseBounds(t, waveform, "FREQUENCY", 0.1, maximum)
		requireGeneratorTimebaseBounds(t, waveform, "PERIOD", 1/maximum, 10)
	}
}

// requireGeneratorTimebaseBounds checks endpoints, adjacent floats, and extreme finite values.
//
// Example: the same cases test frequency in Hz and period in seconds without reciprocal rounding.
func requireGeneratorTimebaseBounds(
	t *testing.T,
	waveform owonmodel.GeneratorWaveform,
	name string,
	minimum float64,
	maximum float64,
) {
	t.Helper()
	for _, testCase := range []struct {
		Value float64
		Valid bool
	}{
		{minimum, true},
		{math.Nextafter(minimum, math.Inf(1)), true},
		{math.Nextafter(maximum, math.Inf(-1)), true},
		{maximum, true},
		{math.Nextafter(minimum, math.Inf(-1)), false},
		{math.Nextafter(maximum, math.Inf(1)), false},
		{math.SmallestNonzeroFloat64, false},
		{math.MaxFloat64, false},
	} {
		patch := &owonmodel.GeneratorPatch{Waveform: &waveform}
		if name == "FREQUENCY" {
			patch.FrequencyHz = &testCase.Value
		} else {
			patch.PeriodSeconds = &testCase.Value
		}
		commands, err := CompileGenerator(patch)
		if !testCase.Valid {
			var invalid *ErrInvalidSetting
			require.ErrorAs(t, err, &invalid, "%d %s %g", waveform, name, testCase.Value)
			require.Nil(t, commands)
			continue
		}
		require.NoError(t, err, "%d %s %g", waveform, name, testCase.Value)
		require.Len(t, commands, 2)
		require.True(t, strings.HasPrefix(commands[1].Text, ":FUNCTION:"+name+" "))
	}
}

// TestGeneratorBuiltinsRejectUnprovedControls prevents implicit support for builtin shape modifiers.
//
// Example: builtin duty errors are unsupported, while absent waveform is an invalid request.
func TestGeneratorBuiltinsRejectUnprovedControls(t *testing.T) {
	value, symmetry := 0.5, int32(50)
	for waveform := owonmodel.GeneratorWaveform(5); waveform <= 12; waveform++ {
		for _, test := range []struct {
			Name  string
			Patch owonmodel.GeneratorPatch
		}{
			{"symmetry", owonmodel.GeneratorPatch{SymmetryPercent: &symmetry}},
			{"duty", owonmodel.GeneratorPatch{DutyPercent: &value}},
			{"pulse width", owonmodel.GeneratorPatch{PulseWidthSeconds: &value}},
			{"rising", owonmodel.GeneratorPatch{RisingSeconds: &value}},
			{"falling", owonmodel.GeneratorPatch{FallingSeconds: &value}},
		} {
			commands, err := CompileGenerator(&test.Patch)
			var invalid *owonmodel.ErrInvalidRequest
			require.ErrorAs(t, err, &invalid)
			require.Nil(t, commands)
			test.Patch.Waveform = &waveform
			commands, err = CompileGenerator(&test.Patch)
			var unsupported *ErrUnsupportedControl
			require.ErrorAs(t, err, &unsupported)
			require.Contains(t, unsupported.Control, test.Name)
			require.Nil(t, commands)
		}
	}
}
