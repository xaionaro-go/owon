package owonmodel

import (
	"time"
)

// WaveformRequest selects the device data to acquire.
//
// Example: Channel1 with Screen=true requests the current screen capture.
type WaveformRequest struct {
	Channel Channel
	Screen  bool
}

// Validate checks waveform selection independently of supported capture modes.
//
// Example: a valid non-screen CH1 request may subsequently be unsupported by a dialect.
func (request *WaveformRequest) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "waveform request is nil"}
	}
	return ValidateChannel(request.Channel)
}

// WaveformMetadata describes known sample properties without guessing an undocumented encoding.
//
// Example: raw byte length can be known while sample count and scaling remain absent.
type WaveformMetadata struct {
	SampleCount       *uint64
	SampleRateHz      float64
	SampleEncoding    string
	XOriginSeconds    *float64
	XIncrementSeconds *float64
	YOriginVolts      *float64
	YIncrementVolts   *float64
	DataLengthBytes   uint64
}

// ScreenTrace describes vendor display coordinates, not calibrated ADC samples.
// Coordinates start at the top left; each Y entry's index is its X coordinate.
// Off-screen coordinates remain intact for the renderer to clip.
//
// Example: an HDS2202S screen has 600 by 200 coordinates over 12 by 8 divisions.
type ScreenTrace struct {
	Profile             string
	Width               int
	Height              int
	HorizontalDivisions int
	VerticalDivisions   int
	Y                   []int
	GroundY             int
}

// Waveform contains one channel capture, its raw header, and independently observed metadata.
//
// Example: Data retains the exact uninterpreted device bytes.
type Waveform struct {
	Channel                      Channel
	Data                         []byte
	Encoding                     string
	ScreenHeaderJSON             []byte
	CapturedAt                   time.Time
	Metadata                     *WaveformMetadata
	ScreenTrace                  *ScreenTrace
	ScreenTraceUnavailableReason string
	// CaptureStartedAt and CapturedAt bound host I/O, not device acquisition time.
	CaptureStartedAt time.Time
}
