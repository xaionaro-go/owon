package owonscpi

import (
	"fmt"
	"strconv"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

const (
	// minimumGeneratorFrequency is the application's lower bound from the community transcription.
	//
	// Example: 0.1 Hz is valid for every supported waveform.
	minimumGeneratorFrequency = 0.1
	// maximumSineFrequency is the transcribed HDS2202S sine bound retained by application policy.
	//
	// Example: a 25 MHz sine is within the supported range.
	maximumSineFrequency = 25_000_000
	// maximumSquarePulseFrequency is the application's transcribed square and pulse bound in hertz.
	//
	// Example: a 5 MHz square is within the supported range.
	maximumSquarePulseFrequency = 5_000_000
	// maximumRampFrequency is the application's transcribed ramp bound in hertz.
	//
	// Example: a 1 MHz ramp is within the supported range.
	maximumRampFrequency = 1_000_000
	// minimumGeneratorPercent is the retained application lower duty and symmetry bound.
	//
	// Example: explicit zero passes validation; this does not establish physical acceptance.
	minimumGeneratorPercent = 0
	// maximumGeneratorPercent is the retained application upper duty and symmetry bound.
	//
	// Example: one hundred passes validation without asserting an inclusive hardware limit.
	maximumGeneratorPercent = 100
)

// generatorNumericSetting describes one optional numeric generator command.
//
// Example: frequency requires a positive finite value before serialization.
type generatorNumericSetting struct {
	Command string
	Value   *float64
}

// validateGeneratorFrequency enforces application bounds drawn from the HDS2202S transcription.
//
// Example: five MHz is valid for square while five MHz plus one hertz is rejected.
func validateGeneratorFrequency(
	frequency float64,
	waveform owonmodel.GeneratorWaveform,
) error {
	maximum := float64(maximumSineFrequency)
	switch waveform {
	case owonmodel.GeneratorWaveformSine:
	case owonmodel.GeneratorWaveformSquare, owonmodel.GeneratorWaveformPulse:
		maximum = maximumSquarePulseFrequency
	case owonmodel.GeneratorWaveformRamp:
		maximum = maximumRampFrequency
	default:
		return fmt.Errorf("unsupported generator waveform %d: %w", waveform, &ErrInvalidSetting{Reason: "waveform is not supported"})
	}
	if frequency < minimumGeneratorFrequency || frequency > maximum {
		return fmt.Errorf("generator frequency %g outside [%g,%g] for %d: %w", frequency, minimumGeneratorFrequency, maximum, waveform, &ErrInvalidSetting{Reason: "frequency is outside the supported range"})
	}

	return nil
}

// validateGeneratorSymmetry enforces integer write grammar and the application's inclusive bounds.
//
// Example: fifty is accepted while -1 and 101 are rejected.
func validateGeneratorSymmetry(symmetry int32) error {
	if symmetry < minimumGeneratorPercent || symmetry > maximumGeneratorPercent {
		return fmt.Errorf("generator symmetry %d must be an integer in [%d,%d]: %w", symmetry, minimumGeneratorPercent, maximumGeneratorPercent, &ErrInvalidSetting{Reason: "symmetry is outside the supported range"})
	}

	return nil
}

// validateGeneratorDuty enforces retained application bounds, not proven inclusive physical limits.
//
// Example: zero and one hundred are accepted boundary values.
func validateGeneratorDuty(duty float64) error {
	if duty < minimumGeneratorPercent || duty > maximumGeneratorPercent {
		return fmt.Errorf("generator duty %g outside [%d,%d]: %w", duty, minimumGeneratorPercent, maximumGeneratorPercent, &ErrInvalidSetting{Reason: "duty is outside the supported range"})
	}

	return nil
}

// appendGeneratorNumbers formats domain-validated physical quantities in command order.
//
// Example: optional level and timing fields retain fractional values while absent fields emit nothing.
func appendGeneratorNumbers(
	commands []string,
	settings []generatorNumericSetting,
) []string {
	for _, setting := range settings {
		if setting.Value == nil {
			continue
		}
		commands = append(commands, setting.Command+formatNumber(*setting.Value))
	}
	return commands
}

// generatorWaveformSCPI maps a typed waveform to a vendor-documented token.
// Serialization tests do not prove physical SET acceptance or a complete typed state mapping.
//
// Example: GeneratorWaveformRamp maps to `RAMP`.
func generatorWaveformSCPI(waveform owonmodel.GeneratorWaveform) (string, error) {
	switch waveform {
	case owonmodel.GeneratorWaveformSine:
		return "SINE", nil
	case owonmodel.GeneratorWaveformSquare:
		return "SQUARE", nil
	case owonmodel.GeneratorWaveformRamp:
		return "RAMP", nil
	case owonmodel.GeneratorWaveformPulse:
		return "PULSE", nil
	default:
		return "", fmt.Errorf("unsupported generator waveform %d: %w", waveform, &ErrInvalidSetting{Reason: "waveform is not supported"})
	}
}

// CompileGenerator validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileGenerator(request *owonmodel.GeneratorPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	waveform := owonmodel.GeneratorWaveformUnspecified
	if request.Waveform != nil {
		waveform = *request.Waveform
	}
	if request.FrequencyHz != nil {
		if request.Waveform == nil {
			return nil, fmt.Errorf("generator frequency requires waveform for model-specific validation: %w", &ErrInvalidSetting{Reason: "waveform is required when frequency is set"})
		}
		if err := validateGeneratorFrequency(*request.FrequencyHz, waveform); err != nil {
			return nil, err
		}
	}
	if request.SymmetryPercent != nil {
		if err := validateGeneratorSymmetry(*request.SymmetryPercent); err != nil {
			return nil, err
		}
	}
	if request.DutyPercent != nil {
		if err := validateGeneratorDuty(*request.DutyPercent); err != nil {
			return nil, err
		}
	}
	commands := make([]string, 0, 14)
	if request.Waveform != nil {
		value, err := generatorWaveformSCPI(*request.Waveform)
		if err != nil {
			return nil, err
		}
		commands = append(commands, ":FUNCTION "+value)
	}
	commands = appendGeneratorNumbers(commands, []generatorNumericSetting{
		{Command: ":FUNCTION:FREQUENCY ", Value: request.FrequencyHz},
		{Command: ":FUNCTION:PERIOD ", Value: request.PeriodSeconds},
		{Command: ":FUNCTION:AMPLITUDE ", Value: request.AmplitudeVolts},
		{Command: ":FUNCTION:OFFSET ", Value: request.OffsetVolts},
		{Command: ":FUNCTION:HIGHT ", Value: request.HighVolts},
		{Command: ":FUNCTION:LOW ", Value: request.LowVolts},
	})
	if request.SymmetryPercent != nil {
		// Symmetry has integer write grammar; keep its order between levels and timing controls.
		commands = append(commands, ":FUNCTION:SYMMETRY "+strconv.FormatInt(int64(*request.SymmetryPercent), 10))
	}
	commands = appendGeneratorNumbers(commands, []generatorNumericSetting{
		{Command: ":FUNCTION:WIDTH ", Value: request.PulseWidthSeconds},
		{Command: ":FUNCTION:RISING ", Value: request.RisingSeconds},
		{Command: ":FUNCTION:FALING ", Value: request.FallingSeconds},
		{Command: ":FUNCTION:DTYCYCLE ", Value: request.DutyPercent},
	})
	if request.Load != nil {
		var load string
		switch *request.Load {
		case owonmodel.GeneratorLoadOn:
			load = "ON"
		case owonmodel.GeneratorLoadOff:
			load = "OFF"
		default:
			return nil, fmt.Errorf("set generator: unsupported load %d: %w", *request.Load, &ErrInvalidSetting{Reason: "load is not supported"})
		}
		commands = append(commands, ":FUNCTION:LOAD "+load)
	}
	if request.Output != nil {
		commands = append(commands, ":CHANNEL "+onOff(*request.Output))
	}

	return writeCommands(commands), nil
}
