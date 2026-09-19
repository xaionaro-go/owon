package owoncontrol

import (
	"context"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// channelSetterRunner owns one concurrent SetChannel result.
//
// Example: scale/probe serialization tests execute two patches through separate runners.
type channelSetterRunner struct {
	Controller *Controller
	Request    *owonmodel.ChannelPatch
	Started    chan<- struct{}
	Result     chan<- error
}

// Run reports entry, applies its channel patch, and publishes the result.
//
// Example: a probe mutation reports entry while waiting behind a scale-only transaction.
func (runner channelSetterRunner) Run(ctx context.Context) {
	runner.Started <- struct{}{}
	runner.Result <- runner.Controller.SetChannel(ctx, runner.Request)
}

// probeBlockingBackend pauses the screen-header exchange while recording command order.
//
// Example: a concurrent probe patch can attempt admission while scale validation holds it.
type probeBlockingBackend struct {
	mu            sync.Mutex
	commands      []string
	headerEntered chan struct{}
	releaseHeader chan struct{}
}

// Exchange blocks the header query and returns a verified current-probe fixture.
//
// Example: CH1 reports 10X before the held scale operation emits its write.
func (backend *probeBlockingBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.recordCommand(command.Text)
	if command.Text != ":DATA:WAVE:SCREEN:HEAD?" {
		return nil, nil
	}
	close(backend.headerEntered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-backend.releaseHeader:
		return []byte(`{"CHANNEL":[{"NAME":"CH1","PROBE":"10X"}]}`), nil
	}
}

// recordCommand appends one command while holding the fixture mutex only for the mutation.
//
// Example: Exchange records a command without retaining the lock while it waits on a channel.
func (backend *probeBlockingBackend) recordCommand(command string) {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	backend.commands = append(backend.commands, command)
}

// ReopenAndValidate satisfies Backend for a test that never poisons its session.
//
// Example: no reconnect is expected during the successful concurrency scenario.
func (backend *probeBlockingBackend) ReopenAndValidate(
	_ context.Context,
	_ owonmodel.SerialNumber,
) error {
	return nil
}

// Close satisfies Backend without external resources.
//
// Example: the fixture owns only channels and an in-memory command slice.
func (backend *probeBlockingBackend) Close(_ context.Context) error {
	return nil
}

// Commands returns a race-free snapshot of recorded commands.
//
// Example: the test asserts HEAD,SCALE,PROBE operation ordering.
func (backend *probeBlockingBackend) Commands() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return append([]string(nil), backend.commands...)
}

// TestInstrumentAppliesTypedControlPatches verifies each control subsystem emits explicit SCPI.
//
// Example: zero-valued offsets and false output states are preserved by native pointer presence.
func TestInstrumentAppliesTypedControlPatches(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	mode := owonmodel.AcquisitionModePeakDetect
	scale := "20us"
	offset := int64(0)
	source := owonmodel.TriggerSourceChannel1
	level := float64(0)
	slope := owonmodel.TriggerSlopeRising
	waveform := owonmodel.GeneratorWaveformSine
	frequency := 1000.0
	output := false
	function := owonmodel.DMMFunctionVoltage
	currentType := owonmodel.DMMCurrentTypeDC
	autoRange := true

	require.NoError(t, controller.SetAcquisition(context.Background(), &owonmodel.AcquisitionPatch{Mode: &mode}))
	require.NoError(t, controller.SetHorizontal(context.Background(), &owonmodel.HorizontalPatch{Scale: &scale, OffsetDivisions: &offset}))
	require.NoError(t, controller.SetTrigger(context.Background(), &owonmodel.TriggerPatch{Source: &source, Slope: &slope, LevelVolts: &level}))
	require.NoError(t, controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{Waveform: &waveform, FrequencyHz: &frequency, Output: &output}))
	require.NoError(t, controller.SetDMM(context.Background(), &owonmodel.DMMPatch{Function: &function, CurrentType: &currentType, AutoRange: &autoRange}))
	require.Equal(t, []string{
		":ACQUIRE:MODE PEAK",
		":HORIZONTAL:SCALE 20us",
		":HORIZONTAL:OFFSET 0",
		":TRIGGER:SINGLE:SOURCE CH1",
		":TRIGGER:SINGLE:EDGE RISE",
		":TRIGGER:SINGLE:EDGE:LEVEL 0V",
		":FUNCTION SINE",
		":FUNCTION:FREQUENCY 1000",
		":CHANNEL OFF",
		":DMM:CONFIGURE:VOLTAGE DC",
		":DMM:AUTO ON",
	}, backend.Commands)
}

