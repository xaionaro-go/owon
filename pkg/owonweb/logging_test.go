package owonweb

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonlog"
)

// TestHTTPRequestLoggingPreservesContextFields verifies routing logs include request and process identity.
//
// Example: a health request records method and path without logging its query string.
func TestHTTPRequestLoggingPreservesContextFields(t *testing.T) {
	var output bytes.Buffer
	ctx, err := owonlog.WithContext(belt.WithField(t.Context(), "endpoint", "fixture"), owonlog.Config{Level: logger.LevelTrace, Service: "owonweb"}, &output)
	require.NoError(t, err)
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/healthz?secret=not-logged", nil)
	response := httptest.NewRecorder()
	mustHandler(t, &fakeService{}).ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	for _, field := range []string{"http_method=GET", "http_path=/healthz", "service=owonweb", "endpoint=fixture", "/ServeHTTP"} {
		require.Contains(t, output.String(), field)
	}
	require.NotContains(t, output.String(), "not-logged")
}
