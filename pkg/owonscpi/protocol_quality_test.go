package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHeaderDecodingClassifiesDeviceTokens verifies invalid device metadata never becomes caller misuse.
//
// Example: an unknown acquisition token and a nonfinite probe both produce DataLoss.
func TestHeaderDecodingClassifiesDeviceTokens(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"SAMPLE":{"TYPE":"unknown"}}`,
		`{"SAMPLE":{"TYPE":"SAMPle"},"TIMEBASE":{"SCALE":"20us"},"CHANNEL":[{"NAME":"CH1","DISPLAY":"ON","COUPLING":"DC","PROBE":"NaNX"}]}`,
		`{"SAMPLE":{"TYPE":"SAMPle"},"TIMEBASE":{"SCALE":"20us"},"Trig":{"Mode":"unknown"}}`,
	} {
		header, err := ParseScreenHeader([]byte(data))
		require.NoError(t, err)
		controls, err := header.Controls()
		requireErrorType[*ErrMalformedResponse](t, err)
		require.Nil(t, controls)
	}
}