// TestInstrumentRejectsGeneratorFrequencyWithoutWaveformBeforeUSB verifies the safe typed boundary.
//
// Example: frequency-only input is rejected before any guessed model-specific command is emitted.
func TestInstrumentRejectsGeneratorFrequencyWithoutWaveformBeforeUSB(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	frequency := 1000.0

	err := controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{FrequencyHz: &frequency})
	requireErrorType[*owonscpi.ErrInvalidSetting](t, err)
	require.Empty(t, backend.Commands)
}

// TestInstrumentSerializesTriggerVoltage verifies unit-bearing finite level formatting.
//
// Example: zero, a negative value, and one microvolt retain their exact sign and scale.
func TestInstrumentSerializesTriggerVoltage(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	for _, level := range []float64{0, -1.25, 0.000001} {
		require.NoError(t, controller.SetTrigger(context.Background(), &owonmodel.TriggerPatch{LevelVolts: &level}))
	}
	require.Equal(t, []string{
		":TRIGGER:SINGLE:EDGE:LEVEL 0V",
		":TRIGGER:SINGLE:EDGE:LEVEL -1.25V",
		":TRIGGER:SINGLE:EDGE:LEVEL 1e-06V",
	}, backend.Commands)
}

// TestInstrumentRejectsUndocumentedControlCommands verifies typed fields never invent SCPI.
//
// Example: averaging and visible-measurement replacement fail before a USB write.
func TestInstrumentRejectsUndocumentedControlCommands(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	average := owonmodel.AcquisitionModeAverage
	averageCount := uint32(16)
	replace := true
	doNotReplace := false
	autoRange := false

	require.ErrorContains(t, controller.SetAcquisition(context.Background(), &owonmodel.AcquisitionPatch{Mode: &average}), "unsupported")
	require.ErrorContains(t, controller.SetAcquisition(context.Background(), &owonmodel.AcquisitionPatch{AverageCount: &averageCount}), "unsupported")
	require.ErrorContains(t, controller.SetMeasurement(context.Background(), &owonmodel.MeasurementPatch{ReplaceVisible: &replace}), "unsupported")
	require.Error(t, controller.SetMeasurement(context.Background(), &owonmodel.MeasurementPatch{
		ReplaceVisible: &doNotReplace,
		Visible: []*owonmodel.MeasurementSelector{{
			Channel: owonmodel.Channel1,
			Kind:    owonmodel.MeasurementKindFrequency,
		}},
	}))
	require.ErrorContains(t, controller.SetDMM(context.Background(), &owonmodel.DMMPatch{AutoRange: &autoRange}), "unsupported")
	require.Empty(t, backend.Commands)
}

// TestInstrumentDMMUsesDocumentedConfigurationForms verifies function-specific DMM paths.
//
// Example: resistance uses CONFIGURE while current AC uses CONFIGURE:CURRENT.
func TestInstrumentDMMUsesDocumentedConfigurationForms(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	resistance := owonmodel.DMMFunctionResistance
	current := owonmodel.DMMFunctionCurrent
	currentType := owonmodel.DMMCurrentTypeAC
	relative := true
	rangeValue := owonmodel.DMMRangeMV

	require.NoError(t, controller.SetDMM(context.Background(), &owonmodel.DMMPatch{Function: &resistance}))
	require.NoError(t, controller.SetDMM(context.Background(), &owonmodel.DMMPatch{Function: &current, CurrentType: &currentType, Relative: &relative, Range: &rangeValue}))
	require.Equal(t, []string{
		":DMM:CONFIGURE RESISTANCE",
		":DMM:CONFIGURE:CURRENT AC",
		":DMM:REL ON",
		":DMM:RANGE mV",
	}, backend.Commands)
}

// TestInstrumentRejectsNonFiniteControls verifies invalid floating-point values never reach USB.
//
// Example: NaN generator amplitude returns an error with no recorded command.
func TestInstrumentRejectsNonFiniteControls(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	amplitude := math.NaN()

	require.Error(t, controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{AmplitudeVolts: &amplitude}))
	require.Empty(t, backend.Commands)
}

