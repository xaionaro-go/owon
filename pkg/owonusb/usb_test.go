package owonusb

import (
	"context"
	"errors"
	"testing"

	"github.com/google/gousb"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestUSBResourceCleanupPreservesAllFailures verifies ended contexts do not skip owned handle cleanup.
//
// Example: peer and context close failures remain independently retryable while successful handles stay released.
func TestUSBResourceCleanupPreservesAllFailures(t *testing.T) {
	t.Parallel()
	var order []string
	peerCause, contextCause := errors.New("peer busy"), errors.New("context busy")
	peer := &orderedUSBCloser{Name: "peer", Order: &order, Err: peerCause}
	nativeContext := &orderedUSBCloser{Name: "context", Order: &order, Err: contextCause}
	resources := &usbResources{extraDevices: []usbResourceCloser{nil, peer}, usbContext: nativeContext}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := resources.Close(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, peerCause)
	require.NotErrorIs(t, err, contextCause)
	require.Equal(t, []string{"peer"}, order)
	peer.Err = nil
	err = resources.Close(nil)
	requireErrorType[*ErrUnavailable](t, err)
	require.ErrorIs(t, err, contextCause)
	require.False(t, hasOpenExtraDevice(resources.extraDevices))
	require.Same(t, nativeContext, resources.usbContext)
	nativeContext.Err = nil
	require.NoError(t, resources.Close(t.Context()))
	require.Nil(t, resources.usbContext)
	require.NoError(t, (*usbResources)(nil).Close(t.Context()))
	require.Equal(t, []string{"peer", "peer", "context", "context"}, order)
}

// TestUSBDescriptorSelectionRequiresOneExactSerial verifies missing and duplicate identities are rejected.
//
// Example: a matching device behind an unrelated peer is selected by index rather than enumeration order.
func TestUSBDescriptorSelectionRequiresOneExactSerial(t *testing.T) {
	t.Parallel()
	devices := []usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: "other"}, fakeUSBSerialDescriptor{Serial: "wanted"}}
	index, _, err := selectUSBDeviceIndex(devices, "wanted")
	require.NoError(t, err)
	require.Equal(t, 1, index)
	_, _, err = selectUSBDeviceIndex(devices, "absent")
	require.Error(t, err)
	devices = append(devices, fakeUSBSerialDescriptor{Serial: "wanted"})
	_, _, err = selectUSBDeviceIndex(devices, "wanted")
	require.Error(t, err)
}

// TestUSBBulkDescriptorsRejectIncompatibleInterfaces verifies descriptor validation before claiming endpoints.
//
// Example: matching endpoint numbers alone cannot authorize an interrupt endpoint.
func TestUSBBulkDescriptorsRejectIncompatibleInterfaces(t *testing.T) {
	t.Parallel()
	input := gousb.EndpointDesc{Address: 0x81, Number: 1, Direction: gousb.EndpointDirectionIn, TransferType: gousb.TransferTypeBulk}
	output := gousb.EndpointDesc{Address: 0x01, Number: 1, Direction: gousb.EndpointDirectionOut, TransferType: gousb.TransferTypeBulk}
	setting := gousb.InterfaceSetting{Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{0x81: input, 0x01: output}}
	require.NoError(t, validateBulkEndpoints(setting))
	input.TransferType = gousb.TransferTypeInterrupt
	setting.Endpoints[0x81] = input
	require.ErrorContains(t, validateBulkEndpoints(setting), "want bulk/IN")
	input.TransferType = gousb.TransferTypeBulk
	setting.Endpoints[0x81] = input
	output.TransferType = gousb.TransferTypeInterrupt
	setting.Endpoints[0x01] = output
	require.ErrorContains(t, validateBulkEndpoints(setting), "want bulk/OUT")
	delete(setting.Endpoints, 0x01)
	require.ErrorContains(t, validateBulkEndpoints(setting), "OUT found=false")
	selector := usbDescriptorSelector{VendorID: gousb.ID(DefaultVendorID), ProductID: gousb.ID(DefaultProductID)}
	require.True(t, selector.matches(&gousb.DeviceDesc{Vendor: gousb.ID(DefaultVendorID), Product: gousb.ID(DefaultProductID)}))
	require.False(t, selector.matches(&gousb.DeviceDesc{Vendor: 1, Product: gousb.ID(DefaultProductID)}))
}

