package owonclient

import (
	"bytes"
	"errors"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonlog"
)

// TestClientLogsOperationFailureWithoutPayload verifies contextual entry and terminal error diagnostics.
//
// Example: an unavailable transport appears in DeviceInfo's exit trace with its original cause intact.
func TestClientLogsOperationFailureWithoutPayload(t *testing.T) {
	var output bytes.Buffer
	ctx, err := owonlog.WithContext(t.Context(), owonlog.Config{Level: logger.LevelTrace, Service: "owonctl"}, &output)
	require.NoError(t, err)
	cause := errors.New("transport unavailable")
	client, err := NewClient(failedClientConnection{Cause: cause})
	require.NoError(t, err)
	_, err = client.DeviceInfo(ctx)
	require.ErrorIs(t, err, cause)
	require.Contains(t, output.String(), "DeviceInfo")
	require.Contains(t, output.String(), "/DeviceInfo: get OWON device info: transport unavailable")
	require.NotContains(t, output.String(), "payload=")
	_, err = client.DeviceInfo(nil)
	require.ErrorAs(t, err, new(*ErrInvalidInput))
}