// TestInstrumentValidatesDocumentedControlMatrices verifies accepted limits and rejected holes.
//
// Example: a square-wave frequency above five MHz and a three-microsecond timebase fail closed.
func TestInstrumentValidatesDocumentedControlMatrices(t *testing.T) {
	t.Parallel()

	probe := 10.0
	validScale := "100mV"
	brokenScale := "1.00kV"
	badHorizontal := "3us"
	square := owonmodel.GeneratorWaveformSquare
	overSquareMaximum := 5_000_001.0
	badSymmetry := int32(101)
	badDuty := 100.1
	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)

	require.NoError(t, controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel:          owonmodel.Channel1,
		ProbeAttenuation: &probe,
		Scale:            &validScale,
	}))
	requireErrorType[*owonscpi.ErrInvalidSetting](t, controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Scale: &brokenScale}))
	requireErrorType[*owonscpi.ErrInvalidSetting](t, controller.SetHorizontal(context.Background(), &owonmodel.HorizontalPatch{Scale: &badHorizontal}))
	requireErrorType[*owonscpi.ErrInvalidSetting](t, controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{Waveform: &square, FrequencyHz: &overSquareMaximum}))
	requireErrorType[*owonscpi.ErrInvalidSetting](t, controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{SymmetryPercent: &badSymmetry}))
	requireErrorType[*owonscpi.ErrInvalidSetting](t, controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{DutyPercent: &badDuty}))
	require.Equal(t, []string{":CH1:PROBE 10X", ":CH1:SCALE 100mV"}, backend.Commands)
}

// TestInstrumentScaleOnlyPatchUsesCurrentProbe verifies dynamic validation precedes mutation.
//
// Example: CH1 at 10X accepts 100mV after one screen-header read in the same operation.
func TestInstrumentScaleOnlyPatchUsesCurrentProbe(t *testing.T) {
	t.Parallel()

	scale := "100mV"
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"CHANNEL":[{"NAME":"CH1","PROBE":"10X"}]}`),
	}}
	controller := newTestInstrument(t, backend)
	require.NoError(t, controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel: owonmodel.Channel1,
		Scale:   &scale,
	}))
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?", ":CH1:SCALE 100mV"}, backend.Commands)
}

// TestInstrumentScalePatchNormalizesObservedPresentationWhitespace verifies state round trips.
//
// Example: a header-rendered `1.00 V` token is emitted as canonical `1.00V` SCPI.
func TestInstrumentScalePatchNormalizesObservedPresentationWhitespace(t *testing.T) {
	t.Parallel()

	scale := "1.00 V"
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"CHANNEL":[{"NAME":"CH1","PROBE":"1X"}]}`),
	}}
	controller := newTestInstrument(t, backend)
	require.NoError(t, controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel: owonmodel.Channel1,
		Scale:   &scale,
	}))
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?", ":CH1:SCALE 1.00V"}, backend.Commands)
}

// TestInstrumentScaleOnlyPatchRejectsProbeMismatch verifies no mutation follows incompatibility.
//
// Example: CH1 at 1X rejects a 100V scale after its header read and emits no scale write.
func TestInstrumentScaleOnlyPatchRejectsProbeMismatch(t *testing.T) {
	t.Parallel()

	scale := "100V"
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"CHANNEL":[{"NAME":"CH1","PROBE":"1X"}]}`),
	}}
	controller := newTestInstrument(t, backend)
	err := controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel: owonmodel.Channel1,
		Scale:   &scale,
	})
	requireErrorType[*owonscpi.ErrInvalidSetting](t, err)
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands)
}

// TestInstrumentScaleOnlyPatchRejectsMissingProbe verifies malformed state cannot authorize a write.
//
// Example: a header without the selected channel fails closed after one read.
func TestInstrumentScaleOnlyPatchRejectsMissingProbe(t *testing.T) {
	t.Parallel()

	scale := "100mV"
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"CHANNEL":[{"NAME":"CH2","PROBE":"10X"}]}`),
	}}
	controller := newTestInstrument(t, backend)
	err := controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel: owonmodel.Channel1,
		Scale:   &scale,
	})
	requireErrorType[*owonscpi.ErrMalformedResponse](t, err)
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands)
}

