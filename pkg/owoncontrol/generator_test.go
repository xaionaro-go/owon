package owoncontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// TestGeneratorRejectsWholePatchBeforeIO ensures invalid dependent or last fields cannot partially write.
//
// Example: invalid load or builtin duty prevents waveform selection and explicit output enable.
func TestGeneratorRejectsWholePatchBeforeIO(t *testing.T) {
	waveform, value, invalidLoad, output := owonmodel.GeneratorWaveformSinc, 1.0, owonmodel.GeneratorLoad(999), true
	for _, patch := range []*owonmodel.GeneratorPatch{
		{Waveform: &waveform, FrequencyHz: &value, Load: &invalidLoad, Output: &output},
		{Waveform: &waveform, FrequencyHz: &value, DutyPercent: &value, Output: &output},
		{Waveform: &waveform, FrequencyHz: &value, PeriodSeconds: &value, Output: &output},
		{PeriodSeconds: &value, Output: &output},
	} {
		backend := &scriptedBackend{}
		err := newTestInstrument(t, backend).SetGenerator(t.Context(), patch)
		require.Error(t, err)
		require.Empty(t, backend.Commands)
	}
}

// TestGeneratorOutputPresenceControlsOnlyExplicitWrites separates builtin selection from output state.
//
// Example: selecting Sinc emits no CHANNEL command, while explicit false emits CHANNEL OFF.
func TestGeneratorOutputPresenceControlsOnlyExplicitWrites(t *testing.T) {
	waveform := owonmodel.GeneratorWaveformSinc
	for _, output := range []*bool{nil, new(false), new(true)} {
		backend := &scriptedBackend{}
		require.NoError(t, newTestInstrument(t, backend).SetGenerator(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform, Output: output}))
		want := []string{":FUNCTION Sinc"}
		if output != nil {
			command := ":CHANNEL OFF"
			if *output {
				command = ":CHANNEL ON"
			}
			want = append(want, command)
		}
		require.Equal(t, want, backend.Commands)
	}
}
