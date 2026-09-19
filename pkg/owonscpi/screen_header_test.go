package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// requireHeaderTokens checks supported spellings and rejects unrecognized device tokens.
//
// Example: mixed-case firmware tokens decode while a future unknown token fails visibly.
func requireHeaderTokens[Value comparable](
	t *testing.T,
	parse func(string) (Value, error),
	accepted map[string]Value,
) {
	t.Helper()
	for token, want := range accepted {
		value, err := parse(token)
		require.NoError(t, err, token)
		require.Equal(t, want, value)
	}
	for _, token := range []string{"", "unknown"} {
		_, err := parse(token)
		require.Error(t, err, token)
	}
}

// TestScreenHeaderTokens verifies every supported header token without inventing firmware aliases.
//
// Example: both observed peak spellings map to peak detection, and invalid probe values fail.
func TestScreenHeaderTokens(t *testing.T) {
	t.Parallel()
	requireHeaderTokens(t, acquisitionModeFromHeader, map[string]owonmodel.AcquisitionMode{" sample ": owonmodel.AcquisitionModeSample, "PEAK": owonmodel.AcquisitionModePeakDetect, "PeakDetect": owonmodel.AcquisitionModePeakDetect, "AVERAGE": owonmodel.AcquisitionModeAverage})
	requireHeaderTokens(t, channelFromHeader, map[string]owonmodel.Channel{"CH1": owonmodel.Channel1, "CH2": owonmodel.Channel2})
	requireHeaderTokens(t, couplingFromHeader, map[string]owonmodel.Coupling{"AC": owonmodel.CouplingAC, "DC": owonmodel.CouplingDC, "GND": owonmodel.CouplingGround, "GROUND": owonmodel.CouplingGround})
	requireHeaderTokens(t, triggerSourceFromHeader, map[string]owonmodel.TriggerSource{"CH1": owonmodel.TriggerSourceChannel1, "CH2": owonmodel.TriggerSourceChannel2})
	requireHeaderTokens(t, triggerSlopeFromHeader, map[string]owonmodel.TriggerSlope{"RISE": owonmodel.TriggerSlopeRising, "RISING": owonmodel.TriggerSlopeRising, "FALL": owonmodel.TriggerSlopeFalling, "FALLING": owonmodel.TriggerSlopeFalling})
	requireHeaderTokens(t, triggerSweepFromHeader, map[string]owonmodel.TriggerSweep{"AUTO": owonmodel.TriggerSweepAuto, "NORMAL": owonmodel.TriggerSweepNormal, "NORM": owonmodel.TriggerSweepNormal, "SINGLE": owonmodel.TriggerSweepSingle})
	requireHeaderTokens(t, probeFromHeader, map[string]float64{"1X": 1, "10x": 10, " 100X ": 100})
	for token, want := range map[string]float64{"0V": 0, "-1.25V": -1.25, "500mV": 0.5, "200uV": 0.0002} {
		value, err := voltageFromHeader(token)
		require.NoError(t, err)
		require.InDelta(t, want, value, 1e-12)
	}
	for _, value := range []string{"", "bad", "badV", "NaNV", "+InfV"} {
		_, err := voltageFromHeader(value)
		require.Error(t, err)
	}
	for _, value := range []string{"badX", "NaNX", "0X", "-1X"} {
		_, err := probeFromHeader(value)
		require.Error(t, err)
	}
	value, err := booleanFromHeader("display", "OFF")
	require.NoError(t, err)
	require.False(t, value)
	_, err = booleanFromHeader("display", "neither")
	require.Error(t, err)
}

// TestScreenHeaderControlBoundaryRejectsCorruptFields verifies each invalid nested field fails the whole snapshot.
//
// Example: a corrupt trigger coupling cannot produce a partly populated control snapshot.
func TestScreenHeaderControlBoundaryRejectsCorruptFields(t *testing.T) {
	t.Parallel()
	for _, badField := range []string{"channel", "display", "coupling", "probe", "mode", "type", "source", "trigger coupling", "slope", "level", "sweep", "timebase"} {
		header := &ScreenHeader{
			Sample: screenHeaderSample{Type: "SAMPLE"}, Timebase: screenHeaderTimebase{Scale: "20us"},
			Channels: []screenHeaderChannel{{Name: "CH1", Display: "ON", Coupling: "DC", Probe: "10X"}},
			Trigger:  screenHeaderTrigger{Mode: "SINGLE", Type: "EDGE", Items: screenHeaderTriggerItems{Channel: "CH1", Coupling: "DC", Edge: "RISE", Level: "0V", Sweep: "AUTO"}},
		}
		switch badField {
		case "channel":
			header.Channels[0].Name = "bad"
		case "display":
			header.Channels[0].Display = "bad"
		case "coupling":
			header.Channels[0].Coupling = "bad"
		case "probe":
			header.Channels[0].Probe = "bad"
		case "mode":
			header.Trigger.Mode = "bad"
		case "type":
			header.Trigger.Type = "bad"
		case "source":
			header.Trigger.Items.Channel = "bad"
		case "trigger coupling":
			header.Trigger.Items.Coupling = "bad"
		case "slope":
			header.Trigger.Items.Edge = "bad"
		case "level":
			header.Trigger.Items.Level = "bad"
		case "sweep":
			header.Trigger.Items.Sweep = "bad"
		case "timebase":
			header.Timebase.Scale = ""
		}
		controls, err := header.Controls()
		requireErrorType[*ErrMalformedResponse](t, err)
		require.Nil(t, controls)
	}
	_, err := (*ScreenHeader)(nil).Controls()
	requireErrorType[*ErrMalformedResponse](t, err)
	trigger, err := triggerStateFromHeader(screenHeaderTrigger{Mode: "SINGLE", Type: "EDGE", Items: screenHeaderTriggerItems{Channel: "CH2", Coupling: "AC", Edge: "FALL", Level: "500mV", Sweep: "NORM"}}, " WAIT ")
	require.NoError(t, err)
	require.Equal(t, owonmodel.TriggerSweepNormal, trigger.Sweep)
	require.Equal(t, 0.5, trigger.Level)
	require.Equal(t, "WAIT", trigger.Status)
}
