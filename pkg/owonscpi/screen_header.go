package owonscpi

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// ScreenHeader is the verified subset of the length-prefixed screen-header JSON.
//
// Example: firmware-specific fields remain ignored while known control metadata is decoded.
type ScreenHeader struct {
	Timebase screenHeaderTimebase  `json:"TIMEBASE"`
	Sample   screenHeaderSample    `json:"SAMPLE"`
	Channels []screenHeaderChannel `json:"CHANNEL"`
	Status   string                `json:"RUNSTATUS"`
	Trigger  screenHeaderTrigger   `json:"Trig"`
}

// screenHeaderTimebase is the observed horizontal-control object.
//
// Example: SCALE `20us` and HOFFSET zero describe the current horizontal view.
type screenHeaderTimebase struct {
	Scale  string   `json:"SCALE"`
	Offset *float64 `json:"HOFFSET"`
}

// screenHeaderSample is the observed acquisition-control object.
//
// Example: TYPE `SAMPle`, DEPMEM `4K`, and DATALEN 600 describe one capture.
type screenHeaderSample struct {
	Type        string `json:"TYPE"`
	MemoryDepth string `json:"DEPMEM"`
	DataLength  uint64 `json:"DATALEN"`
}

// screenHeaderChannel is the observed per-channel control object.
//
// Example: NAME `CH1` and PROBE `10X` identify one enabled ten-times probe.
type screenHeaderChannel struct {
	Name     string   `json:"NAME"`
	Display  string   `json:"DISPLAY"`
	Coupling string   `json:"COUPLING"`
	Probe    string   `json:"PROBE"`
	Scale    string   `json:"SCALE"`
	Offset   *float64 `json:"OFFSET"`
}

// screenHeaderTrigger is the observed edge-trigger control object.
//
// Example: the nested Items object carries channel, level, slope, and coupling.
type screenHeaderTrigger struct {
	Mode  string                   `json:"Mode"`
	Type  string                   `json:"Type"`
	Items screenHeaderTriggerItems `json:"Items"`
	Sweep string                   `json:"Sweep"`
}

// screenHeaderTriggerItems is the observed trigger-items object.
//
// Example: Level `1.52V` is converted to a numeric voltage.
type screenHeaderTriggerItems struct {
	Channel  string `json:"Channel"`
	Level    string `json:"Level"`
	Edge     string `json:"Edge"`
	Coupling string `json:"Coupling"`
	Sweep    string `json:"Sweep"`
}

// ParseScreenHeader decodes the verified JSON envelope without inferring sample encoding.
//
// Example: malformed JSON returns an error before a waveform data query is issued.
func ParseScreenHeader(data []byte) (*ScreenHeader, error) {
	var header ScreenHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("parse screen header JSON: %w", &ErrMalformedResponse{Reason: "JSON is malformed", Cause: err})
	}

	return &header, nil
}

// Controls interprets the screen header as one coherent control observation.
//
// Example: an observed CH1 object becomes a ChannelState rather than an inferred query result.
func (header *ScreenHeader) Controls() (*Controls, error) {
	if header == nil {
		return nil, &ErrMalformedResponse{Reason: "decode screen controls: nil header"}
	}
	mode, err := acquisitionModeFromHeader(header.Sample.Type)
	if err != nil {
		return nil, &ErrMalformedResponse{Reason: "decode acquisition controls", Cause: err}
	}
	if strings.TrimSpace(header.Timebase.Scale) == "" {
		return nil, &ErrMalformedResponse{Reason: "decode screen controls: horizontal scale is empty"}
	}
	channels := make([]*owonmodel.ChannelState, 0, len(header.Channels))
	for _, encoded := range header.Channels {
		channel, channelErr := channelStateFromHeader(encoded)
		if channelErr != nil {
			return nil, &ErrMalformedResponse{Reason: "decode channel controls", Cause: channelErr}
		}
		channels = append(channels, channel)
	}
	trigger, err := triggerStateFromHeader(header.Trigger, header.Status)
	if err != nil {
		return nil, &ErrMalformedResponse{Reason: "decode trigger controls", Cause: err}
	}

	return &Controls{Channels: channels, Acquisition: &owonmodel.AcquisitionState{
		Mode:        mode,
		MemoryDepth: strings.TrimSpace(header.Sample.MemoryDepth),
	}, Horizontal: &owonmodel.HorizontalState{
		Scale:              strings.TrimSpace(header.Timebase.Scale),
		ScreenHeaderOffset: header.Timebase.Offset,
	}, Trigger: trigger}, nil
}

// Controls groups the control domains interpreted from one HDS screen header.
//
// Example: a state snapshot uses these observations without guessing generator or DMM state.
type Controls struct {
	Channels    []*owonmodel.ChannelState
	Acquisition *owonmodel.AcquisitionState
	Horizontal  *owonmodel.HorizontalState
	Trigger     *owonmodel.TriggerState
}

