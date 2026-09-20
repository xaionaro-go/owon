package owonscpi

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// ErrGeneratorObservationUnavailable identifies the device sentinel that means a generator query is temporarily unavailable.
//
// Example: a transient `error` reply resets a two-observation convergence count.
type ErrGeneratorObservationUnavailable struct {
	Query string
}

// Error describes the unavailable generator query dimension.
//
// Example: errors.As distinguishes a retryable FUNCTION? sentinel from malformed data.
func (err *ErrGeneratorObservationUnavailable) Error() string {
	if err == nil || err.Query == "" {
		return "generator observation unavailable"
	}
	return fmt.Sprintf("generator observation unavailable for %s", err.Query)
}

// Unwrap reports that the device sentinel is a leaf classification.
//
// Example: errors.As matches this type without treating it as a transport failure.
func (*ErrGeneratorObservationUnavailable) Unwrap() error { return nil }

// GeneratorFunctionQuery selects the ASCII waveform-context query.
//
// Example: the controller uses it to observe FUNCTION after a waveform write.
func GeneratorFunctionQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":FUNCTION?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// GeneratorChannelQuery selects the ASCII logical generator-output query.
//
// Example: the controller uses it to preserve output state across a waveform transition.
func GeneratorChannelQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: ":CHANNEL?", ResponseMode: owonprotocol.ResponseModeASCII}
}

// GeneratorOutputCommand builds the validated logical generator-channel mutation used for output restoration.
//
// Example: false becomes the explicit `:CHANNEL OFF` command.
func GeneratorOutputCommand(value bool) owonprotocol.Command {
	token := "OFF"
	if value {
		token = "ON"
	}
	return owonprotocol.Command{Text: ":CHANNEL " + token, ResponseMode: owonprotocol.ResponseModeNone}
}

// SplitGeneratorOutputCommand removes the compiled CHANNEL write so observed operations can make it last explicitly.
//
// Example: FUNCTION and frequency remain before the operation-local output restoration command.
func SplitGeneratorOutputCommand(commands []owonprotocol.Command) ([]owonprotocol.Command, *owonprotocol.Command) {
	writeCommands := make([]owonprotocol.Command, 0, len(commands))
	var outputCommand *owonprotocol.Command
	for _, command := range commands {
		if strings.HasPrefix(strings.ToUpper(command.Text), ":CHANNEL ") {
			copyCommand := command
			outputCommand = &copyCommand
			continue
		}
		writeCommands = append(writeCommands, command)
	}
	return writeCommands, outputCommand
}

// ParseGeneratorFunction decodes one waveform-context reply while retaining its raw token.
//
// Example: `SINe` becomes GeneratorWaveformSine without asserting electrical output behavior.
func ParseGeneratorFunction(response []byte) (*owonmodel.GeneratorWaveformObservation, error) {
	raw := strings.TrimSpace(string(response))
	observation := &owonmodel.GeneratorWaveformObservation{Raw: raw}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "FUNCTION? reply is empty"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	if len(fields) != 1 {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "FUNCTION? reply has an invalid field count"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	observation.Token = fields[0]
	if observation.Token == "error" {
		observation.Status = owonmodel.GeneratorObservationUnavailable
		observation.Reason = "device returned error"
		return observation, &ErrGeneratorObservationUnavailable{Query: ":FUNCTION?"}
	}
	if !isASCII(observation.Token) {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "FUNCTION? reply token is non-ASCII"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	waveform, ok := generatorWaveformFromToken(observation.Token)
	if !ok {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "FUNCTION? reply token is unknown"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	observation.Status = owonmodel.GeneratorObservationObserved
	observation.Value = &waveform
	return observation, nil
}

// ParseGeneratorChannel decodes one logical ON/OFF reply while retaining its raw token.
//
// Example: `OFF` becomes false as a logical channel state, not an electrical validation.
func ParseGeneratorChannel(response []byte) (*owonmodel.GeneratorOutputObservation, error) {
	raw := strings.TrimSpace(string(response))
	observation := &owonmodel.GeneratorOutputObservation{Raw: raw}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "CHANNEL? reply is empty"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	if len(fields) != 1 {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "CHANNEL? reply has an invalid field count"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	observation.Token = fields[0]
	if observation.Token == "error" {
		observation.Status = owonmodel.GeneratorObservationUnavailable
		observation.Reason = "device returned error"
		return observation, &ErrGeneratorObservationUnavailable{Query: ":CHANNEL?"}
	}
	if !isASCII(observation.Token) {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "CHANNEL? reply token is non-ASCII"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	var value bool
	if !isASCII(observation.Token) {
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "CHANNEL? reply token is unknown"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	switch strings.ToUpper(observation.Token) {
	case "ON":
		value = true
	case "OFF":
		value = false
	default:
		observation.Status = owonmodel.GeneratorObservationMalformed
		observation.Reason = "CHANNEL? reply token is unknown"
		return observation, &ErrMalformedResponse{Reason: observation.Reason}
	}
	observation.Status = owonmodel.GeneratorObservationObserved
	observation.Value = &value
	return observation, nil
}

// generatorWaveformFromToken maps only the retained vendor waveform token set.
//
// Example: case differences in a SINe reply do not change its typed waveform identity.
func generatorWaveformFromToken(token string) (owonmodel.GeneratorWaveform, bool) {
	if !isASCII(token) {
		return owonmodel.GeneratorWaveformUnspecified, false
	}
	switch strings.ToUpper(token) {
	case "SINE":
		return owonmodel.GeneratorWaveformSine, true
	case "SQUARE":
		return owonmodel.GeneratorWaveformSquare, true
	case "RAMP":
		return owonmodel.GeneratorWaveformRamp, true
	case "PULSE":
		return owonmodel.GeneratorWaveformPulse, true
	case "AMPALT":
		return owonmodel.GeneratorWaveformAmpALT, true
	case "ATTALT":
		return owonmodel.GeneratorWaveformAttALT, true
	case "STAIRDN":
		return owonmodel.GeneratorWaveformStairDown, true
	case "STAIRUD":
		return owonmodel.GeneratorWaveformStairUpDown, true
	case "STAIRUP":
		return owonmodel.GeneratorWaveformStairUp, true
	case "BESSELJ":
		return owonmodel.GeneratorWaveformBesselJ, true
	case "BESSELY":
		return owonmodel.GeneratorWaveformBesselY, true
	case "SINC":
		return owonmodel.GeneratorWaveformSinc, true
	default:
		return owonmodel.GeneratorWaveformUnspecified, false
	}
}

// isASCII keeps Unicode case mappings from turning an unverified token into a known command token.
//
// Example: the long-s character in `ſINE` must remain malformed instead of matching SINE after uppercasing.
func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x80 {
			return false
		}
	}
	return true
}
