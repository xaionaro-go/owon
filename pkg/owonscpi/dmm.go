package owonscpi

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// CompileDMM validates and serializes one complete supported DMM patch without I/O.
//
// Example: voltage/DC followed by relative mode produces two ordered commands.
func CompileDMM(request *owonmodel.DMMPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	commands := make([]string, 0, 4)
	if request.Function != nil {
		command, err := dmmFunctionCommand(*request.Function, request.CurrentType)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if request.Relative != nil {
		commands = append(commands, ":DMM:REL "+onOff(*request.Relative))
	}
	if request.Range != nil {
		var rangeValue string
		switch *request.Range {
		case owonmodel.DMMRangeOn:
			rangeValue = "ON"
		case owonmodel.DMMRangeMV:
			rangeValue = "mV"
		case owonmodel.DMMRangeV:
			rangeValue = "V"
		default:
			return nil, fmt.Errorf("set DMM: unsupported range %d: %w", *request.Range, &owonmodel.ErrInvalidRequest{Reason: "range is not supported"})
		}
		commands = append(commands, ":DMM:RANGE "+rangeValue)
	}
	if request.AutoRange != nil {
		if !*request.AutoRange {
			return nil, fmt.Errorf("set DMM auto range OFF: %w", &ErrUnsupportedControl{Control: "manual DMM range"})
		}
		commands = append(commands, ":DMM:AUTO "+onOff(*request.AutoRange))
	}

	return writeCommands(commands), nil
}

// dmmFunctionCommand serializes a domain-validated function/current-type selection.
//
// Example: voltage needs AC or DC, while resistance rejects either companion.
func dmmFunctionCommand(
	function owonmodel.DMMFunction,
	currentType *owonmodel.DMMCurrentType,
) (string, error) {
	value, err := dmmFunctionSCPI(function)
	if err != nil {
		return "", err
	}
	switch function {
	case owonmodel.DMMFunctionVoltage, owonmodel.DMMFunctionCurrent:
		current, err := dmmCurrentTypeSCPI(*currentType)
		if err != nil {
			return "", err
		}
		return ":DMM:CONFIGURE:" + value + " " + current, nil
	default:
		return ":DMM:CONFIGURE " + value, nil
	}
}

// DMMFunctionQuery selects the verified readback form for one DMM function dimension.
//
// Example: resistance uses the generic CONFIGURE query, while voltage uses CONFIGURE:VOLTAGE.
func DMMFunctionQuery(function owonmodel.DMMFunction) (owonprotocol.Command, error) {
	switch function {
	case owonmodel.DMMFunctionVoltage:
		return owonprotocol.Command{Text: ":DMM:CONFIGURE:VOLTAGE?", ResponseMode: owonprotocol.ResponseModeASCII}, nil
	case owonmodel.DMMFunctionCurrent:
		return owonprotocol.Command{Text: ":DMM:CONFIGURE:CURRENT?", ResponseMode: owonprotocol.ResponseModeASCII}, nil
	case owonmodel.DMMFunctionResistance, owonmodel.DMMFunctionCapacitance, owonmodel.DMMFunctionDiode, owonmodel.DMMFunctionContinuity:
		return owonprotocol.Command{Text: ":DMM:CONFIGURE?", ResponseMode: owonprotocol.ResponseModeASCII}, nil
	default:
		return owonprotocol.Command{}, fmt.Errorf("query DMM function %d: %w", function, &owonmodel.ErrInvalidRequest{Reason: "function is not supported"})
	}
}

// DMMRangeQuery selects the documented multimeter range readback.
//
// Example: a state snapshot executes this ASCII query before exposing a typed range.
func DMMRangeQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":DMM:RANGE?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// ParseDMMRange decodes one documented multimeter range token.
//
// Example: `mV` becomes DMMRangeMV while an unverified device-specific token is malformed.
func ParseDMMRange(response []byte) (owonmodel.DMMRange, error) {
	fields := strings.Fields(strings.TrimSpace(string(response)))
	if len(fields) != 1 {
		return owonmodel.DMMRangeUnspecified, &ErrMalformedResponse{Reason: "DMM range reply has an invalid field count"}
	}
	switch strings.ToUpper(fields[0]) {
	case "ON":
		return owonmodel.DMMRangeOn, nil
	case "MV":
		return owonmodel.DMMRangeMV, nil
	case "V":
		return owonmodel.DMMRangeV, nil
	default:
		return owonmodel.DMMRangeUnspecified, &ErrMalformedResponse{Reason: "DMM range reply token is unknown"}
	}
}

// ErrDMMFunctionUnavailable identifies the instrument's transient DMM function-query sentinel.
//
// Example: the exact trimmed `error` reply resets a convergence match count without being treated as malformed data.
type ErrDMMFunctionUnavailable struct {
	Function owonmodel.DMMFunction
}

// Error describes a function query that the instrument could not answer yet.
//
// Example: a controller retries this typed condition during its bounded convergence barrier.
func (err *ErrDMMFunctionUnavailable) Error() string {
	if err == nil {
		return "DMM function query unavailable"
	}
	return fmt.Sprintf("DMM function query unavailable for %d", err.Function)
}

// Unwrap reports that the device sentinel is a leaf classification.
//
// Example: errors.As identifies the transient condition without parsing its text.
func (*ErrDMMFunctionUnavailable) Unwrap() error {
	return nil
}

// ParseDMMFunction decodes one function-dimension query reply without claiming full device state.
// For voltage/current queries, the returned Function is the query context and CurrentType is the observed AC/DC token; neither field alone proves the instrument's active function.
//
// Example: ParseDMMFunction(DMMFunctionDiode, []byte("RESistance")) returns a valid resistance nonmatch.
func ParseDMMFunction(
	function owonmodel.DMMFunction,
	response []byte,
) (*owonmodel.DMMFunctionSelection, error) {
	if _, err := DMMFunctionQuery(function); err != nil {
		return nil, err
	}
	fields := strings.Fields(strings.TrimSpace(string(response)))
	if len(fields) == 0 {
		return nil, &ErrMalformedResponse{Reason: "DMM function reply is empty"}
	}
	if len(fields) != 1 {
		return nil, &ErrMalformedResponse{Reason: "DMM function reply has an invalid field count"}
	}
	token := fields[0]
	if token == "error" {
		return nil, &ErrDMMFunctionUnavailable{Function: function}
	}
	switch strings.ToUpper(token) {
	case "AC":
		if function != owonmodel.DMMFunctionVoltage && function != owonmodel.DMMFunctionCurrent {
			return nil, &ErrMalformedResponse{Reason: "AC reply is invalid for generic DMM function query"}
		}
		currentType := owonmodel.DMMCurrentTypeAC
		return &owonmodel.DMMFunctionSelection{Function: function, CurrentType: &currentType}, nil
	case "DC":
		if function != owonmodel.DMMFunctionVoltage && function != owonmodel.DMMFunctionCurrent {
			return nil, &ErrMalformedResponse{Reason: "DC reply is invalid for generic DMM function query"}
		}
		currentType := owonmodel.DMMCurrentTypeDC
		return &owonmodel.DMMFunctionSelection{Function: function, CurrentType: &currentType}, nil
	case "VOLTAGE":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionVoltage}, nil
	case "CURRENT":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionCurrent}, nil
	case "RESISTANCE":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionResistance}, nil
	case "CAPACITANCE":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionCapacitance}, nil
	case "DIODE":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionDiode}, nil
	case "CONTINUITY":
		return &owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionContinuity}, nil
	default:
		return nil, &ErrMalformedResponse{Reason: "DMM function reply token is unknown"}
	}
}