// ScreenHeaderQuery selects the length-prefixed screen-header payload.
//
// Example: scale preflight, state and waveform operations share the same dialect query.
func ScreenHeaderQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":DATA:WAVE:SCREEN:HEAD?", ResponseMode: owonprotocol.ResponseModeLengthPrefixed}
}

// Probe interprets one selected channel's attenuation without acquiring it from the device.
//
// Example: scale preflight rejects malformed preceding channel names and a missing selected probe.
func (header *ScreenHeader) Probe(selected owonmodel.Channel) (float64, error) {
	if header == nil {
		return 0, &ErrMalformedResponse{Reason: "probe header is nil"}
	}
	for _, encoded := range header.Channels {
		channel, err := channelFromHeader(encoded.Name)
		if err != nil {
			return 0, &ErrMalformedResponse{Reason: "decode current probe channel", Cause: err}
		}
		if channel != selected {
			continue
		}
		probe, err := probeFromHeader(encoded.Probe)
		if err != nil {
			return 0, &ErrMalformedResponse{Reason: "probe field is malformed", Cause: err}
		}
		return probe, nil
	}
	return 0, &ErrMalformedResponse{Reason: "selected channel has no probe field"}
}

// acquisitionModeFromHeader maps the device's mixed-case acquisition token.
//
// Example: `SAMPle` maps to AcquisitionModeSample.
func acquisitionModeFromHeader(value string) (owonmodel.AcquisitionMode, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SAMPLE":
		return owonmodel.AcquisitionModeSample, nil
	case "PEAK", "PEAKDETECT":
		return owonmodel.AcquisitionModePeakDetect, nil
	case "AVERAGE":
		return owonmodel.AcquisitionModeAverage, nil
	default:
		return owonmodel.AcquisitionModeUnspecified, fmt.Errorf("decode screen controls: unsupported acquisition mode %q", value)
	}
}

// channelStateFromHeader converts one observed channel object with strict token validation.
//
// Example: `CH2`, `OFF`, and `AC` become their corresponding typed fields.
func channelStateFromHeader(encoded screenHeaderChannel) (*owonmodel.ChannelState, error) {
	channel, err := channelFromHeader(encoded.Name)
	if err != nil {
		return nil, err
	}
	display, err := booleanFromHeader("channel display", encoded.Display)
	if err != nil {
		return nil, err
	}
	coupling, err := couplingFromHeader(encoded.Coupling)
	if err != nil {
		return nil, err
	}
	probe, err := probeFromHeader(encoded.Probe)
	if err != nil {
		return nil, err
	}

	return &owonmodel.ChannelState{
		Channel:            channel,
		Display:            display,
		Coupling:           coupling,
		ProbeAttenuation:   probe,
		Scale:              strings.TrimSpace(encoded.Scale),
		ScreenHeaderOffset: encoded.Offset,
	}, nil
}

// channelFromHeader maps an observed channel name to its public enum.
//
// Example: `CH1` maps to Channel1.
func channelFromHeader(value string) (owonmodel.Channel, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CH1":
		return owonmodel.Channel1, nil
	case "CH2":
		return owonmodel.Channel2, nil
	default:
		return owonmodel.ChannelUnspecified, fmt.Errorf("decode screen controls: unsupported channel %q", value)
	}
}

// booleanFromHeader maps an explicit ON/OFF token without accepting unknown values.
//
// Example: `OFF` returns false while an empty token returns an error.
func booleanFromHeader(
	name string,
	value string,
) (bool, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ON":
		return true, nil
	case "OFF":
		return false, nil
	default:
		return false, fmt.Errorf("decode screen controls: unsupported %s %q", name, value)
	}
}

// couplingFromHeader maps the observed coupling token to its public enum.
//
// Example: `GND` maps to CouplingGround.
func couplingFromHeader(value string) (owonmodel.Coupling, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "AC":
		return owonmodel.CouplingAC, nil
	case "DC":
		return owonmodel.CouplingDC, nil
	case "GND", "GROUND":
		return owonmodel.CouplingGround, nil
	default:
		return owonmodel.CouplingUnspecified, fmt.Errorf("decode screen controls: unsupported coupling %q", value)
	}
}

// probeFromHeader parses the observed positive attenuation token.
//
// Example: `10X` returns 10.
func probeFromHeader(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || !strings.EqualFold(trimmed[len(trimmed)-1:], "X") {
		return 0, fmt.Errorf("decode screen controls: invalid probe attenuation %q", value)
	}
	probe, err := strconv.ParseFloat(strings.TrimSpace(trimmed[:len(trimmed)-1]), 64)
	if err != nil {
		return 0, fmt.Errorf("decode screen controls: parse probe attenuation %q: %w", value, err)
	}
	if math.IsNaN(probe) || math.IsInf(probe, 0) || probe <= 0 {
		return 0, &ErrMalformedResponse{Reason: "decoded probe attenuation is not finite and positive"}
	}

	return probe, nil
}

