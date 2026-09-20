package owoncontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestInstrumentNilContextDoesNotPanicOrAdmitWork verifies tracing preserves input rejection.
//
// Example: a valid measurement with no context fails before USB, and later valid work still succeeds.
func TestInstrumentNilContextDoesNotPanicOrAdmitWork(t *testing.T) {
	backend := &scriptedBackend{Responses: map[string][]byte{"*IDN?": []byte("OWON,HDS2202S,serial,firmware")}}
	controller := newTestInstrument(t, backend)
	_, err := controller.DeviceInfo(nil)
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	_, err = controller.Execute(nil, owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII})
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	requireErrorType[*owonsession.ErrUnavailable](t, controller.Run(nil))
	requireErrorType[*owonsession.ErrUnavailable](t, controller.Stop(nil))
	requireErrorType[*owonsession.ErrUnavailable](t, controller.Single(nil))
	_, err = controller.DMMMeasurement(nil)
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	_, err = controller.Measure(nil, &owonmodel.MeasurementSelector{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKindFrequency})
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	_, err = controller.State(nil, new(owonmodel.StateRequest))
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	_, err = controller.Waveform(nil, &owonmodel.WaveformRequest{Screen: true, Channel: owonmodel.Channel1})
	requireErrorType[*owonsession.ErrUnavailable](t, err)
	require.Empty(t, backend.Commands)
	info, err := controller.DeviceInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, owonmodel.SerialNumber("serial"), info.Serial)
	require.Equal(t, []string{"*IDN?"}, backend.Commands)
	require.Zero(t, backend.ReopenCalls)
}

// TestInstrumentUnavailablePaths verifies uninitialized resources fail consistently without panicking.
//
// Example: a nil controller cannot manufacture a reading or begin a mutation.
func TestInstrumentUnavailablePaths(t *testing.T) {
	t.Parallel()
	for _, controller := range []*Controller{nil, {}} {
		_, err := controller.DeviceInfo(t.Context())
		requireErrorType[*ErrUnavailable](t, err)
		_, err = controller.Execute(t.Context(), owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone})
		requireErrorType[*ErrUnavailable](t, err)
		requireErrorType[*ErrUnavailable](t, controller.Run(t.Context()))
		_, err = controller.DMMMeasurement(t.Context())
		requireErrorType[*ErrUnavailable](t, err)
		_, err = controller.Measure(t.Context(), &owonmodel.MeasurementSelector{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKindFrequency})
		requireErrorType[*ErrUnavailable](t, err)
		_, err = controller.State(t.Context(), new(owonmodel.StateRequest))
		requireErrorType[*ErrUnavailable](t, err)
		_, err = controller.Waveform(t.Context(), &owonmodel.WaveformRequest{Screen: true, Channel: owonmodel.Channel1})
		requireErrorType[*ErrUnavailable](t, err)
	}
	_, err := New(nil, owonmodel.DeviceIdentity{})
	requireErrorType[*ErrInvalidConfig](t, err)
}

// TestInstrumentCanceledWorkDoesNotReachBackend verifies cancellation precedes each transaction boundary.
//
// Example: a canceled snapshot cannot emit even its identity query.
func TestInstrumentCanceledWorkDoesNotReachBackend(t *testing.T) {
	t.Parallel()
	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, controller.Run(ctx), context.Canceled)
	_, err := controller.State(ctx, new(owonmodel.StateRequest))
	require.ErrorIs(t, err, context.Canceled)
	_, err = controller.Waveform(ctx, &owonmodel.WaveformRequest{Screen: true, Channel: owonmodel.Channel1})
	require.ErrorIs(t, err, context.Canceled)
	scale := "100mV"
	require.ErrorIs(t, controller.SetChannel(ctx, &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Scale: &scale}), context.Canceled)
	require.Empty(t, backend.Commands)
}

// TestInstrumentStopsSnapshotAtFirstFailure verifies no partial or invented state escapes a failed read.
//
// Example: a header read failure returns no snapshot and never queries a waveform payload.
func TestInstrumentStopsSnapshotAtFirstFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("USB failed")
	for _, header := range []string{"bad JSON", `{"SAMPLE":{"TYPE":"bad"}}`} {
		backend := &scriptedBackend{Responses: map[string][]byte{"*IDN?": []byte("OWON,HDS2202S,serial,firmware"), ":DATA:WAVE:SCREEN:HEAD?": []byte(header), ":DMM:RANGE?": []byte("mV")}}
		state, err := newTestInstrument(t, backend).State(t.Context(), &owonmodel.StateRequest{IncludeControls: true})
		require.Nil(t, state)
		requireErrorType[*owonscpi.ErrMalformedResponse](t, err)
	}
	backend := &scriptedBackend{Responses: map[string][]byte{"*IDN?": []byte("OWON,HDS2202S,serial,firmware")}, ExchangeErrors: []error{nil, cause}}
	state, err := newTestInstrument(t, backend).State(t.Context(), &owonmodel.StateRequest{IncludeScreenHeader: true})
	require.Nil(t, state)
	require.ErrorIs(t, err, cause)
	require.Equal(t, []string{"*IDN?", ":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands)
	backend = &scriptedBackend{Responses: map[string][]byte{":DATA:WAVE:SCREEN:HEAD?": []byte(`{}`)}, ExchangeErrors: []error{nil, cause}}
	waveform, err := newTestInstrument(t, backend).Waveform(t.Context(), &owonmodel.WaveformRequest{Screen: true, Channel: owonmodel.Channel1})
	require.Nil(t, waveform)
	require.ErrorIs(t, err, cause)
	require.Len(t, backend.Commands, 2)
}