// TestInstrumentScaleOnlyPatchExcludesConcurrentProbeMutation verifies operation-wide serialization.
//
// Example: a concurrent 100X patch cannot appear between the current-probe read and scale write.
func TestInstrumentScaleOnlyPatchExcludesConcurrentProbeMutation(t *testing.T) {
	t.Parallel()

	backend := &probeBlockingBackend{headerEntered: make(chan struct{}), releaseHeader: make(chan struct{})}
	session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	controller, err := New(session, owonmodel.DeviceIdentity{})
	require.NoError(t, err)
	scale := "100mV"
	probe := 100.0
	scaleStarted := make(chan struct{}, 1)
	scaleResult := make(chan error, 1)
	observability.Go(context.Background(), channelSetterRunner{
		Controller: controller, Request: &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Scale: &scale},
		Started: scaleStarted, Result: scaleResult,
	}.Run)
	<-scaleStarted
	<-backend.headerEntered
	probeStarted := make(chan struct{}, 1)
	probeResult := make(chan error, 1)
	observability.Go(context.Background(), channelSetterRunner{
		Controller: controller, Request: &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, ProbeAttenuation: &probe},
		Started: probeStarted, Result: probeResult,
	}.Run)
	<-probeStarted
	close(backend.releaseHeader)
	require.NoError(t, <-scaleResult)
	require.NoError(t, <-probeResult)
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?", ":CH1:SCALE 100mV", ":CH1:PROBE 100X"}, backend.Commands())
}

