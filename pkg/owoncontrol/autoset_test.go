package owoncontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInstrumentAutoUsesTheSourceBackedCandidate verifies the typed action
// emits one no-response candidate and does not perform guessed readback.
func TestInstrumentAutoUsesTheSourceBackedCandidate(t *testing.T) {
	backend := &scriptedBackend{}
	controller := newTestInstrument(t, backend)

	require.NoError(t, controller.Auto(t.Context()))
	require.Equal(t, []string{":AUToseton"}, backend.Commands)
}
