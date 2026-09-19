package owoncontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestInstrumentSetChannelPreservesExplicitFalseAndZero verifies optional
// native patch fields map to explicit SCPI writes rather than omission.
//
// Example: display=false and offset=0 each produce a command.
func TestInstrumentSetChannelPreservesExplicitFalseAndZero(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	controller, err := New(session, owonmodel.DeviceIdentity{})
	require.NoError(t, err)
	display := false
	offset := int64(0)

	err = controller.SetChannel(context.Background(), &owonmodel.ChannelPatch{
		Channel:         owonmodel.Channel1,
		Display:         &display,
		OffsetDivisions: &offset,
	})
	require.NoError(t, err)
	require.Equal(t, []string{":CH1:DISPLAY OFF", ":CH1:OFFSET 0"}, backend.Commands)
}

// TestInstrumentMeasurementDoesNotConflateUnknown verifies unknown selectors
// fail instead of silently mapping to a supported measurement.
//
// Example: MeasurementKindUnknown is never interpreted as frequency.
func TestInstrumentMeasurementDoesNotConflateUnknown(t *testing.T) {
	t.Parallel()

	backend := &scriptedBackend{}
	session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "25061855"})
	require.NoError(t, err)
	controller, err := New(session, owonmodel.DeviceIdentity{})
	require.NoError(t, err)

	_, err = controller.Measure(context.Background(), &owonmodel.MeasurementSelector{
		Channel: owonmodel.Channel1,
		Kind:    owonmodel.MeasurementKindUnknown,
	})
	require.Error(t, err)
	require.Empty(t, backend.Commands)
}
