package owonsession

import (
	"bytes"
	"errors"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	beltlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestSessionLogsQuarantineAndRecovery checks contextual operational diagnostics without response payloads.
//
// Example: an ambiguous exchange warns once, then a later command traces recovery without replay.
func TestSessionLogsQuarantineAndRecovery(t *testing.T) {
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	log.SetLevel(logrus.TraceLevel)
	ctx := logger.CtxWithLogger(t.Context(), beltlogrus.New(log).WithLevel(logger.LevelTrace))
	ctx = belt.WithField(ctx, "operation_id", "test-operation")
	backend := &scriptedBackend{ExchangeErrors: []error{errors.New("ambiguous-transfer"), nil}, Responses: map[string][]byte{"READ?": []byte("private-response")}}
	session, err := New(backend, Config{ExpectedSerial: "serial"})
	require.NoError(t, err)
	command := owonprotocol.Command{Text: "READ?", ResponseMode: owonprotocol.ResponseModeASCII}
	_, err = session.Execute(ctx, command)
	require.ErrorContains(t, err, "ambiguous-transfer")
	_, err = session.Execute(ctx, command)
	require.NoError(t, err)
	require.NoError(t, session.CloseContext(ctx))
	logs := output.String()
	require.Contains(t, logs, "operation_id=test-operation")
	require.Contains(t, logs, "level=warning")
	require.Contains(t, logs, "quarantined")
	require.Contains(t, logs, "recovering")
	require.Contains(t, logs, "/Session.Execute")
	require.Contains(t, logs, "ambiguous-transfer")
	require.NotContains(t, logs, "private-response")
	require.Len(t, backend.Commands, 2)
	require.Equal(t, 1, backend.ReopenCalls)
}
