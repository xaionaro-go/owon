package owonusb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReopenRejectsMissingOpenerBeforeCleanup checks owned acquisition state before mutation.
//
// Example: a corrupt opener cannot close an existing device or silently acquire native hardware.
func TestReopenRejectsMissingOpenerBeforeCleanup(t *testing.T) {
	for _, opener := range []usbResourcesOpener{nil, (*startupResourcesOpener)(nil)} {
		verifyMissingOpenerPreservesResources(t, opener)
	}
}

// verifyMissingOpenerPreservesResources checks both absent and interface-wrapped nil owners.
//
// Example: a typed nil opener must not close the existing device before failing.
func verifyMissingOpenerPreservesResources(
	t *testing.T,
	opener usbResourcesOpener,
) {
	t.Helper()
	var order []string
	device := &orderedUSBCloser{Name: "device", Order: &order, Err: errors.New("must not close")}
	resources := &usbResources{device: device}
	backend := &Backend{config: (Config{Serial: "serial"}).withDefaults(), resources: resources, opener: opener}
	err := backend.ReopenAndValidate(t.Context(), "serial")
	requireErrorType[*ErrUnavailable](t, err)
	require.Empty(t, order)
	require.Same(t, resources, backend.resources)
}
