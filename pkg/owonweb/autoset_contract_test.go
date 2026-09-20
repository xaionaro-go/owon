package owonweb

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmbeddedAutoActionDisclosesItsUnverifiedNoResponseSemantics keeps the
// browser action honest when the command has no device readback contract.
func TestEmbeddedAutoActionDisclosesItsUnverifiedNoResponseSemantics(t *testing.T) {
	index, err := embeddedUI.ReadFile("ui/index.html")
	require.NoError(t, err)
	app, err := embeddedUI.ReadFile("ui/app.js")
	require.NoError(t, err)

	require.Contains(t, string(index), `data-action="/api/auto"`)
	require.Contains(t, string(app), "device response/readback unavailable")
	require.Contains(t, string(app), "effect unverified")
}

// TestHandlerRoutesAutoAsAnEmptyPOST verifies the WebUI action reaches the
// typed RPC boundary without accepting an invented request body.
func TestHandlerRoutesAutoAsAnEmptyPOST(t *testing.T) {
	recorder := httptest.NewRecorder()
	mustHandler(t, &fakeService{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/auto", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
}
