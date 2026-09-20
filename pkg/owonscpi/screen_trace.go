package owonscpi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// Screen geometry constants describe the sole verified vendor display profile.
//
// Example: 25 vertical counts per division across eight divisions gives height 200.
const (
	// screenTraceWidth is the proven horizontal display extent.
	//
	// Example: each capture yields 600 display slots.
	screenTraceWidth = 600
	// screenTraceHeight is the vendor's eight divisions of 25 Y counts.
	//
	// Example: Y=200 is the bottom display edge.
	screenTraceHeight = 200
	// screenTraceCenterY is the zero-offset display ground.
	//
	// Example: OFFSET=0 gives ground Y=100.
	screenTraceCenterY = screenTraceHeight / 2
	// screenTraceMinimumOffset keeps the derived ground at or below signed32 maximum.
	//
	// Example: this endpoint maps exactly to ground Y=2147483647.
	screenTraceMinimumOffset int64 = -2147483547
	// screenTraceMaximumOffset keeps the derived ground at or above signed32 minimum.
	//
	// Example: this endpoint maps exactly to ground Y=-2147483648.
	screenTraceMaximumOffset int64 = 2147483748
)

// ErrUnsupportedScreenProfile identifies a valid but unproved display interpretation.
//
// Example: a PEAK capture remains available as raw bytes without a screen trace.
type ErrUnsupportedScreenProfile struct{ Reason string }

// Error describes the profile feature preventing display interpretation.
//
// Example: a different model retains an explicit unsupported reason.
func (err *ErrUnsupportedScreenProfile) Error() string {
	if err == nil || err.Reason == "" {
		return "unsupported OWON screen profile"
	}
	return "unsupported OWON screen profile: " + err.Reason
}

// Unwrap identifies this as a leaf capability classification.
//
// Example: errors.As distinguishes unsupported profiles from malformed responses.
func (*ErrUnsupportedScreenProfile) Unwrap() error { return nil }

// screenTraceField preserves absence while rejecting explicitly null profile fields.
//
// Example: missing OFFSET is unsupported, whereas OFFSET:null is malformed.
type screenTraceField[T any] struct {
	Value   T
	Present bool
}

// UnmarshalJSON validates a present profile field without conflating null and absence.
//
// Example: string fields reject numeric tokens through the standard JSON decoder.
func (field *screenTraceField[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return &ErrMalformedResponse{Reason: "screen profile field is null"}
	}
	if err := json.Unmarshal(data, &field.Value); err != nil {
		return err
	}
	field.Present = true
	return nil
}

// screenTraceNumber retains numeric spelling without imposing known-profile size limits.
//
// Example: a future model's arbitrarily large DATALEN remains unsupported, not corrupt.
type screenTraceNumber string

// UnmarshalJSON accepts JSON numbers without float rounding or quoted-number coercion.
//
// Example: DATALEN:600 is distinct from the unproved numeric value 600.000000000000001.
func (number *screenTraceNumber) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || (data[0] != '-' && (data[0] < '0' || data[0] > '9')) {
		return &ErrMalformedResponse{Reason: "screen profile requires a numeric token"}
	}
	var value json.Number
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*number = screenTraceNumber(value)
	return nil
}

// screenTraceProfile holds only fields relevant to the proven display interpretation.
//
// Example: timebase and physical scaling stay in the untouched raw header.
type screenTraceProfile struct {
	Model    screenTraceField[string]                                 `json:"MODEL"`
	DataType screenTraceField[string]                                 `json:"DATATYPE"`
	Sample   screenTraceField[screenTraceSample]                      `json:"SAMPLE"`
	Channels screenTraceField[[]screenTraceField[screenTraceChannel]] `json:"CHANNEL"`
}

// screenTraceSample holds geometry and acquisition-mode admission fields.
//
// Example: nonzero SCREENOFFSET is unproved even when all other fields match.
type screenTraceSample struct {
	Fullscreen   screenTraceField[screenTraceNumber] `json:"FULLSCREEN"`
	DataLength   screenTraceField[screenTraceNumber] `json:"DATALEN"`
	Type         screenTraceField[string]            `json:"TYPE"`
	SlowMove     screenTraceField[screenTraceNumber] `json:"SLOWMOVE"`
	ScreenOffset screenTraceField[screenTraceNumber] `json:"SCREENOFFSET"`
}

// screenTraceChannel pairs a channel name with an exact, wide integer offset.
//
// Example: positive OFFSET can exceed int32 while its derived ground still fits int32.
type screenTraceChannel struct {
	Name   screenTraceField[string] `json:"NAME"`
	Offset screenTraceField[int64]  `json:"OFFSET"`
}

