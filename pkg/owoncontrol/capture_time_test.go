package owoncontrol

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// timedBackend models one second of opaque device response latency on synctest's virtual clock.
//
// Example: identity, measurement and header exchanges complete at independently observable times.
type timedBackend struct{ scriptedBackend }

// Exchange advances virtual device latency and then returns a scripted payload.
//
// Example: this delay models hardware response time, not synchronization between test goroutines.
func (backend *timedBackend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	time.Sleep(time.Second)
	return backend.scriptedBackend.Exchange(ctx, command)
}

// TestObservationCaptureTimeBelongsToController specifies each observation's capture point.
//
// Example: state timestamps after identity, whereas waveform timestamps after its data reply.
func TestObservationCaptureTimeBelongsToController(t *testing.T) {
	synctest.Test(t,
		// checkCapturePoints distinguishes observation time from request start and final snapshot completion.
		//
		// Example: a three-exchange state retains the identity-completion time, not the later header time.
		func(t *testing.T) {
			backend := &timedBackend{scriptedBackend: scriptedBackend{Responses: map[string][]byte{
				"*IDN?":                       []byte("OWON,HDS2202S,serial,firmware"),
				":MEASUREMENT:CH1:FREQUENCY?": []byte("42"),
				":DATA:WAVE:SCREEN:HEAD?":     []byte(`{}`),
				":DATA:WAVE:SCREEN:CH1?":      []byte{1, 2},
				":DMM:MEAS?":                  []byte("0 V"),
			}}}
			controller := newTestInstrument(t, backend)
			start := time.Now()
			state, err := controller.State(t.Context(), &owonmodel.StateRequest{IncludeScreenHeader: true, Measurements: []*owonmodel.MeasurementSelector{{Channel: owonmodel.Channel1, Kind: owonmodel.MeasurementKindFrequency}}})
			require.NoError(t, err)
			require.Equal(t, start.Add(time.Second), state.CapturedAt)
			require.Equal(t, start.Add(3*time.Second), time.Now())
			start = time.Now()
			waveform, err := controller.Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
			require.NoError(t, err)
			require.Equal(t, start.Add(2*time.Second), waveform.CapturedAt)
			start = time.Now()
			measurement, err := controller.DMMMeasurement(t.Context())
			require.NoError(t, err)
			require.Equal(t, start.Add(time.Second), measurement.CapturedAt)
		})
}
