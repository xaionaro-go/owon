package owonscpi

import (
	"fmt"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// triggerSourceSCPI maps a typed trigger source to the instrument token.
//
// Example: TriggerSourceChannel2 maps to `CH2`.
func triggerSourceSCPI(source owonmodel.TriggerSource) (string, error) {
	switch source {
	case owonmodel.TriggerSourceChannel1:
		return "CH1", nil
	case owonmodel.TriggerSourceChannel2:
		return "CH2", nil
	case owonmodel.TriggerSourceExternal:
		return "", fmt.Errorf("trigger source EXTERNAL: %w", &ErrUnsupportedControl{Control: "external trigger source"})
	default:
		return "", fmt.Errorf("unsupported trigger source %d: %w", source, &owonmodel.ErrInvalidRequest{Reason: "trigger source is not supported"})
	}
}

// triggerSlopeSCPI maps a typed edge slope to the instrument token.
//
// Example: TriggerSlopeFalling maps to `FALL`.
func triggerSlopeSCPI(slope owonmodel.TriggerSlope) (string, error) {
	switch slope {
	case owonmodel.TriggerSlopeRising:
		return "RISE", nil
	case owonmodel.TriggerSlopeFalling:
		return "FALL", nil
	default:
		return "", fmt.Errorf("unsupported trigger slope %d: %w", slope, &owonmodel.ErrInvalidRequest{Reason: "trigger slope is not supported"})
	}
}

// triggerCouplingSCPI maps only coupling modes documented for the HDS200 trigger subsystem.
//
// Example: CouplingDC maps to `DC`, while ground coupling is rejected.
func triggerCouplingSCPI(coupling owonmodel.Coupling) (string, error) {
	switch coupling {
	case owonmodel.CouplingAC:
		return "AC", nil
	case owonmodel.CouplingDC:
		return "DC", nil
	case owonmodel.CouplingGround:
		return "", fmt.Errorf("trigger coupling GROUND: %w", &ErrUnsupportedControl{Control: "ground trigger coupling"})
	default:
		return "", fmt.Errorf("unsupported trigger coupling %d: %w", coupling, &owonmodel.ErrInvalidRequest{Reason: "trigger coupling is not supported"})
	}
}

// triggerSweepSCPI maps a typed sweep to the instrument token.
//
// Example: TriggerSweepSingle maps to `SINGLE`.
func triggerSweepSCPI(sweep owonmodel.TriggerSweep) (string, error) {
	switch sweep {
	case owonmodel.TriggerSweepAuto:
		return "AUTO", nil
	case owonmodel.TriggerSweepNormal:
		return "NORMAL", nil
	case owonmodel.TriggerSweepSingle:
		return "SINGLE", nil
	default:
		return "", fmt.Errorf("unsupported trigger sweep %d: %w", sweep, &owonmodel.ErrInvalidRequest{Reason: "trigger sweep is not supported"})
	}
}

// CompileTrigger validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileTrigger(request *owonmodel.TriggerPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	commands := make([]string, 0, 5)
	if request.Source != nil {
		value, err := triggerSourceSCPI(*request.Source)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":TRIGGER:SINGLE:SOURCE "+value)
	}
	if request.Coupling != nil {
		value, err := triggerCouplingSCPI(*request.Coupling)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":TRIGGER:SINGLE:COUPLING "+value)
	}
	if request.Slope != nil {
		value, err := triggerSlopeSCPI(*request.Slope)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":TRIGGER:SINGLE:EDGE "+value)
	}
	if request.LevelVolts != nil {
		commands = append(commands, ":TRIGGER:SINGLE:EDGE:LEVEL "+formatNumber(*request.LevelVolts)+"V")
	}
	if request.Sweep != nil {
		value, err := triggerSweepSCPI(*request.Sweep)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":TRIGGER:SINGLE:SWEEP "+value)
	}

	return writeCommands(commands), nil
}
