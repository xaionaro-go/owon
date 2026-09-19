package owonscpi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// TestParseIdentityPreservesAllFields verifies the documented identity tuple.
//
// Example: an HDS2202S identity exposes manufacturer, model, serial, and firmware.
func TestParseIdentityPreservesAllFields(t *testing.T) {
	t.Parallel()

	info, err := ParseIdentity([]byte("OWON,HDS2202S,25061855,V2.6.0"))
	require.NoError(t, err)
	require.Equal(t, "OWON", info.Manufacturer)
	require.Equal(t, "HDS2202S", info.Model)
	require.Equal(t, owonmodel.SerialNumber("25061855"), info.Serial)
	require.Equal(t, "V2.6.0", info.Firmware)
	require.Zero(t, info.VendorID)
	require.Zero(t, info.ProductID)
	require.Empty(t, info.Capabilities)
	_, err = ParseIdentity([]byte("OWON,HDS2202S"))
	var malformed *ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
}
