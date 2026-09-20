package owoncontrol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// TestIntrinsicValidationPrecedesDialectSupport specifies deterministic compound-invalid rejection before I/O.
//
// Example: averaging plus an unknown memory depth is invalid rather than merely unsupported.
func TestIntrinsicValidationPrecedesDialectSupport(t *testing.T) {
	backend := new(scriptedBackend)
	controller := newTestInstrument(t, backend)
	average, depth := owonmodel.AcquisitionModeAverage, owonmodel.AcquisitionMemoryDepth(99)
	external, slope := owonmodel.TriggerSourceExternal, owonmodel.TriggerSlope(99)
	manual, badRange := false, owonmodel.DMMRange(99)
	yes, waveform, frequency, load := true, owonmodel.GeneratorWaveformSine, 1e9, owonmodel.GeneratorLoad(99)
	_, waveformErr := controller.Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel(99)})
	for _, err := range []error{
		controller.SetAcquisition(t.Context(), &owonmodel.AcquisitionPatch{Mode: &average, MemoryDepth: &depth}),
		controller.SetTrigger(t.Context(), &owonmodel.TriggerPatch{Source: &external, Slope: &slope}),
		controller.SetDMM(t.Context(), &owonmodel.DMMPatch{AutoRange: &manual, Range: &badRange}),
		controller.SetMeasurement(t.Context(), &owonmodel.MeasurementPatch{ReplaceVisible: &yes, Visible: []*owonmodel.MeasurementSelector{nil}}),
		controller.SetGenerator(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform, FrequencyHz: &frequency, Load: &load}),
		waveformErr,
	} {
		var invalid *owonmodel.ErrInvalidRequest
		require.ErrorAs(t, err, &invalid)
		var unsupported *owonscpi.ErrUnsupportedControl
		require.False(t, errors.As(err, &unsupported))
		var setting *owonscpi.ErrInvalidSetting
		require.False(t, errors.As(err, &setting))
	}
	require.Empty(t, backend.Commands)
	for _, err := range []error{
		controller.SetTrigger(t.Context(), &owonmodel.TriggerPatch{Source: &external}),
		controller.SetDMM(t.Context(), &owonmodel.DMMPatch{AutoRange: &manual}),
		controller.SetMeasurement(t.Context(), &owonmodel.MeasurementPatch{ReplaceVisible: &yes}),
	} {
		var unsupported *owonscpi.ErrUnsupportedControl
		require.ErrorAs(t, err, &unsupported)
	}
	require.Empty(t, backend.Commands)
}
