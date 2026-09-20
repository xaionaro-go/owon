package owonscpi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// screenTraceHeader is the admitted geometry with independently located channel metadata.
//
// Example: CH2 precedes CH1 to prevent positional channel selection.
const screenTraceHeader = `{"MODEL":"HDS2202S_LS","DATATYPE":"SCREEN","SAMPLE":{"FULLSCREEN":600,"DATALEN":600,"TYPE":"SAMPle","SLOWMOVE":-1},"CHANNEL":[{"NAME":"CH2","OFFSET":-49},{"NAME":"CH1","OFFSET":-78}]}`

// TestDecodeScreenTraceGeometry rejects unsigned, int16, and all-byte interpretations.
//
// Example: -128 and 127 map outside the viewport without clipping.
func TestDecodeScreenTraceGeometry(t *testing.T) {
	data := bytes.Repeat([]byte{177, 12, 178, 255, 180, 127, 183, 128, 128, 77, 127, 88}, 50)
	original := bytes.Clone(data)
	header := []byte(screenTraceHeader)
	trace, err := DecodeScreenTrace(owonmodel.Channel1, header, data)
	require.NoError(t, err)
	require.Equal(t, &owonmodel.ScreenTrace{Profile: "owon-hds2202s-screen", Width: 600, Height: 200, HorizontalDivisions: 12, VerticalDivisions: 8, Y: trace.Y, GroundY: 178}, trace)
	require.Len(t, trace.Y, 600)
	require.Equal(t, []int{179, 179, 178, 178, 176, 176, 173, 173, 228, 228, -27, -27}, trace.Y[:12])
	require.Equal(t, original, data)
	require.Equal(t, screenTraceHeader, string(header))
	trace.Y[0] = 0
	require.Equal(t, original, data)
	second, err := DecodeScreenTrace(owonmodel.Channel2, header, data)
	require.NoError(t, err)
	require.Equal(t, 149, second.GroundY)
}

// TestDecodeScreenTraceRecordedCapture checks the first-party physical capture against golden ordinates.
//
// Example: 292 differing odd bytes remain untouched while the vendor repeats the even ordinates.
func TestDecodeScreenTraceRecordedCapture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/hds2202s-screen.json")
	require.NoError(t, err)
	var capture struct {
		Data   []byte `json:"data"`
		Header []byte `json:"screen_header_json"`
	}
	require.NoError(t, json.Unmarshal(fixture, &capture))
	original := bytes.Clone(capture.Data)
	trace, err := DecodeScreenTrace(owonmodel.Channel1, capture.Header, capture.Data)
	require.NoError(t, err)
	require.Equal(t, []int{179, 179, 178, 178, 176, 176, 173, 173, 177, 177, 176, 176}, trace.Y[:12])
	require.Equal(t, 178, trace.GroundY)
	differing := 0
	for i := 0; i < len(capture.Data); i += 2 {
		require.Equal(t, trace.Y[i], trace.Y[i+1])
		require.GreaterOrEqual(t, trace.Y[i], 173)
		require.LessOrEqual(t, trace.Y[i], 179)
		if capture.Data[i] != capture.Data[i+1] {
			differing++
		}
	}
	require.Equal(t, 292, differing)
	require.Equal(t, original, capture.Data)
}

// TestDecodeScreenTraceAdmission distinguishes unproved profiles from corrupt payloads.
//
// Example: another model's DATALEN has no established relationship to this payload.
func TestDecodeScreenTraceAdmission(t *testing.T) {
	for _, test := range []struct {
		Name      string
		Old       string
		New       string
		Length    int
		Malformed bool
	}{
		{Name: "unknown model differing length", Old: "HDS2202S_LS", New: "OTHER", Length: 7},
		{Name: "unknown datatype", Old: "SCREEN", New: "MEMORY", Length: 600},
		{Name: "unknown geometry", Old: `"FULLSCREEN":600`, New: `"FULLSCREEN":1200`, Length: 600},
		{Name: "unknown datalen", Old: `"DATALEN":600`, New: `"DATALEN":1200`, Length: 7},
		{Name: "huge datalen", Old: `"DATALEN":600`, New: `"DATALEN":999999999999999999999999999`, Length: 7},
		{Name: "fractional datalen", Old: `"DATALEN":600`, New: `"DATALEN":600.000000000000001`, Length: 7},
		{Name: "peak", Old: "SAMPle", New: "PEAK", Length: 600},
		{Name: "roll", Old: `"SLOWMOVE":-1`, New: `"SLOWMOVE":0`, Length: 600},
		{Name: "missing model", Old: `"MODEL":"HDS2202S_LS",`, New: "", Length: 600},
		{Name: "missing sample", Old: `"SAMPLE":`, New: `"IGNORED":`, Length: 600},
		{Name: "missing channel", Old: "CH1", New: "CH3", Length: 600},
		{Name: "missing offset", Old: `,"OFFSET":-78`, New: "", Length: 600},
		{Name: "duplicate channel", Old: "CH2", New: "CH1", Length: 600, Malformed: true},
		{Name: "length mismatch", Length: 599, Malformed: true},
		{Name: "screen offset nonzero", Old: `"SLOWMOVE":-1`, New: `"SLOWMOVE":-1,"SCREENOFFSET":1`, Length: 600},
		{Name: "screen offset type", Old: `"SLOWMOVE":-1`, New: `"SLOWMOVE":-1,"SCREENOFFSET":"0"`, Length: 600, Malformed: true},
		{Name: "null model", Old: `"HDS2202S_LS"`, New: "null", Length: 600, Malformed: true},
		{Name: "model type", Old: `"HDS2202S_LS"`, New: "1", Length: 600, Malformed: true},
		{Name: "sample type", Old: `"SAMPLE":{`, New: `"SAMPLE":[`, Length: 600, Malformed: true},
		{Name: "quoted number", Old: `"DATALEN":600`, New: `"DATALEN":"600"`, Length: 600, Malformed: true},
		{Name: "null number", Old: `"DATALEN":600`, New: `"DATALEN":null`, Length: 600, Malformed: true},
		{Name: "fractional offset", Old: `"OFFSET":-78`, New: `"OFFSET":-78.5`, Length: 600, Malformed: true},
		{Name: "overflow offset", Old: `"OFFSET":-78`, New: `"OFFSET":9223372036854775808`, Length: 600, Malformed: true},
	} {
		t.Run(test.Name,
			// checkAdmission asserts exactly one classification and no partial trace.
			//
			// Example: malformed OFFSET must never become raw-only unsupported success.
			func(t *testing.T) {
				header := screenTraceHeader
				if test.Old != "" {
					header = strings.Replace(header, test.Old, test.New, 1)
				}
				trace, err := DecodeScreenTrace(owonmodel.Channel1, []byte(header), make([]byte, test.Length))
				require.Nil(t, trace)
				var malformed *ErrMalformedResponse
				var unsupported *ErrUnsupportedScreenProfile
				var framing *owonprotocol.ErrMalformedResponse
				if test.Malformed {
					require.ErrorAs(t, err, &malformed)
					require.NotErrorAs(t, err, &unsupported)
				} else {
					require.ErrorAs(t, err, &unsupported)
					require.NotErrorAs(t, err, &malformed)
					require.NotEmpty(t, unsupported.Reason)
				}
				require.NotErrorAs(t, err, &framing)
			})
	}
	for _, header := range []string{"null", "[]", `"text"`, "true", "{", `{"CHANNEL":[null]}`, `{"SAMPLE":null}`} {
		trace, err := DecodeScreenTrace(owonmodel.Channel1, []byte(header), make([]byte, 600))
		require.Nil(t, trace)
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, header)
	}
}

