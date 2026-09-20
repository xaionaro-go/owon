package owonscpi

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// RunCommand starts continuous acquisition without changing the acquisition configuration.
//
// Example: the controller executes this command in one admitted write batch.
func RunCommand() owonprotocol.Command {
	return owonprotocol.Command{Text: ":RUN", ResponseMode: owonprotocol.ResponseModeNone}
}

// StopCommand stops acquisition without changing its configuration.
//
// Example: a remote Stop request compiles to this no-response command.
func StopCommand() owonprotocol.Command {
	return owonprotocol.Command{Text: ":STOP", ResponseMode: owonprotocol.ResponseModeNone}
}

// SingleCommand arms one acquisition with the current trigger configuration.
//
// Example: the controller retains the same session admission policy as other writes.
func SingleCommand() owonprotocol.Command {
	return owonprotocol.Command{Text: ":SINGLE", ResponseMode: owonprotocol.ResponseModeNone}
}

// AutoCommand emits the source-backed V2.5.1 autoset candidate without requesting a response.
//
// Example: `:AUToseton` is retained as a candidate without claiming V2.6.0
// firmware acceptance or readback.
func AutoCommand() owonprotocol.Command {
	return owonprotocol.Command{Text: ":AUToseton", ResponseMode: owonprotocol.ResponseModeNone}
}

// AcquisitionModeQuery selects the documented acquisition-mode readback.
//
// Example: a source-backed Average write can be checked without treating transport completion as acceptance.
func AcquisitionModeQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":ACQUIRE:MODE?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// ParseAcquisitionMode decodes one acquisition-mode reply without accepting unknown firmware tokens.
//
// Example: `SAMPle` becomes AcquisitionModeSample while an unrecognized reply is malformed.
func ParseAcquisitionMode(response []byte) (owonmodel.AcquisitionMode, error) {
	fields := strings.Fields(strings.TrimSpace(string(response)))
	if len(fields) != 1 {
		return owonmodel.AcquisitionModeUnspecified, &ErrMalformedResponse{Reason: "acquisition mode reply has an invalid field count"}
	}
	switch strings.ToUpper(fields[0]) {
	case "SAMPLE":
		return owonmodel.AcquisitionModeSample, nil
	case "PEAK", "PEAKDETECT":
		return owonmodel.AcquisitionModePeakDetect, nil
	case "AVERAGE":
		return owonmodel.AcquisitionModeAverage, nil
	default:
		return owonmodel.AcquisitionModeUnspecified, &ErrMalformedResponse{Reason: "acquisition mode reply token is unknown"}
	}
}

// acquisitionModeSCPI maps a typed mode to the instrument token.
//
// Example: AcquisitionModePeakDetect maps to `PEAK`.
func acquisitionModeSCPI(mode owonmodel.AcquisitionMode) (string, error) {
	switch mode {
	case owonmodel.AcquisitionModeSample:
		return "SAMPLE", nil
	case owonmodel.AcquisitionModePeakDetect:
		return "PEAK", nil
	case owonmodel.AcquisitionModeAverage:
		return "AVERAGE", nil
	default:
		return "", fmt.Errorf("acquisition mode: %w", &owonmodel.ErrInvalidRequest{Reason: fmt.Sprintf("%d is unsupported", mode)})
	}
}

// CompileAcquisition validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileAcquisition(request *owonmodel.AcquisitionPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	commands := make([]string, 0, 2)
	if request.Mode != nil {
		mode, err := acquisitionModeSCPI(*request.Mode)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":ACQUIRE:MODE "+mode)
	}
	if request.AverageCount != nil {
		return nil, fmt.Errorf("set acquisition: %w", &ErrUnsupportedControl{Control: "average count"})
	}
	if request.MemoryDepth != nil {
		var depth string
		switch *request.MemoryDepth {
		case owonmodel.AcquisitionMemoryDepth4K:
			depth = "4K"
		case owonmodel.AcquisitionMemoryDepth8K:
			depth = "8K"
		default:
			return nil, fmt.Errorf("set acquisition: %w", &owonmodel.ErrInvalidRequest{Reason: fmt.Sprintf("memory depth %d is unsupported", *request.MemoryDepth)})
		}
		commands = append(commands, ":ACQUIRE:DEPMEM "+depth)
	}

	return writeCommands(commands), nil
}
