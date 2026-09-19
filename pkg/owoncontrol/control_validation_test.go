package owoncontrol

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// TestScalePatchRejectsUnknownHeaderChannel verifies malformed metadata cannot authorize a scale write.
//
// Example: an unknown channel object preceding CH1 fails before the otherwise valid scale command.
func TestScalePatchRejectsUnknownHeaderChannel(t *testing.T) {
	t.Parallel()
	backend := &scriptedBackend{Responses: map[string][]byte{":DATA:WAVE:SCREEN:HEAD?": []byte(`{"CHANNEL":[{"NAME":"broken","PROBE":"10X"},{"NAME":"CH1","PROBE":"10X"}]}`)}}
	scale := "100mV"
	err := newTestInstrument(t, backend).SetChannel(t.Context(), &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Scale: &scale})
	requireErrorType[*owonscpi.ErrMalformedResponse](t, err)
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands)
}

// requireRejectedPatches verifies validation occurs before any session mutation.
//
// Example: a valid first field followed by an invalid enum must not emit a partial patch.
func requireRejectedPatches[Request any](
	t *testing.T,
	call func(
		context.Context,
		*Request,
	) error,
	requests ...*Request,
) {
	t.Helper()
	require.Error(t, call(t.Context(), nil))
	for _, request := range requests {
		require.Error(t, call(t.Context(), request))
	}
}

// TestControlValidationRejectsMalformedPatches verifies range, presence and enum failures precede all writes.
//
// Example: invalid probe, offset and trigger values all leave the backend command log empty.
func TestControlValidationRejectsMalformedPatches(t *testing.T) {
	t.Parallel()
	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	yes, zero, negative, oddProbe, nan := true, 0.0, -1.0, 3.0, math.NaN()
	largeOffset := int64(201)
	unknownCoupling := owonmodel.CouplingUnspecified
	unknownMode := owonmodel.AcquisitionModeUnspecified
	unknownSource := owonmodel.TriggerSourceUnspecified
	external := owonmodel.TriggerSourceExternal
	unknownSweep := owonmodel.TriggerSweepUnspecified
	ground := owonmodel.CouplingGround
	unknownFunction := owonmodel.DMMFunctionUnspecified
	voltage := owonmodel.DMMFunctionVoltage
	resistance := owonmodel.DMMFunctionResistance
	unknownCurrent := owonmodel.DMMCurrentTypeUnspecified
	dc := owonmodel.DMMCurrentTypeDC
	unknownWaveform := owonmodel.GeneratorWaveformUnspecified
	badDepth, smallScale := owonmodel.AcquisitionMemoryDepth(999), "10.0mV"
	badSymmetry := int32(-1)
	probe := 10.0
	requireRejectedPatches(t, controller.SetChannel,
		&owonmodel.ChannelPatch{Display: &yes},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Coupling: &unknownCoupling},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, ProbeAttenuation: &negative},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, ProbeAttenuation: &oddProbe},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, ProbeAttenuation: &probe, Scale: &smallScale},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, OffsetDivisions: &largeOffset},
		&owonmodel.ChannelPatch{Channel: owonmodel.Channel1, BandwidthLimit: &yes},
	)
	requireRejectedPatches(t, controller.SetAcquisition, &owonmodel.AcquisitionPatch{Mode: &unknownMode}, &owonmodel.AcquisitionPatch{MemoryDepth: &badDepth})
	requireRejectedPatches(t, controller.SetHorizontal)
	requireRejectedPatches(t, controller.SetTrigger, &owonmodel.TriggerPatch{Source: &unknownSource}, &owonmodel.TriggerPatch{Source: &external}, &owonmodel.TriggerPatch{Coupling: &unknownCoupling}, &owonmodel.TriggerPatch{Coupling: &ground}, &owonmodel.TriggerPatch{LevelVolts: &nan}, &owonmodel.TriggerPatch{Sweep: &unknownSweep})
	requireRejectedPatches(t, controller.SetMeasurement)
	requireRejectedPatches(t, controller.SetDMM, &owonmodel.DMMPatch{Function: &unknownFunction}, &owonmodel.DMMPatch{Function: &voltage}, &owonmodel.DMMPatch{Function: &voltage, CurrentType: &unknownCurrent}, &owonmodel.DMMPatch{Function: &resistance, CurrentType: &dc}, &owonmodel.DMMPatch{CurrentType: &dc})
	requireRejectedPatches(t, controller.SetGenerator, &owonmodel.GeneratorPatch{Waveform: &unknownWaveform}, &owonmodel.GeneratorPatch{PeriodSeconds: &zero}, &owonmodel.GeneratorPatch{SymmetryPercent: &badSymmetry}, &owonmodel.GeneratorPatch{DutyPercent: &nan})
	require.Empty(t, backend.Commands)
}

