package owoncontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// TestInstrumentSetsAverageMode verifies the typed controller forwards the
// source-backed mode token without inventing an average-count command.
func TestInstrumentSetsAverageMode(t *testing.T) {
	backend := &scriptedBackend{Responses: map[string][]byte{":ACQUIRE:MODE?": []byte("AVERage")}}
	controller := newTestInstrument(t, backend)
	mode := owonmodel.AcquisitionModeAverage

	require.NoError(t, controller.SetAcquisition(t.Context(), &owonmodel.AcquisitionPatch{Mode: &mode}))
	require.Equal(t, []string{":ACQUIRE:MODE AVERAGE", ":ACQUIRE:MODE?"}, backend.Commands)
}

func TestInstrumentRejectsAverageWhenFirmwareKeepsAnotherMode(t *testing.T) {
	backend := &scriptedBackend{Responses: map[string][]byte{":ACQUIRE:MODE?": []byte("SAMPle")}}
	controller := newTestInstrument(t, backend)
	mode := owonmodel.AcquisitionModeAverage

	err := controller.SetAcquisition(t.Context(), &owonmodel.AcquisitionPatch{Mode: &mode})
	var unsupported *owonscpi.ErrUnsupportedControl
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, err.Error(), "observed")
	require.Equal(t, []string{":ACQUIRE:MODE AVERAGE", ":ACQUIRE:MODE?"}, backend.Commands)
}