// parseScreenTraceProfile validates the JSON shape and field types without admitting a profile.
//
// Example: an unknown numeric DATALEN remains available for a raw-only capture.
func parseScreenTraceProfile(header []byte) (*screenTraceProfile, error) {
	var profile screenTraceProfile
	header = bytes.TrimSpace(header)
	if len(header) == 0 || header[0] != '{' {
		return nil, &ErrMalformedResponse{Reason: "screen profile root must be a JSON object"}
	}
	if err := json.Unmarshal(header, &profile); err != nil {
		return nil, &ErrMalformedResponse{Reason: "decode screen profile JSON", Cause: err}
	}
	return &profile, nil
}

// ValidateScreenTraceHeader checks header syntax and field types before payload I/O.
// It does not decide whether the display profile is supported.
//
// Example: a missing model passes preflight while a null root fails before the data query.
func ValidateScreenTraceHeader(header []byte) error {
	_, err := parseScreenTraceProfile(header)
	return err
}

// DecodeScreenTrace interprets the verified HDS2202S screen profile without modifying inputs.
// The vendor draws signed even bytes twice; odd bytes remain uninterpreted raw data.
//
// Example: bytes {177, 12} produce two Y=179 display slots, not two ADC samples.
func DecodeScreenTrace(
	channel owonmodel.Channel,
	header []byte,
	data []byte,
) (*owonmodel.ScreenTrace, error) {
	name, err := channelName(channel)
	if err != nil {
		return nil, err
	}
	profile, err := parseScreenTraceProfile(header)
	if err != nil {
		return nil, err
	}
	sample := profile.Sample.Value
	for _, field := range []struct {
		Name            string
		Present         bool
		Value, Expected string
	}{
		{"MODEL", profile.Model.Present, profile.Model.Value, "HDS2202S_LS"},
		{"DATATYPE", profile.DataType.Present, profile.DataType.Value, "SCREEN"},
		{"SAMPLE.FULLSCREEN", sample.Fullscreen.Present, string(sample.Fullscreen.Value), "600"},
		{"SAMPLE.DATALEN", sample.DataLength.Present, string(sample.DataLength.Value), "600"},
		{"SAMPLE.TYPE", sample.Type.Present, sample.Type.Value, "SAMPle"},
		{"SAMPLE.SLOWMOVE", sample.SlowMove.Present, string(sample.SlowMove.Value), "-1"},
	} {
		if !field.Present {
			return nil, &ErrUnsupportedScreenProfile{Reason: field.Name + " is absent"}
		}
		if field.Value != field.Expected {
			return nil, &ErrUnsupportedScreenProfile{Reason: fmt.Sprintf("%s=%q is unproved", field.Name, field.Value)}
		}
	}
	if sample.ScreenOffset.Present && sample.ScreenOffset.Value != "0" {
		return nil, &ErrUnsupportedScreenProfile{Reason: "nonzero SAMPLE.SCREENOFFSET is unproved"}
	}
	var selected *screenTraceChannel
	for _, candidate := range profile.Channels.Value {
		if candidate.Value.Name.Value != name {
			continue
		}
		if selected != nil {
			return nil, &ErrMalformedResponse{Reason: "duplicate screen profile channel " + name}
		}
		selected = &candidate.Value
	}
	if selected == nil || !selected.Offset.Present {
		return nil, &ErrUnsupportedScreenProfile{Reason: name + " OFFSET is absent"}
	}
	offset := selected.Offset.Value
	if offset < screenTraceMinimumOffset || offset > screenTraceMaximumOffset {
		return nil, &ErrMalformedResponse{Reason: name + " OFFSET produces a ground coordinate outside signed 32-bit range"}
	}
	if len(data) != screenTraceWidth {
		return nil, &ErrMalformedResponse{Reason: fmt.Sprintf("screen DATALEN=%d but received %d bytes", screenTraceWidth, len(data))}
	}
	trace := &owonmodel.ScreenTrace{
		Profile: "owon-hds2202s-screen", Width: screenTraceWidth, Height: screenTraceHeight,
		HorizontalDivisions: 12, VerticalDivisions: 8,
		Y: make([]int, screenTraceWidth), GroundY: int(int64(screenTraceCenterY) - offset),
	}
	for i := range trace.Y {
		trace.Y[i] = screenTraceCenterY - int(int8(data[i & ^1]))
	}
	return trace, nil
}
