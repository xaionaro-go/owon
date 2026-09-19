package owonserver

import (
	"context"
	"reflect"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// Instrument is the typed operation surface consumed by the RPC service.
//
// Example: a local controller or a remote implementation can serve the same RPC handlers.
type Instrument interface {
	// DeviceInfo queries the connected instrument identity and capabilities.
	//
	// Example: callers can verify the current model and serial.
	DeviceInfo(ctx context.Context) (*owonmodel.DeviceInfo, error)
	// State captures identity, selected measurements, and requested screen/control state.
	//
	// Example: a subscription requests CH1 frequency and omits screen bytes.
	State(
		ctx context.Context,
		request *owonmodel.StateRequest,
	) (*owonmodel.StateSnapshot, error)
	// Execute sends one validated command using its explicit response framing.
	//
	// Example: an ASCII identity query returns the device response bytes.
	Execute(
		ctx context.Context,
		command owonprotocol.Command,
	) ([]byte, error)
	// Run starts continuous acquisition.
	//
	// Example: resuming a stopped capture updates subsequent waveform observations.
	Run(ctx context.Context) error
	// Stop stops acquisition.
	//
	// Example: a client freezes the displayed capture.
	Stop(ctx context.Context) error
	// Single arms one single acquisition.
	//
	// Example: a client waits for the next trigger without continuous acquisition.
	Single(ctx context.Context) error
	// DMMMeasurement queries the current multimeter observation.
	//
	// Example: a voltage reading retains its capture time and raw text.
	DMMMeasurement(ctx context.Context) (*owonmodel.DMMMeasurement, error)
	// Waveform captures the requested channel's raw waveform and verified metadata.
	//
	// Example: screen capture retains header bytes without inventing sample scaling.
	Waveform(
		ctx context.Context,
		request *owonmodel.WaveformRequest,
	) (*owonmodel.Waveform, error)
	// SetChannel applies present channel fields after validating the complete patch.
	//
	// Example: an explicit false display value disables the selected channel.
	SetChannel(
		ctx context.Context,
		request *owonmodel.ChannelPatch,
	) error
	// SetAcquisition applies present acquisition settings after validating the complete patch.
	//
	// Example: a sample-mode patch leaves omitted memory depth unchanged.
	SetAcquisition(
		ctx context.Context,
		request *owonmodel.AcquisitionPatch,
	) error
	// SetHorizontal applies present timebase settings without losing integer offset precision.
	//
	// Example: an explicit zero offset resets the horizontal position.
	SetHorizontal(
		ctx context.Context,
		request *owonmodel.HorizontalPatch,
	) error
	// SetTrigger applies present trigger settings after validating the complete patch.
	//
	// Example: a rising-edge patch leaves omitted level and source unchanged.
	SetTrigger(
		ctx context.Context,
		request *owonmodel.TriggerPatch,
	) error
	// SetMeasurement applies supported measurement display settings.
	//
	// Example: an explicit false display value hides measurements.
	SetMeasurement(
		ctx context.Context,
		request *owonmodel.MeasurementPatch,
	) error
	// SetGenerator applies present generator settings after validating the complete patch.
	//
	// Example: a waveform and frequency patch is validated before either write.
	SetGenerator(
		ctx context.Context,
		request *owonmodel.GeneratorPatch,
	) error
	// SetDMM applies present multimeter settings after validating the complete patch.
	//
	// Example: a voltage function with DC current type selects the intended measurement.
	SetDMM(
		ctx context.Context,
		request *owonmodel.DMMPatch,
	) error
}

// isNilInstrument rejects typed nil implementations before storing the interface.
//
// Example: a nil controller pointer cannot create a seemingly available service.
func isNilInstrument(instrument Instrument) bool {
	if instrument == nil {
		return true
	}
	value := reflect.ValueOf(instrument)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
