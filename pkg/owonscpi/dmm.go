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