// DMMMeasurementQuery selects the current digital-multimeter scalar reading.
//
// Example: the controller executes this ASCII query before parsing and timestamping its result.
func DMMMeasurementQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":DMM:MEAS?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// ParseDMMMeasurement decodes a value and optional unit without acquiring a timestamp.
//
// Example: "1.25 V" retains its text and voltage unit while CapturedAt remains zero.
func ParseDMMMeasurement(response []byte) (*owonmodel.DMMMeasurement, error) {
	raw := strings.TrimSpace(string(response))
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 2 {
		return nil, &ErrMalformedResponse{Reason: "DMM measurement has an invalid field count"}
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, &ErrMalformedResponse{Reason: "DMM measurement is not numeric", Cause: err}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, &ErrMalformedResponse{Reason: "DMM measurement is not finite"}
	}
	unit := ""
	if len(fields) == 2 {
		unit = fields[1]
	}
	return &owonmodel.DMMMeasurement{Value: value, Unit: unit, Raw: raw}, nil
}

// dmmFunctionSCPI maps a typed DMM function to the instrument token.
//
// Example: DMMFunctionResistance maps to `RESISTANCE`.
func dmmFunctionSCPI(function owonmodel.DMMFunction) (string, error) {
	switch function {
	case owonmodel.DMMFunctionVoltage:
		return "VOLTAGE", nil
	case owonmodel.DMMFunctionCurrent:
		return "CURRENT", nil
	case owonmodel.DMMFunctionResistance:
		return "RESISTANCE", nil
	case owonmodel.DMMFunctionCapacitance:
		return "CAPACITANCE", nil
	case owonmodel.DMMFunctionDiode:
		return "DIODE", nil
	case owonmodel.DMMFunctionContinuity:
		return "CONTINUITY", nil
	default:
		return "", fmt.Errorf("unsupported DMM function %d: %w", function, &owonmodel.ErrInvalidRequest{Reason: "function is not supported"})
	}
}

// dmmCurrentTypeSCPI maps a typed AC/DC selection to the instrument token.
//
// Example: DMM_CURRENT_TYPE_DC maps to `DC`.
func dmmCurrentTypeSCPI(currentType owonmodel.DMMCurrentType) (string, error) {
	switch currentType {
	case owonmodel.DMMCurrentTypeAC:
		return "AC", nil
	case owonmodel.DMMCurrentTypeDC:
		return "DC", nil
	default:
		return "", fmt.Errorf("unsupported DMM current type %d: %w", currentType, &owonmodel.ErrInvalidRequest{Reason: "current type is not supported"})
	}
}