// TestInstrumentSettersValidateBeforeWrites verifies a late invalid field prevents every write.
//
// Example: display=true plus unsupported inversion emits neither channel command.
func TestInstrumentSettersValidateBeforeWrites(t *testing.T) {
	t.Parallel()

	enabled := true
	t.Run("channel",
		// rejectChannelInversion verifies display is not written before unsupported inversion.
		//
		// Example: a patch containing both fields leaves the command log empty.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Display: &enabled, Inversion: &enabled})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("acquisition",
		// rejectAcquisitionAverage verifies the mode is not written before an unsupported count.
		//
		// Example: sample mode with an average count rejects the complete patch.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			mode := owonmodel.AcquisitionModeSample
			average := uint32(2)
			err := controller.SetAcquisition(context.Background(), &owonmodel.AcquisitionPatch{Mode: &mode, AverageCount: &average})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("horizontal",
		// rejectHorizontalScale verifies an invalid scale prevents the offset write.
		//
		// Example: offset zero cannot make an invalid scale patch partially applicable.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			scale := "invalid"
			offset := int64(0)
			err := controller.SetHorizontal(context.Background(), &owonmodel.HorizontalPatch{Scale: &scale, OffsetDivisions: &offset})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("trigger",
		// rejectTriggerSlope verifies an invalid slope prevents the source write.
		//
		// Example: a valid channel source does not authorize an unspecified slope.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			source := owonmodel.TriggerSourceChannel1
			invalidSlope := owonmodel.TriggerSlopeUnspecified
			err := controller.SetTrigger(context.Background(), &owonmodel.TriggerPatch{Source: &source, Slope: &invalidSlope})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("measurement",
		// rejectMeasurementReplacement verifies inconsistent selector intent prevents display writes.
		//
		// Example: visible selectors with replacement disabled reject the entire patch.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			replace := false
			err := controller.SetMeasurement(context.Background(), &owonmodel.MeasurementPatch{Display: &enabled, ReplaceVisible: &replace, Visible: []*owonmodel.MeasurementSelector{{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKindFrequency}}})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("generator",
		// rejectGeneratorLoad verifies an invalid load prevents the waveform write.
		//
		// Example: a valid sine waveform cannot hide an unknown load value.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			waveform := owonmodel.GeneratorWaveformSine
			invalidLoad := owonmodel.GeneratorLoad(999)
			err := controller.SetGenerator(context.Background(), &owonmodel.GeneratorPatch{Waveform: &waveform, Load: &invalidLoad})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("DMM",
		// rejectDMMRange verifies an invalid range prevents the relative-mode write.
		//
		// Example: relative mode remains untouched when the requested range is unknown.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			invalidRange := owonmodel.DMMRange(999)
			err := controller.SetDMM(context.Background(), &owonmodel.DMMPatch{Relative: &enabled, Range: &invalidRange})
			require.Error(t, err)
			require.Empty(t, backend.Commands)
		})
}

// TestInstrumentRejectsEmptySetterPatches verifies no-op mutations are caller errors.
//
// Example: explicit false remains valid while a request containing no optional field is rejected.
func TestInstrumentRejectsEmptySetterPatches(t *testing.T) {
	t.Parallel()

	t.Run("channel",
		// rejectEmptyChannel verifies a channel selector alone is not a mutation.
		//
		// Example: channel one without any optional settings returns ErrInvalidRequest.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{Channel: owonmodel.Channel1})
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("acquisition",
		// rejectEmptyAcquisition verifies an absent acquisition change is invalid.
		//
		// Example: an empty acquisition patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetAcquisition(context.Background(), new(owonmodel.AcquisitionPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("horizontal",
		// rejectEmptyHorizontal verifies an absent horizontal change is invalid.
		//
		// Example: an empty horizontal patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetHorizontal(context.Background(), new(owonmodel.HorizontalPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("trigger",
		// rejectEmptyTrigger verifies an absent trigger change is invalid.
		//
		// Example: an empty trigger patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetTrigger(context.Background(), new(owonmodel.TriggerPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("measurement",
		// rejectEmptyMeasurement verifies an absent measurement change is invalid.
		//
		// Example: an empty measurement patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetMeasurement(context.Background(), new(owonmodel.MeasurementPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("measurement no-op",
		// rejectDisabledReplacement verifies false replacement alone does not change measurements.
		//
		// Example: a no-op replacement patch returns ErrInvalidRequest without writes.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			replace := false
			err := controller.SetMeasurement(context.Background(), &owonmodel.MeasurementPatch{ReplaceVisible: &replace})
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("generator",
		// rejectEmptyGenerator verifies an absent generator change is invalid.
		//
		// Example: an empty generator patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetGenerator(context.Background(), new(owonmodel.GeneratorPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
	t.Run("DMM",
		// rejectEmptyDMM verifies an absent multimeter change is invalid.
		//
		// Example: an empty DMM patch emits no device commands.
		func(t *testing.T) {
			backend := &scriptedBackend{}
			controller := newTestInstrument(t, backend)
			err := controller.SetDMM(context.Background(), new(owonmodel.DMMPatch))
			requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
			require.Empty(t, backend.Commands)
		})
}

// TestInstrumentDMMMeasurementUsesVerifiedQuery verifies the hardware-observed DMM query.
//
// Example: a numeric `:DMM:MEAS?` response is preserved and parsed without an alias query.
func TestInstrumentDMMMeasurementUsesVerifiedQuery(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		":DMM:MEAS?": []byte("1.2500 V"),
	}}
	controller := newTestInstrument(t, backend)

	measurement, err := controller.DMMMeasurement(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1.25, measurement.Value)
	require.Equal(t, "V", measurement.Unit)
	require.Equal(t, "1.2500 V", measurement.Raw)
	require.Equal(t, []string{":DMM:MEAS?"}, backend.Commands)
}

// TestInstrumentRejectsNonFiniteReadings verifies protocol sentinels do not become measurements.
//
// Example: a textual NaN from either subsystem returns an error instead of a metric value.
func TestInstrumentRejectsNonFiniteReadings(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		":DMM:MEAS?":                  []byte("NaN V"),
		":MEASUREMENT:CH1:FREQUENCY?": []byte("+Inf"),
	}}
	controller := newTestInstrument(t, backend)

	_, err := controller.DMMMeasurement(context.Background())
	require.ErrorContains(t, err, "finite")
	_, err = controller.Measure(context.Background(), &owonmodel.MeasurementSelector{
		Channel: owonmodel.Channel1,
		Kind:    owonmodel.MeasurementKindFrequency,
	})
	require.ErrorContains(t, err, "finite")
}

// TestInstrumentAcquisitionActionsAndWaveform verifies typed run control and raw waveform preservation.
//
// Example: a screen CH1 waveform returns unparsed bytes plus the screen header JSON.
func TestInstrumentAcquisitionActionsAndWaveform(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"SAMPLE":{"DATALEN":3},"MODEL":"HDS2202S_LS"}`),
		":DATA:WAVE:SCREEN:CH1?":  {1, 2, 3},
	}}
	controller := newTestInstrument(t, backend)
	require.NoError(t, controller.Run(context.Background()))
	require.NoError(t, controller.Stop(context.Background()))
	require.NoError(t, controller.Single(context.Background()))
	waveform, err := controller.Waveform(context.Background(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, waveform.Data)
	require.Nil(t, waveform.Metadata.SampleCount)
	require.Equal(t, uint64(3), waveform.Metadata.DataLengthBytes)
	require.Equal(t, "owon-raw-unverified", waveform.Encoding)
	require.Equal(t, []string{":RUN", ":STOP", ":SINGLE", ":DATA:WAVE:SCREEN:HEAD?", ":DATA:WAVE:SCREEN:CH1?"}, backend.Commands)
}

// TestInstrumentWaveformDoesNotInferSampleCount verifies DATALEN remains opaque metadata.
//
// Example: arbitrary or missing header DATALEN never becomes an asserted sample count.
func TestInstrumentWaveformDoesNotInferSampleCount(t *testing.T) {
	t.Parallel()

	for _, header := range []string{`{"SAMPLE":{"DATALEN":999}}`, `{}`} {
		backend := &scriptedBackend{Responses: map[string][]byte{
			":DATA:WAVE:SCREEN:HEAD?": []byte(header),
			":DATA:WAVE:SCREEN:CH1?":  {1, 2, 3},
		}}
		controller := newTestInstrument(t, backend)
		waveform, err := controller.Waveform(context.Background(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
		require.NoError(t, err)
		require.Nil(t, waveform.Metadata.SampleCount)
		require.Equal(t, uint64(3), waveform.Metadata.DataLengthBytes)
		require.Equal(t, []byte(header), waveform.ScreenHeaderJSON)
	}
}

// TestInstrumentWaveformRejectsMalformedHeader verifies metadata corruption fails closed.
//
// Example: invalid JSON is not silently presented as a zero-sample waveform.
func TestInstrumentWaveformRejectsMalformedHeader(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte("not-json"),
	}}
	controller := newTestInstrument(t, backend)

	_, err := controller.Waveform(context.Background(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
	require.ErrorContains(t, err, "parse waveform header")
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands)
}

// TestInstrumentWaveformRejectsUnverifiedCaptureMode verifies only screen framing is exposed.
//
// Example: screen=false returns unsupported without issuing a guessed data command.
func TestInstrumentWaveformRejectsUnverifiedCaptureMode(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	_, err := controller.Waveform(context.Background(), &owonmodel.WaveformRequest{
		Channel: owonmodel.Channel1,
	})
	requireErrorType[*owonscpi.ErrUnsupportedControl](t, err)
	require.Empty(t, backend.Commands)
}

// TestInstrumentStateCollectsRequestedMeasurements verifies state reads only requested data.
//
// Example: one frequency selector produces identity and one measurement query without a header query.
func TestInstrumentStateCollectsRequestedMeasurements(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		"*IDN?":                       []byte("OWON,HDS2202S,25061855,V2.6.0"),
		":MEASUREMENT:CH1:FREQUENCY?": []byte("1.0000e+03"),
	}}
	controller := newTestInstrument(t, backend)
	state, err := controller.State(context.Background(), &owonmodel.StateRequest{Measurements: []*owonmodel.MeasurementSelector{{
		Channel: owonmodel.Channel1,
		Kind:    owonmodel.MeasurementKindFrequency,
	}}})
	require.NoError(t, err)
	require.Equal(t, owonmodel.SerialNumber("25061855"), state.Device.Serial)
	require.Len(t, state.Measurements, 1)
	require.Equal(t, 1000.0, state.Measurements[0].Value)
	require.Empty(t, state.ScreenHeaderJSON)
}

// TestInstrumentStateRejectsDuplicateMeasurementsBeforeIdentity verifies bounded unary polling.
//
// Example: duplicate CH1 frequency selectors issue no identity or measurement query.
func TestInstrumentStateRejectsDuplicateMeasurementsBeforeIdentity(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	selector := &owonmodel.MeasurementSelector{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKindFrequency}
	_, err := controller.State(context.Background(), &owonmodel.StateRequest{Measurements: []*owonmodel.MeasurementSelector{selector, selector}})
	requireErrorType[*owonmodel.ErrInvalidRequest](t, err)
	require.Empty(t, backend.Commands)
}

// TestInstrumentStateDecodesVerifiedHeaderControls verifies typed state accompanies raw metadata.
//
// Example: timebase, acquisition, and channel fields are decoded from the observed screen header schema.
func TestInstrumentStateDecodesVerifiedHeaderControls(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{Responses: map[string][]byte{
		"*IDN?": []byte("OWON,HDS2202S,25061855,V2.6.0"),
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{
			"TIMEBASE":{"SCALE":"20us","HOFFSET":0},
			"SAMPLE":{"TYPE":"SAMPle","DEPMEM":"4K","DATALEN":600},
			"CHANNEL":[{"NAME":"CH1","DISPLAY":"ON","COUPLING":"DC","PROBE":"10X","SCALE":"1.00 V","OFFSET":-78}],
			"RUNSTATUS":"TRIG",
			"Trig":{"Mode":"SINGle","Type":"Edge","Items":{"Channel":"CH1","Level":"1.52V","Edge":"RISE","Coupling":"DC"},"Sweep":"AUTO"}
		}`),
	}}
	controller := newTestInstrument(t, backend)
	state, err := controller.State(context.Background(), &owonmodel.StateRequest{IncludeControls: true})
	require.NoError(t, err)
	require.Equal(t, "20us", state.Horizontal.Scale)
	require.Equal(t, owonmodel.AcquisitionModeSample, state.Acquisition.Mode)
	require.Len(t, state.Channels, 1)
	require.Equal(t, 10.0, state.Channels[0].ProbeAttenuation)
	require.Equal(t, owonmodel.TriggerSourceChannel1, state.Trigger.Source)
	require.Equal(t, owonmodel.TriggerSlopeRising, state.Trigger.Slope)
	require.Equal(t, 1.52, state.Trigger.Level)
	require.Equal(t, "TRIG", state.Trigger.Status)
}

