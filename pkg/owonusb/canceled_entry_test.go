package owonusb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestUSBEntryCancellationPrecedesAcquisition verifies native entry validation never opens resources.
//
// Example: an already canceled request leaves the opener untouched.
func TestUSBEntryCancellationPrecedesAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opener := new(scriptedUSBResourcesOpener)
	native := &Backend{config: (Config{Serial: "serial"}).withDefaults(), opener: opener}
	require.Error(t, native.ReopenAndValidate(nil, "serial"))
	require.ErrorIs(t, native.ReopenAndValidate(ctx, "serial"), context.Canceled)
	require.Zero(t, opener.Calls)
	_, err := Open(context.Background(), Config{Serial: "serial", OperationTimeout: -time.Second})
	requireErrorType[*owonsession.ErrInvalidConfig](t, err)
}