// TestUSBOpenErrorRetainsFailedCleanup verifies retry ownership and causal diagnostics after opening fails.
//
// Example: successful device cleanup releases its backend once, while a busy device remains retryable.
func TestUSBOpenErrorRetainsFailedCleanup(t *testing.T) {
	t.Parallel()
	var order []string
	cause := errors.New("busy")
	device := &orderedUSBCloser{Name: "device", Order: &order, Err: cause}
	failure := &ErrUSBOpen{cause: errors.New("identity"), backend: &Backend{resources: &usbResources{device: device}}}
	require.ErrorIs(t, failure.Close(t.Context()), cause)
	require.NotNil(t, failure.backend)
	device.Err = nil
	require.NoError(t, failure.Close(t.Context()))
	require.Nil(t, failure.backend)
	require.NoError(t, failure.Close(t.Context()))
	require.NoError(t, (*ErrUSBOpen)(nil).Close(t.Context()))
	require.Equal(t, []string{"device", "device"}, order)
}

// TestUSBPeerCleanupRetriesOnlyFailedHandles verifies unselected devices stay owned until close succeeds.
//
// Example: a successful peer is never closed twice while its busy sibling is retried.
func TestUSBPeerCleanupRetriesOnlyFailedHandles(t *testing.T) {
	t.Parallel()
	var order []string
	cause := errors.New("peer busy")
	peer := &orderedUSBCloser{Name: "busy", Order: &order, Err: cause}
	resources := &usbResources{extraDevices: []usbResourceCloser{nil, &orderedUSBCloser{Name: "ready", Order: &order}, peer}}
	require.ErrorIs(t, resources.closeExtraDevices(), cause)
	require.True(t, hasOpenExtraDevice(resources.extraDevices))
	peer.Err = nil
	require.NoError(t, resources.closeExtraDevices())
	require.False(t, hasOpenExtraDevice(resources.extraDevices))
	require.NoError(t, (*usbResources)(nil).closeExtraDevices())
	require.Equal(t, []string{"ready", "busy", "busy"}, order)
}

