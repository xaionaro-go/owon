package owonscpi

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// TestCompilationOwnsCommandValues proves a plan does not capture mutable caller pointers.
//
// Example: changing a requested scale after compilation cannot change preflight or command bytes.
func TestCompilationOwnsCommandValues(t *testing.T) {
	scale, display := "100mV", false
	request := &owonmodel.ChannelPatch{Channel: owonmodel.Channel1, Scale: &scale, Display: &display}
	plan, err := CompileChannel(request)
	require.NoError(t, err)
	scale, request.Channel = "1.00V", owonmodel.Channel2
	require.Equal(t, "100mV", plan.Scale)
	require.Equal(t, owonmodel.Channel1, plan.Channel)
	require.True(t, plan.ProbeRequired)
	require.Equal(t, ":CH1:DISPLAY OFF", plan.Commands[0].Text)
	require.Equal(t, ":CH1:SCALE 100mV", plan.Commands[1].Text)
	require.NoError(t, ValidateChannelProbe(plan, 10))
	var invalid *ErrInvalidSetting
	require.ErrorAs(t, ValidateChannelProbe(plan, 10000), &invalid)
	require.ErrorAs(t, ValidateChannelProbe(nil, 10), &invalid)
	for _, probe := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		require.ErrorAs(t, ValidateChannelProbe(plan, probe), &invalid)
	}
}

// TestPureDMMParsingDoesNotTimestamp verifies the dialect does not invent acquisition provenance.
//
// Example: a valid zero reading has no clock until the controller records its observation.
func TestPureDMMParsingDoesNotTimestamp(t *testing.T) {
	for _, text := range []string{"0", "0 V"} {
		measurement, err := ParseDMMMeasurement([]byte(text))
		require.NoError(t, err)
		require.Zero(t, measurement.Value)
		require.True(t, measurement.CapturedAt.IsZero())
		require.Equal(t, text, measurement.Raw)
	}
	for _, text := range []string{"", "1 V extra", "NaN", "+Inf", "not-a-number"} {
		measurement, err := ParseDMMMeasurement([]byte(text))
		require.Nil(t, measurement)
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed)
	}
}
