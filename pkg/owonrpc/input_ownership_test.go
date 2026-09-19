package owonrpc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInputFailuresAreRPCOwned distinguishes wire input from instrument values.
//
// Example: an absent Execute message is rejected before a domain command exists.
func TestInputFailuresAreRPCOwned(t *testing.T) {
	_, err := CommandFromProto(nil)
	require.Equal(t, "*owonrpc.ErrInvalidInput", fmt.Sprintf("%T", err))
}