// TestDecodeScreenTraceOffsetBounds requires subtraction before narrowing to signed wire coordinates.
//
// Example: the largest admitted positive OFFSET maps exactly to the minimum signed 32-bit ground.
func TestDecodeScreenTraceOffsetBounds(t *testing.T) {
	for _, offset := range []int64{-2147483548, -2147483547, -1000, 0, 1000, 2147483748, 2147483749} {
		header := strings.Replace(screenTraceHeader, `"OFFSET":-78`, fmt.Sprintf(`"OFFSET":%d`, offset), 1)
		trace, err := DecodeScreenTrace(owonmodel.Channel1, []byte(header), make([]byte, 600))
		if offset < -2147483547 || offset > 2147483748 {
			var malformed *ErrMalformedResponse
			require.ErrorAs(t, err, &malformed)
			require.Nil(t, trace)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, int(100-offset), trace.GroundY)
	}
	header := strings.Replace(screenTraceHeader, `"SLOWMOVE":-1`, `"SLOWMOVE":-1,"SCREENOFFSET":0`, 1)
	trace, err := DecodeScreenTrace(owonmodel.Channel1, []byte(header), make([]byte, 600))
	require.NoError(t, err)
	require.NotNil(t, trace)
}

// TestValidateScreenTraceHeaderAllowsUnknownProfiles exercises preflight without payload admission.
//
// Example: a syntactically valid huge numeric DATALEN is allowed before raw capture.
func TestValidateScreenTraceHeaderAllowsUnknownProfiles(t *testing.T) {
	for _, header := range []string{screenTraceHeader, `{}`, `{"MODEL":"OTHER","SAMPLE":{"DATALEN":1e1000}}`} {
		require.NoError(t, ValidateScreenTraceHeader([]byte(header)))
	}
	for _, header := range []string{`null`, `[]`, `{"SAMPLE":{"SCREENOFFSET":false}}`, `{"CHANNEL":[{"OFFSET":1.0}]}`} {
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, ValidateScreenTraceHeader([]byte(header)), &malformed)
	}
	trace, err := DecodeScreenTrace(owonmodel.Channel(99), []byte(screenTraceHeader), make([]byte, 600))
	require.Nil(t, trace)
	var invalid *ErrInvalidSetting
	require.ErrorAs(t, err, &invalid)
}

// TestUnsupportedScreenProfileDiagnostic preserves typed matching through contextual errors.
//
// Example: a controller can catch only the unsupported classification and retain its reason.
func TestUnsupportedScreenProfileDiagnostic(t *testing.T) {
	var nilError *ErrUnsupportedScreenProfile
	require.Equal(t, "unsupported OWON screen profile", nilError.Error())
	require.Nil(t, nilError.Unwrap())
	require.Equal(t, "unsupported OWON screen profile", (&ErrUnsupportedScreenProfile{}).Error())
	err := &ErrUnsupportedScreenProfile{Reason: "MODEL is absent"}
	wrapped := fmt.Errorf("capture: %w", err)
	require.Equal(t, "capture: unsupported OWON screen profile: MODEL is absent", wrapped.Error())
	var matched *ErrUnsupportedScreenProfile
	require.True(t, errors.As(wrapped, &matched))
	require.Same(t, err, matched)
	require.Nil(t, errors.Unwrap(err))
}
