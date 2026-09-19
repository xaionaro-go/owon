package owonlog

import (
	"bytes"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/stretchr/testify/require"
)

// TestWithContextEmitsFieldsAndFiltersLevels verifies the configured backend retains contextual fields.
//
// Example: trace output includes the service and endpoint while Info omits Debug.
func TestWithContextEmitsFieldsAndFiltersLevels(t *testing.T) {
	var output bytes.Buffer
	ctx, err := WithContext(belt.WithField(t.Context(), "endpoint", "unix:///tmp/device"), Config{Level: logger.LevelTrace, Service: "owond"}, &output)
	require.NoError(t, err)
	logger.Tracef(ctx, "operation")
	require.Contains(t, output.String(), "service=owond")
	require.Contains(t, output.String(), "endpoint=")
	require.Contains(t, output.String(), "level=trace")
	output.Reset()
	ctx, err = WithContext(t.Context(), Config{Level: logger.LevelInfo}, &output)
	require.NoError(t, err)
	logger.Debugf(ctx, "hidden")
	require.Empty(t, output.String())
	logger.Infof(ctx, "visible")
	require.Contains(t, output.String(), "visible")
}

// TestWithContextRejectsInvalidConfiguration catches invalid initialization before callbacks run.
//
// Example: a nil writer cannot install a backend that panics at the first request.
func TestWithContextRejectsInvalidConfiguration(t *testing.T) {
	_, err := WithContext(t.Context(), Config{Level: logger.LevelUndefined}, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "logging level")
	require.ErrorAs(t, err, new(*ErrInvalidConfig))
	_, err = WithContext(t.Context(), Config{Level: logger.LevelInfo}, nil)
	require.Error(t, err)
	var absent *bytes.Buffer
	_, err = WithContext(t.Context(), Config{Level: logger.LevelInfo}, absent)
	require.ErrorAs(t, err, new(*ErrInvalidConfig))
	require.Contains(t, err.Error(), "typed nil")
	_, err = WithContext(nil, Config{Level: logger.LevelInfo}, &bytes.Buffer{})
	require.Error(t, err)
	_, err = WithContext(t.Context(), Config{Level: types.EndOfLevel}, &bytes.Buffer{})
	require.ErrorAs(t, err, new(*ErrInvalidConfig))
	require.Nil(t, (&ErrInvalidConfig{Reason: "invalid"}).Unwrap())
	require.Contains(t, (*ErrInvalidConfig)(nil).Error(), "logging configuration")
}

// TestUnsupportedLoggingLevelFailsInitialization rejects levels the logrus backend cannot represent.
//
// Example: selecting none returns a configuration error instead of panicking during startup.
func TestUnsupportedLoggingLevelFailsInitialization(t *testing.T) {
	var output bytes.Buffer
	ctx, err := WithContext(t.Context(), Config{Level: types.LevelNone}, &output)
	require.ErrorAs(t, err, new(*ErrInvalidConfig))
	require.Nil(t, ctx)
	require.Empty(t, output.String())
}
