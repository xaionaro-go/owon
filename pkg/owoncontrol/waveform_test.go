package owoncontrol

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// TestWaveformProvidesScreenTrace requires semantic display data alongside raw capture bytes.
//
// Example: signed byte -79 at channel offset -78 appears at Y=179 and ground Y=178.
func TestWaveformProvidesScreenTrace(t *testing.T) {
	header := []byte(`{"MODEL":"HDS2202S_LS","DATATYPE":"SCREEN","SAMPLE":{"FULLSCREEN":600,"DATALEN":600,"TYPE":"SAMPle","SLOWMOVE":-1},"CHANNEL":[{"NAME":"CH1","OFFSET":-78}]}`)
	data := make([]byte, 600)
	data[0], data[1] = 177, 12
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": header,
		":DATA:WAVE:SCREEN:CH1?":  data,
	}}
	waveform, err := newTestInstrument(t, backend).Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
	require.NoError(t, err)
	require.Equal(t, data, waveform.Data)
	require.Equal(t, header, waveform.ScreenHeaderJSON)
	require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?", ":DATA:WAVE:SCREEN:CH1?"}, backend.Commands)
	require.NotNil(t, waveform.ScreenTrace)
	require.Equal(t, []int{179, 179}, waveform.ScreenTrace.Y[:2])
	require.Equal(t, 178, waveform.ScreenTrace.GroundY)
	require.Empty(t, waveform.ScreenTraceUnavailableReason)
	require.Equal(t, "owon-raw-unverified", waveform.Encoding)
	require.Equal(t, &owonmodel.WaveformMetadata{SampleEncoding: "unverified", DataLengthBytes: 600}, waveform.Metadata)
}

// TestWaveformCaptureWindow encloses exactly the two host exchanges.
//
// Example: two virtual one-second exchanges retain their original start and completion instants.
func TestWaveformCaptureWindow(t *testing.T) {
	synctest.Test(t,
		// checkWindow separates the host I/O window from the unsupported decode outcome.
		//
		// Example: an unknown profile still retains both observation timestamps and its raw bytes.
		func(t *testing.T) {
			backend := &timedBackend{scriptedBackend: scriptedBackend{Responses: map[string][]byte{
				":DATA:WAVE:SCREEN:HEAD?": []byte(`{}`),
				":DATA:WAVE:SCREEN:CH1?":  {1, 2, 3},
			}}}
			controller := newTestInstrument(t, backend)
			start := time.Now()
			waveform, err := controller.Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
			require.NoError(t, err)
			require.Equal(t, start, waveform.CaptureStartedAt)
			require.Equal(t, start.Add(2*time.Second), waveform.CapturedAt)
			require.Len(t, backend.Commands, 2)
			require.Nil(t, waveform.ScreenTrace)
			require.NotEmpty(t, waveform.ScreenTraceUnavailableReason)
			require.Equal(t, []byte{1, 2, 3}, waveform.Data)
		})
}

// TestWaveformMalformedTraceIsNotSuppressed preserves decoder failures at the control boundary.
//
// Example: admitted DATALEN=600 with a short payload is an error, not an unsupported capture.
func TestWaveformMalformedTraceIsNotSuppressed(t *testing.T) {
	backend := &scriptedBackend{Responses: map[string][]byte{
		":DATA:WAVE:SCREEN:HEAD?": []byte(`{"MODEL":"HDS2202S_LS","DATATYPE":"SCREEN","SAMPLE":{"FULLSCREEN":600,"DATALEN":600,"TYPE":"SAMPle","SLOWMOVE":-1},"CHANNEL":[{"NAME":"CH1","OFFSET":0}]}`),
		":DATA:WAVE:SCREEN:CH1?":  {1},
	}}
	waveform, err := newTestInstrument(t, backend).Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
	require.Nil(t, waveform)
	var malformed *owonscpi.ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
	require.Contains(t, err.Error(), "channel 1")
	require.Len(t, backend.Commands, 2)
}

// TestWaveformUnknownNumericProfileRetainsRaw separates preflight shape checks from profile admission.
//
// Example: another model's numeric DATALEN cannot be interpreted as the known 600-slot layout.
func TestWaveformUnknownNumericProfileRetainsRaw(t *testing.T) {
	for _, length := range []string{"999999999999999999999999999", "600.25", "-1"} {
		header := []byte(`{"MODEL":"OTHER","SAMPLE":{"DATALEN":` + length + `}}`)
		backend := &scriptedBackend{Responses: map[string][]byte{
			":DATA:WAVE:SCREEN:HEAD?": header,
			":DATA:WAVE:SCREEN:CH1?":  {1, 2, 3, 4, 5, 6, 7},
		}}
		waveform, err := newTestInstrument(t, backend).Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
		require.NoError(t, err, length)
		require.Equal(t, header, waveform.ScreenHeaderJSON)
		require.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7}, waveform.Data)
		require.Nil(t, waveform.ScreenTrace)
		require.Contains(t, waveform.ScreenTraceUnavailableReason, "MODEL")
		require.Len(t, backend.Commands, 2)
	}
}

// TestWaveformRejectsMalformedProfileBeforeData prevents invalid header shapes from starting data I/O.
//
// Example: a null root or a numeric MODEL returns a dialect error after HEAD only.
func TestWaveformRejectsMalformedProfileBeforeData(t *testing.T) {
	for _, header := range []string{`null`, `[]`, `{`, `{"MODEL":4}`, `{"SAMPLE":null}`, `{"SAMPLE":{"DATALEN":"600"}}`, `{"CHANNEL":[{"NAME":"CH1","OFFSET":null}]}`} {
		backend := &scriptedBackend{Responses: map[string][]byte{":DATA:WAVE:SCREEN:HEAD?": []byte(header)}}
		waveform, err := newTestInstrument(t, backend).Waveform(t.Context(), &owonmodel.WaveformRequest{Channel: owonmodel.Channel1, Screen: true})
		require.Nil(t, waveform)
		var malformed *owonscpi.ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, header)
		require.Equal(t, []string{":DATA:WAVE:SCREEN:HEAD?"}, backend.Commands, header)
	}
}