// triggerStateFromHeader converts the verified edge-trigger object into public state.
//
// Example: CH1, RISE, DC, and AUTO become their corresponding typed enum values.
func triggerStateFromHeader(
	encoded screenHeaderTrigger,
	status string,
) (*owonmodel.TriggerState, error) {
	if !strings.EqualFold(strings.TrimSpace(encoded.Mode), "SINGLE") {
		return nil, fmt.Errorf("decode screen controls: unsupported trigger mode %q", encoded.Mode)
	}
	if !strings.EqualFold(strings.TrimSpace(encoded.Type), "EDGE") {
		return nil, fmt.Errorf("decode screen controls: unsupported trigger type %q", encoded.Type)
	}
	source, err := triggerSourceFromHeader(encoded.Items.Channel)
	if err != nil {
		return nil, err
	}
	coupling, err := couplingFromHeader(encoded.Items.Coupling)
	if err != nil {
		return nil, err
	}
	slope, err := triggerSlopeFromHeader(encoded.Items.Edge)
	if err != nil {
		return nil, err
	}
	level, err := voltageFromHeader(encoded.Items.Level)
	if err != nil {
		return nil, err
	}
	sweepValue := encoded.Sweep
	if strings.TrimSpace(sweepValue) == "" {
		sweepValue = encoded.Items.Sweep
	}
	sweep, err := triggerSweepFromHeader(sweepValue)
	if err != nil {
		return nil, err
	}

	return &owonmodel.TriggerState{
		Source:   source,
		Coupling: coupling,
		Slope:    slope,
		Level:    level,
		Sweep:    sweep,
		Status:   strings.TrimSpace(status),
	}, nil
}

// triggerSourceFromHeader maps the verified channel-only trigger source.
//
// Example: `CH2` maps to TriggerSourceChannel2.
func triggerSourceFromHeader(value string) (owonmodel.TriggerSource, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CH1":
		return owonmodel.TriggerSourceChannel1, nil
	case "CH2":
		return owonmodel.TriggerSourceChannel2, nil
	default:
		return owonmodel.TriggerSourceUnspecified, fmt.Errorf("decode screen controls: unsupported trigger source %q", value)
	}
}

// triggerSlopeFromHeader maps the verified edge token.
//
// Example: `FALL` maps to TriggerSlopeFalling.
func triggerSlopeFromHeader(value string) (owonmodel.TriggerSlope, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "RISE", "RISING":
		return owonmodel.TriggerSlopeRising, nil
	case "FALL", "FALLING":
		return owonmodel.TriggerSlopeFalling, nil
	default:
		return owonmodel.TriggerSlopeUnspecified, fmt.Errorf("decode screen controls: unsupported trigger slope %q", value)
	}
}

// triggerSweepFromHeader maps the verified sweep token.
//
// Example: `NORMal` maps to TriggerSweepNormal.
func triggerSweepFromHeader(value string) (owonmodel.TriggerSweep, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "AUTO":
		return owonmodel.TriggerSweepAuto, nil
	case "NORMAL", "NORM":
		return owonmodel.TriggerSweepNormal, nil
	case "SINGLE":
		return owonmodel.TriggerSweepSingle, nil
	default:
		return owonmodel.TriggerSweepUnspecified, fmt.Errorf("decode screen controls: unsupported trigger sweep %q", value)
	}
}

// voltageFromHeader parses a trigger voltage with an explicit supported SI unit.
//
// Example: `500mV` returns 0.5.
func voltageFromHeader(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	upper := strings.ToUpper(trimmed)
	unit := ""
	multiplier := 0.0
	switch {
	case strings.HasSuffix(upper, "UV"):
		unit = trimmed[len(trimmed)-2:]
		multiplier = 1e-6
	case strings.HasSuffix(upper, "MV"):
		unit = trimmed[len(trimmed)-2:]
		multiplier = 1e-3
	case strings.HasSuffix(upper, "V"):
		unit = trimmed[len(trimmed)-1:]
		multiplier = 1
	default:
		return 0, fmt.Errorf("decode screen controls: unsupported trigger level %q", value)
	}
	numeric := strings.TrimSpace(strings.TrimSuffix(trimmed, unit))
	level, err := strconv.ParseFloat(numeric, 64)
	if err != nil {
		return 0, fmt.Errorf("decode screen controls: parse trigger level %q: %w", value, err)
	}
	level *= multiplier
	if math.IsNaN(level) || math.IsInf(level, 0) {
		return 0, &ErrMalformedResponse{Reason: "decoded trigger level is not finite"}
	}

	return level, nil
}
