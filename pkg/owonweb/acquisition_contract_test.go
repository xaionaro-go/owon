package owonweb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmbeddedAcquisitionAverageControlMatchesTheSupportedBoundary keeps the
// documented mode visible while leaving average-count writes unavailable.
func TestEmbeddedAcquisitionAverageControlMatchesTheSupportedBoundary(t *testing.T) {
	content, err := embeddedUI.ReadFile("ui/index.html")
	require.NoError(t, err)
	markup := string(content)

	require.Contains(t, markup, `value="ACQUISITION_MODE_AVERAGE">Average`)
	require.Contains(t, markup, "Average count and external/ground trigger controls remain unavailable")
}