// TestInstrumentStateSeparatesHeaderFetchFromExposure verifies both request flags independently.
//
// Example: IncludeControls parses one header but does not expose its raw JSON unless requested.
func TestInstrumentStateSeparatesHeaderFetchFromExposure(t *testing.T) {
	t.Parallel()

	header := []byte(`{
		"TIMEBASE":{"SCALE":"20us","HOFFSET":0},
		"SAMPLE":{"TYPE":"SAMPle","DEPMEM":"4K"},
		"CHANNEL":[{"NAME":"CH1","DISPLAY":"ON","COUPLING":"DC","PROBE":"10X","SCALE":"1.00V","OFFSET":0}],
		"RUNSTATUS":"TRIG",
		"Trig":{"Mode":"SINGle","Type":"Edge","Items":{"Channel":"CH1","Level":"0V","Edge":"RISE","Coupling":"DC"},"Sweep":"AUTO"}
	}`)
	cases := []struct {
		Name              string
		IncludeHeader     bool
		IncludeControls   bool
		WantHeader        bool
		WantControls      bool
		WantHeaderCommand bool
	}{
		{Name: "neither"},
		{Name: "header", IncludeHeader: true, WantHeader: true, WantHeaderCommand: true},
		{Name: "controls", IncludeControls: true, WantControls: true, WantHeaderCommand: true},
		{Name: "both", IncludeHeader: true, IncludeControls: true, WantHeader: true, WantControls: true, WantHeaderCommand: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.Name,
			// verifyStateFlags checks header acquisition, exposure, and control decoding independently.
			//
			// Example: controls-only fetches HEAD but returns no ScreenHeaderJSON bytes.
			func(t *testing.T) {
				backend := &scriptedBackend{Responses: map[string][]byte{
					"*IDN?":                   []byte("OWON,HDS2202S,25061855,V2.6.0"),
					":DATA:WAVE:SCREEN:HEAD?": header,
				}}
				controller := newTestInstrument(t, backend)
				state, err := controller.State(context.Background(), &owonmodel.StateRequest{
					IncludeScreenHeader: testCase.IncludeHeader,
					IncludeControls:     testCase.IncludeControls,
				})
				require.NoError(t, err)
				require.Equal(t, testCase.WantHeader, len(state.ScreenHeaderJSON) != 0)
				require.Equal(t, testCase.WantControls, state.Horizontal != nil)
				require.Equal(t, testCase.WantHeaderCommand, slices.Contains(backend.Commands, ":DATA:WAVE:SCREEN:HEAD?"))
			})
	}
}

// newTestInstrument constructs a controller backed by one deterministic recorder.
//
// Example: control tests use it to assert the exact SCPI command sequence.
func newTestInstrument(
	t *testing.T,
	backend owonsession.Backend,
) *Controller {
	t.Helper()
	session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	controller, err := New(session, owonmodel.DeviceIdentity{})
	require.NoError(t, err)

	return controller
}
