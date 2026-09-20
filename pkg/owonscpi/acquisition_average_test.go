package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestCompileAcquisitionAverageUsesTheDocumentedModeToken verifies the cached
// HDS200 SCPI spelling without claiming physical setter acceptance.
func TestCompileAcquisitionAverageUsesTheDocumentedModeToken(t *testing.T) {
	mode := owonmodel.AcquisitionModeAverage
	commands, err := CompileAcquisition(&owonmodel.AcquisitionPatch{Mode: &mode})

	require.NoError(t, err)
	require.Equal(t, []string{":ACQUIRE:MODE AVERAGE"}, acquisitionCommandTexts(commands))
}

func TestParseAcquisitionModeUsesStrictObservedTokens(t *testing.T) {
	for _, testCase := range []struct {
		Response string
		Want     owonmodel.AcquisitionMode
	}{
		{Response: "SAMPle", Want: owonmodel.AcquisitionModeSample},
		{Response: "PEAK", Want: owonmodel.AcquisitionModePeakDetect},
		{Response: "AVERage", Want: owonmodel.AcquisitionModeAverage},
	} {
		got, err := ParseAcquisitionMode([]byte(testCase.Response))
		require.NoError(t, err)
		require.Equal(t, testCase.Want, got)
	}
	for _, response := range []string{"", "SAMPLE extra", "unknown"} {
		got, err := ParseAcquisitionMode([]byte(response))
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, response)
		require.Zero(t, got, response)
	}
}

func acquisitionCommandTexts(commands []owonprotocol.Command) []string {
	texts := make([]string, 0, len(commands))
	for _, command := range commands {
		texts = append(texts, command.Text)
	}
	return texts
}