// TestUSBOpenRejectsInvalidAndCanceledInputs verifies invalid requests do not need native discovery.
//
// Example: an already canceled context is preserved through both public opening APIs.
func TestUSBOpenRejectsInvalidAndCanceledInputs(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backend, err := Open(ctx, Config{Serial: "25061855"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, backend)
	instrument, transport, err := openTestInstrument(ctx, Config{Serial: "25061855"}, nativeUSBResourcesOpener{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, instrument)
	require.Nil(t, transport)
	backend, err = Open(ctx, Config{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, backend)
	require.Error(t, checkUSBContext(nil, "enumerate"))
	require.Error(t, validateUSBConfig(Config{Serial: "serial"}))
	_, err = (*Backend)(nil).Exchange(t.Context(), owonprotocol.Command{})
	requireErrorType[*ErrUnavailable](t, err)
	requireErrorType[*ErrUnavailable](t, (*Backend)(nil).ReopenAndValidate(t.Context(), "serial"))
	require.ErrorContains(t, (&Backend{config: Config{Serial: "serial"}}).ReopenAndValidate(t.Context(), "other"), "does not match")
}

// orderedUSBCloser records cleanup order and returns one configured error.
//
// Example: resource tests distinguish device closure from context closure.
type orderedUSBCloser struct {
	Name  string
	Order *[]string
	Err   error
}

// fakeUSBSerialDescriptor models one descriptor without opening native USB hardware.
//
// Example: selection tests inject an unreadable peer beside the requested serial.
type fakeUSBSerialDescriptor struct {
	Name   string
	Serial string
	Err    error
}

// String identifies the fake descriptor in selection errors.
//
// Example: diagnostics name the candidate that could not expose its serial.
func (descriptor fakeUSBSerialDescriptor) String() string {
	return descriptor.Name
}

// SerialNumber returns the configured serial or descriptor failure.
//
// Example: an unreadable descriptor is treated as an ambiguity.
func (descriptor fakeUSBSerialDescriptor) SerialNumber() (string, error) {
	return descriptor.Serial, descriptor.Err
}

// usbReleaseRecorder records interface release independently from close errors.
//
// Example: usbResources stores its Release method as a named usbInterfaceRelease.
type usbReleaseRecorder struct {
	Order *[]string
}

// scriptedUSBResourcesOpener returns predetermined resources for recovery tests.
//
// Example: identity-mismatch tests prove the rejected resource owner is closed.
type scriptedUSBResourcesOpener struct {
	Resources []*usbResources
	Errors    []error
	Calls     int
}

// Open returns the next scripted acquisition result.
//
// Example: an empty script fails instead of accidentally opening real hardware.
func (opener *scriptedUSBResourcesOpener) Open(
	_ context.Context,
	config Config,
) (*usbResources, error) {
	index := opener.Calls
	opener.Calls++
	if index < len(opener.Errors) && opener.Errors[index] != nil {
		return nil, opener.Errors[index]
	}
	if index >= len(opener.Resources) {
		return nil, errors.New("unexpected USB resource open")
	}

	resources := opener.Resources[index]
	if resources.serial == "" {
		resources.serial = config.Serial
	}
	return resources, nil
}

// Release records one interface release.
//
// Example: repeated resource cleanup must invoke it exactly once.
func (recorder usbReleaseRecorder) Release() {
	*recorder.Order = append(*recorder.Order, "release")
}

// Close appends its name before returning the configured result.
//
// Example: a failing device still records its attempted position after interface release.
func (closer *orderedUSBCloser) Close() error {
	*closer.Order = append(*closer.Order, closer.Name)

	return closer.Err
}

// TestValidateUSBConfigRejectsUnrepresentableIdentifiers verifies safe descriptor conversion.
//
// Example: 0x10000 cannot silently wrap into a sixteen-bit gousb.ID.
func TestValidateUSBConfigRejectsUnrepresentableIdentifiers(t *testing.T) {
	t.Parallel()

	valid := Config{VendorID: DefaultVendorID, ProductID: DefaultProductID, Serial: "25061855", ExpectedModel: DefaultModel}
	require.NoError(t, validateUSBConfig(valid))
	invalidVendor := valid
	invalidVendor.VendorID = 0x10000
	require.ErrorContains(t, validateUSBConfig(invalidVendor), "vendor")
	invalidProduct := valid
	invalidProduct.ProductID = 0x10000
	require.ErrorContains(t, validateUSBConfig(invalidProduct), "product")
}

// TestSelectUSBDeviceIndexFailsClosedOnUnreadablePeer verifies uniqueness is provable.
//
// Example: a valid requested serial plus a descriptor read error is rejected.
func TestSelectUSBDeviceIndexFailsClosedOnUnreadablePeer(t *testing.T) {
	t.Parallel()

	_, _, err := selectUSBDeviceIndex([]usbSerialDescriptor{
		fakeUSBSerialDescriptor{Name: "requested", Serial: "25061855"},
		fakeUSBSerialDescriptor{Name: "unreadable", Err: errors.New("descriptor unavailable")},
	}, "25061855")
	require.ErrorContains(t, err, "unique")
}

// TestValidateUSBConfigEnforcesHardResponseCap verifies device allocation cannot exceed sixteen MiB.
//
// Example: a maximum of sixteen MiB passes while sixteen MiB plus one fails.
func TestValidateUSBConfigEnforcesHardResponseCap(t *testing.T) {
	t.Parallel()

	config := Config{VendorID: DefaultVendorID, ProductID: DefaultProductID, Serial: "25061855", ExpectedModel: DefaultModel}
	for _, maximum := range []uint32{0, 1, owonprotocol.DefaultMaximumResponseBytes} {
		config.MaximumResponseBytes = maximum
		require.NoError(t, validateUSBConfig(config))
	}
	for _, maximum := range []uint32{owonprotocol.DefaultMaximumResponseBytes + 1, ^uint32(0)} {
		config.MaximumResponseBytes = maximum
		requireErrorType[*owonprotocol.ErrInvalidResponseLimit](t, validateUSBConfig(config))
	}
}

// TestUSBConfigDefaultsRequireTheSupportedModel verifies reconnect identity stays model-bound.
//
// Example: an omitted ExpectedModel becomes HDS2202S instead of allowing arbitrary hardware.
func TestUSBConfigDefaultsRequireTheSupportedModel(t *testing.T) {
	t.Parallel()

	config := (Config{Serial: "25061855"}).withDefaults()
	require.Equal(t, DefaultModel, config.ExpectedModel)
	require.NoError(t, validateUSBConfig(config))
}

// TestUSBResourcesCloseInOwnershipOrder verifies release, device, then context cleanup.
//
// Example: repeated Close does not release an interface or close successful handles twice.
func TestUSBResourcesCloseInOwnershipOrder(t *testing.T) {
	t.Parallel()

	var order []string
	release := usbReleaseRecorder{Order: &order}
	resources := &usbResources{
		release:    release.Release,
		device:     &orderedUSBCloser{Name: "device", Order: &order},
		usbContext: &orderedUSBCloser{Name: "context", Order: &order},
	}
	require.NoError(t, resources.Close(context.Background()))
	require.Equal(t, []string{"release", "device", "context"}, order)
	require.NoError(t, resources.Close(context.Background()))
	require.Equal(t, []string{"release", "device", "context"}, order)
}

// TestUSBResourcesRetainOwnershipAfterCloseFailure verifies recovery can retry handles.
//
// Example: a failed device close keeps both device and context while never releasing twice.
func TestUSBResourcesRetainOwnershipAfterCloseFailure(t *testing.T) {
	t.Parallel()

	var order []string
	device := &orderedUSBCloser{Name: "device", Order: &order, Err: errors.New("busy")}
	release := usbReleaseRecorder{Order: &order}
	resources := &usbResources{
		release:    release.Release,
		device:     device,
		usbContext: &orderedUSBCloser{Name: "context", Order: &order},
	}
	require.Error(t, resources.Close(context.Background()))
	require.Equal(t, []string{"release", "device"}, order)
	require.NotNil(t, resources.device)
	require.NotNil(t, resources.usbContext)
	device.Err = nil
	require.NoError(t, resources.Close(context.Background()))
	require.Equal(t, []string{"release", "device", "device", "context"}, order)
}

// TestUSBBackendClosesResourcesAfterIdentityFailure verifies partial recovery ownership.
//
// Example: a serial mismatch closes interface, device, and context without publishing the session.
func TestUSBBackendClosesResourcesAfterIdentityFailure(t *testing.T) {
	t.Parallel()

	var order []string
	release := usbReleaseRecorder{Order: &order}
	endpoint := cleanIdentityRecoveryEndpoint("OWON,HDS2202S,wrong,V2.6.0\n")
	session, err := newEndpointSession(endpoint, endpoint, 1024)
	require.NoError(t, err)
	resources := &usbResources{
		release:    release.Release,
		device:     &orderedUSBCloser{Name: "device", Order: &order},
		usbContext: &orderedUSBCloser{Name: "context", Order: &order},
		session:    session,
	}
	opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{resources}}
	backend := &Backend{
		config: Config{
			VendorID: DefaultVendorID, ProductID: DefaultProductID,
			Serial: "25061855", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024,
		},
		opener: opener,
	}

	err = backend.ReopenAndValidate(context.Background(), "25061855")
	require.ErrorContains(t, err, "serial")
	require.Equal(t, []string{"release", "device", "context"}, order)
	require.Nil(t, backend.resources)
	require.Equal(t, 1, opener.Calls)
}

// TestUSBBackendRetainsCandidateAfterIdentityCleanupFailure verifies rejected handles remain retryable.
//
// Example: a busy native device is retained instead of becoming an unreachable leak.
func TestUSBBackendRetainsCandidateAfterIdentityCleanupFailure(t *testing.T) {
	t.Parallel()

	var order []string
	device := &orderedUSBCloser{Name: "device", Order: &order, Err: errors.New("busy")}
	resources := &usbResources{
		release:    usbReleaseRecorder{Order: &order}.Release,
		device:     device,
		usbContext: &orderedUSBCloser{Name: "context", Order: &order},
	}
	endpoint := cleanIdentityRecoveryEndpoint("OWON,HDS2202S,wrong,V2.6.0\n")
	session, err := newEndpointSession(endpoint, endpoint, 1024)
	require.NoError(t, err)
	resources.session = session
	opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{resources}}
	backend := &Backend{
		config: Config{
			VendorID: DefaultVendorID, ProductID: DefaultProductID,
			Serial: "25061855", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024,
		},
		opener: opener,
	}

	err = backend.ReopenAndValidate(context.Background(), "25061855")
	require.ErrorContains(t, err, "serial")
	require.Same(t, resources, backend.resources)
	require.Equal(t, []string{"release", "device"}, order)
	device.Err = nil
	require.NoError(t, backend.Close(context.Background()))
	require.Equal(t, []string{"release", "device", "device", "context"}, order)
}

// TestUSBBackendRetainsFailedCleanupBeforeRecovery verifies no new owner is acquired early.
//
// Example: a failed poisoned-device close blocks reopen until cleanup succeeds on a later attempt.
func TestUSBBackendRetainsFailedCleanupBeforeRecovery(t *testing.T) {
	t.Parallel()

	var order []string
	device := &orderedUSBCloser{Name: "old-device", Order: &order, Err: errors.New("busy")}
	oldResources := &usbResources{
		device:     device,
		usbContext: &orderedUSBCloser{Name: "old-context", Order: &order},
	}
	newEndpoint := cleanIdentityRecoveryEndpoint("OWON,HDS2202S,25061855,V2.6.0\n")
	newSession, err := newEndpointSession(newEndpoint, newEndpoint, 1024)
	require.NoError(t, err)
	newResources := &usbResources{session: newSession}
	opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{newResources}}
	backend := &Backend{
		config: Config{
			VendorID: DefaultVendorID, ProductID: DefaultProductID,
			Serial: "25061855", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024,
		},
		opener: opener, resources: oldResources,
	}

	err = backend.ReopenAndValidate(context.Background(), "25061855")
	require.ErrorContains(t, err, "busy")
	require.Same(t, oldResources, backend.resources)
	require.Equal(t, 0, opener.Calls)
	device.Err = nil
	require.NoError(t, backend.ReopenAndValidate(context.Background(), "25061855"))
	require.Same(t, newResources, backend.resources)
	require.Equal(t, 1, opener.Calls)
	require.Equal(t, []string{"old-device", "old-device", "old-context"}, order)
}
