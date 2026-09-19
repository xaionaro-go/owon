package owonscpi

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// MeasurementQuery compiles a domain-validated selector into one ASCII scalar query.
//
// Example: CH1 frequency maps to the HDS FREQUENCY suffix.
func MeasurementQuery(selector *owonmodel.MeasurementSelector) (owonprotocol.Command, error) {
	if err := selector.Validate(); err != nil {
		return owonprotocol.Command{}, err
	}
	channel, err := channelName(selector.Channel)
	if err != nil {
		return owonprotocol.Command{}, err
	}
	kind, _, err := measurementSCPI(selector.Kind)
	if err != nil {
		return owonprotocol.Command{}, err
	}
	return owonprotocol.Command{Text: ":MEASUREMENT:" + channel + ":" + kind + "?", ResponseMode: owonprotocol.ResponseModeASCII}, nil
}

// ParseMeasurement interprets a scalar for its selected quantity and preserves device text.
//
// Example: the frequency selector attaches Hz without inferring it from the numeric reply.
func ParseMeasurement(
	selector *owonmodel.MeasurementSelector,
	response []byte,
) (*owonmodel.Measurement, error) {
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	_, unit, err := measurementSCPI(selector.Kind)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(response))
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, &ErrMalformedResponse{Reason: "measurement value is not numeric", Cause: err}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, &ErrMalformedResponse{Reason: "measurement value is not finite"}
	}
	return &owonmodel.Measurement{Channel: selector.Channel, Kind: selector.Kind, Value: value, Unit: unit, Raw: raw}, nil
}

// measurementSCPI maps a typed measurement to its command suffix and unit.
//
// Example: MeasurementKindFrequency maps to `FREQUENCY` and `Hz`.
func measurementSCPI(kind owonmodel.MeasurementKind) (string, string, error) {
	switch kind {
	case owonmodel.MeasurementKindMaximum:
		return "MAX", "V", nil
	case owonmodel.MeasurementKindMinimum:
		return "MIN", "V", nil
	case owonmodel.MeasurementKindPeakToPeak:
		return "PKPK", "V", nil
	case owonmodel.MeasurementKindAmplitude:
		return "VAMP", "V", nil
	case owonmodel.MeasurementKindAverage:
		return "AVERAGE", "V", nil
	case owonmodel.MeasurementKindPeriod:
		return "PERIOD", "s", nil
	case owonmodel.MeasurementKindFrequency:
		return "FREQUENCY", "Hz", nil
	case owonmodel.MeasurementKindUnspecified, owonmodel.MeasurementKindUnknown:
		return "", "", fmt.Errorf("measurement kind %d is not queryable: %w", kind, &owonmodel.ErrInvalidRequest{Reason: "measurement kind is not queryable"})
	default:
		return "", "", fmt.Errorf("unsupported measurement kind %d: %w", kind, &owonmodel.ErrInvalidRequest{Reason: "measurement kind is not supported"})
	}
}

// CompileMeasurement validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileMeasurement(request *owonmodel.MeasurementPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	commands := make([]string, 0, 2+len(request.Visible))
	if request.Display != nil {
		commands = append(commands, ":MEASUREMENT:DISPLAY "+onOff(*request.Display))
	}
	if request.ReplaceVisible != nil && *request.ReplaceVisible {
		return nil, fmt.Errorf("set measurement visible replacement: %w", &ErrUnsupportedControl{Control: "visible measurement selection"})
	}

	return writeCommands(commands), nil
}