// TestAdditionalVerifiedControlTokens verifies less common valid enum paths and preserves command order.
//
// Example: capacitance and diode select their documented DMM functions without guessed current-type tokens.
func TestAdditionalVerifiedControlTokens(t *testing.T) {
	t.Parallel()
	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	for _, function := range []owonmodel.DMMFunction{owonmodel.DMMFunctionCapacitance, owonmodel.DMMFunctionDiode, owonmodel.DMMFunctionContinuity} {
		require.NoError(t, controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Function: &function}))
	}
	for _, value := range []owonmodel.DMMRange{owonmodel.DMMRangeOn, owonmodel.DMMRangeV} {
		require.NoError(t, controller.SetDMM(t.Context(), &owonmodel.DMMPatch{Range: &value}))
	}
	for _, waveform := range []owonmodel.GeneratorWaveform{owonmodel.GeneratorWaveformSquare, owonmodel.GeneratorWaveformRamp, owonmodel.GeneratorWaveformPulse} {
		require.NoError(t, controller.SetGenerator(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform}))
	}
	load, symmetry, duty := owonmodel.GeneratorLoadOff, int32(50), 100.0
	require.NoError(t, controller.SetGenerator(t.Context(), &owonmodel.GeneratorPatch{Load: &load, SymmetryPercent: &symmetry, DutyPercent: &duty}))
	for _, coupling := range []owonmodel.Coupling{owonmodel.CouplingDC, owonmodel.CouplingGround} {
		require.NoError(t, controller.SetChannel(t.Context(), &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Coupling: &coupling}))
	}
	for _, sweep := range []owonmodel.TriggerSweep{owonmodel.TriggerSweepAuto, owonmodel.TriggerSweepSingle} {
		require.NoError(t, controller.SetTrigger(t.Context(), &owonmodel.TriggerPatch{Sweep: &sweep}))
	}
	coupling, depth := owonmodel.CouplingDC, owonmodel.AcquisitionMemoryDepth8K
	require.NoError(t, controller.SetTrigger(t.Context(), &owonmodel.TriggerPatch{Coupling: &coupling}))
	require.NoError(t, controller.SetAcquisition(t.Context(), &owonmodel.AcquisitionPatch{MemoryDepth: &depth}))
	require.Equal(t, []string{":DMM:CONFIGURE CAPACITANCE", ":DMM:CONFIGURE DIODE", ":DMM:CONFIGURE CONTINUITY", ":DMM:RANGE ON", ":DMM:RANGE V", ":FUNCTION SQUARE", ":FUNCTION RAMP", ":FUNCTION PULSE", ":FUNCTION:SYMMETRY 50", ":FUNCTION:DTYCYCLE 100", ":FUNCTION:LOAD OFF", ":CH1:COUPLING DC", ":CH1:COUPLING GND", ":TRIGGER:SINGLE:SWEEP AUTO", ":TRIGGER:SINGLE:SWEEP SINGLE", ":TRIGGER:SINGLE:COUPLING DC", ":ACQUIRE:DEPMEM 8K"}, backend.Commands)
}

// TestMeasurementKindsPreserveUnits verifies typed measurement selectors use their documented suffix and units.
//
// Example: a period is seconds while a maximum voltage is volts.
func TestMeasurementKindsPreserveUnits(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Kind    owonmodel.MeasurementKind
		Command string
		Unit    string
	}{
		{owonmodel.MeasurementKindMaximum, "MAX", "V"},
		{owonmodel.MeasurementKindMinimum, "MIN", "V"},
		{owonmodel.MeasurementKindPeakToPeak, "PKPK", "V"},
		{owonmodel.MeasurementKindAmplitude, "VAMP", "V"},
		{owonmodel.MeasurementKindAverage, "AVERAGE", "V"},
		{owonmodel.MeasurementKindPeriod, "PERIOD", "s"},
	} {
		command := ":MEASUREMENT:CH2:" + testCase.Command + "?"
		backend := &scriptedBackend{Responses: map[string][]byte{command: []byte("0")}}
		measurement, err := newTestInstrument(t, backend).Measure(t.Context(), &owonmodel.MeasurementSelector{Channel: owonmodel.Channel2, Kind: testCase.Kind})
		require.NoError(t, err)
		require.Equal(t, testCase.Unit, measurement.Unit)
		require.Zero(t, measurement.Value)
		require.Equal(t, []string{command}, backend.Commands)
	}
	_, err := owonscpi.MeasurementQuery(&owonmodel.MeasurementSelector{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKind(999)})
	requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
	for _, selectors := range [][]*owonmodel.MeasurementSelector{{nil}, {{Kind: owonmodel.MeasurementKindFrequency}}, {{Channel: owonmodel.Channel1}}} {
		requireErrorType[*owonmodel.ErrInvalidRequest](t, owonmodel.ValidateMeasurementSelectors(selectors))
	}
}
