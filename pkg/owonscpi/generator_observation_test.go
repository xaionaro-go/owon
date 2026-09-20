package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestGeneratorFunctionQueryAndParserPreserveTheObservedToken verifies the supported waveform query contract.
//
// Example: a firmware reply with mixed-case SINe maps to the typed sine value without losing its raw token.
func TestGeneratorFunctionQueryAndParserPreserveTheObservedToken(t *testing.T) {
	command := GeneratorFunctionQuery()
	require.Equal(t, ":FUNCTION?", command.Text)
	require.Equal(t, owonprotocol.ResponseModeASCII, command.ResponseMode)

	observation, err := ParseGeneratorFunction([]byte("SINe\r\n"))
	require.NoError(t, err)
	require.Equal(t, owonmodel.GeneratorWaveformSine, *observation.Value)
	require.Equal(t, "SINe", observation.Token)
	require.Equal(t, "SINe", observation.Raw)
	require.Equal(t, owonmodel.GeneratorObservationObserved, observation.Status)
}

// TestParseGeneratorFunctionRejectsUnknownTokensWithoutDroppingRawData verifies fail-closed context parsing.
//
// Example: an unrecognized firmware token remains available for diagnostics and cannot become a guessed waveform.
func TestParseGeneratorFunctionRejectsUnknownTokensWithoutDroppingRawData(t *testing.T) {
	observation, err := ParseGeneratorFunction([]byte("STAIR_UNKNOWN\n"))
	var malformed *ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
	require.Equal(t, "STAIR_UNKNOWN", observation.Token)
	require.Equal(t, "STAIR_UNKNOWN", observation.Raw)
	require.Equal(t, owonmodel.GeneratorObservationMalformed, observation.Status)
	require.Nil(t, observation.Value)
}

// TestGeneratorChannelQueryAndParserPreserveLogicalOutput verifies ON/OFF parsing without electrical claims.
//
// Example: an OFF reply is a logical generator-channel observation with its original token retained.
func TestGeneratorChannelQueryAndParserPreserveLogicalOutput(t *testing.T) {
	command := GeneratorChannelQuery()
	require.Equal(t, ":CHANNEL?", command.Text)
	require.Equal(t, owonprotocol.ResponseModeASCII, command.ResponseMode)

	observation, err := ParseGeneratorChannel([]byte("OFF\r\n"))
	require.NoError(t, err)
	require.NotNil(t, observation.Value)
	require.False(t, *observation.Value)
	require.Equal(t, "OFF", observation.Token)
	require.Equal(t, owonmodel.GeneratorObservationObserved, observation.Status)
}

// TestGeneratorObservationUnavailableRequiresExactLowercaseSentinel verifies parser strictness.
//
// Example: only the observed lowercase error token is retryable; uppercase or non-ASCII variants remain malformed.
func TestGeneratorObservationUnavailableRequiresExactLowercaseSentinel(t *testing.T) {
	parsers := []struct {
		Name   string
		Parse  func([]byte) (any, error)
		Status func(any) owonmodel.GeneratorObservationStatus
	}{
		{
			Name:  "function",
			Parse: func(raw []byte) (any, error) { return ParseGeneratorFunction(raw) },
			Status: func(value any) owonmodel.GeneratorObservationStatus {
				return value.(*owonmodel.GeneratorWaveformObservation).Status
			},
		},
		{
			Name:  "channel",
			Parse: func(raw []byte) (any, error) { return ParseGeneratorChannel(raw) },
			Status: func(value any) owonmodel.GeneratorObservationStatus {
				return value.(*owonmodel.GeneratorOutputObservation).Status
			},
		},
	}
	for _, parser := range parsers {
		t.Run(parser.Name, func(t *testing.T) {
			observation, err := parser.Parse([]byte("error"))
			var unavailable *ErrGeneratorObservationUnavailable
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, owonmodel.GeneratorObservationUnavailable, parser.Status(observation))

			for _, token := range []string{"ERROR", "Error", "é", "ſINE"} {
				observation, err = parser.Parse([]byte(token))
				var malformed *ErrMalformedResponse
				require.ErrorAs(t, err, &malformed, token)
				require.Equal(t, owonmodel.GeneratorObservationMalformed, parser.Status(observation), token)
			}
		})
	}
}
