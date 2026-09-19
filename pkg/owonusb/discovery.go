package owonusb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/gousb"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// usbDescriptorSelector owns the VID/PID predicate used during enumeration.
//
// Example: OpenDevices receives selector.matches without an anonymous callback.
type usbDescriptorSelector struct {
	VendorID  gousb.ID
	ProductID gousb.ID
}

// matches reports whether one descriptor has the configured vendor and product IDs.
//
// Example: 5345:1234 matches the default HDS2202S selector.
func (selector usbDescriptorSelector) matches(descriptor *gousb.DeviceDesc) bool {
	return descriptor.Vendor == selector.VendorID && descriptor.Product == selector.ProductID
}

// openUSBResources selects one device and validates its bulk endpoints.
//
// Example: multiple matching VID/PID devices are narrowed by descriptor serial before claim.
func openUSBResources(
	ctx context.Context,
	config Config,
) (_result *usbResources, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "openUSBResources")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/openUSBResources: %v", _err) }()
	}

	if err := checkUSBContext(ctx, "open OWON USB resources"); err != nil {
		return nil, err
	}
	usbContext := gousb.NewContext()
	resources := &usbResources{usbContext: usbContext}
	selector := usbDescriptorSelector{VendorID: gousb.ID(config.VendorID), ProductID: gousb.ID(config.ProductID)}
	devices, enumerateErr := usbContext.OpenDevices(selector.matches)
	if enumerateErr != nil {
		resources.extraDevices = usbDeviceClosers(devices)
		return resources, errors.Join(fmt.Errorf("enumerate OWON USB devices: %w", enumerateErr), resources.Close(ctx))
	}
	if err := checkUSBContext(ctx, "continue after USB enumeration"); err != nil {
		resources.extraDevices = usbDeviceClosers(devices)
		return resources, errors.Join(err, resources.Close(ctx))
	}
	selected, serial, selectionErr := selectUSBDevice(devices, config.Serial)
	if selectionErr != nil {
		resources.extraDevices = usbDeviceClosers(devices)
		return resources, errors.Join(selectionErr, resources.Close(ctx))
	}
	resources.device = selected
	resources.serial = serial
	logger.Debugf(ctx, "selected USB serial %q from %d candidates", serial, len(devices))
	for _, device := range devices {
		if device != selected {
			resources.extraDevices = append(resources.extraDevices, device)
		}
	}
	if err := resources.closeExtraDevices(); err != nil {
		return resources, errors.Join(err, resources.Close(ctx))
	}
	if err := checkUSBContext(ctx, "configure selected USB device"); err != nil {
		return resources, errors.Join(err, resources.Close(ctx))
	}
	if err := selected.SetAutoDetach(true); err != nil {
		return resources, errors.Join(fmt.Errorf("enable automatic kernel-driver detach: %w", err), resources.Close(ctx))
	}
	usbInterface, release, err := selected.DefaultInterface()
	if err != nil {
		return resources, errors.Join(fmt.Errorf("claim OWON USB interface: %w", err), resources.Close(ctx))
	}
	resources.release = usbInterfaceRelease(release)
	if err := validateBulkEndpoints(usbInterface.Setting); err != nil {
		return resources, errors.Join(err, resources.Close(ctx))
	}
	input, err := usbInterface.InEndpoint(defaultInEndpoint)
	if err != nil {
		return resources, errors.Join(fmt.Errorf("open bulk IN endpoint: %w", err), resources.Close(ctx))
	}
	output, err := usbInterface.OutEndpoint(defaultOutEndpoint)
	if err != nil {
		return resources, errors.Join(fmt.Errorf("open bulk OUT endpoint: %w", err), resources.Close(ctx))
	}
	resources.session, err = newEndpointSession(input, output, config.MaximumResponseBytes)
	if err != nil {
		return resources, errors.Join(err, resources.Close(ctx))
	}

	return resources, nil
}

// usbDeviceClosers adapts enumerated gousb handles to the retryable owner list.
//
// Example: an enumeration failure can retain every opened handle until cleanup succeeds.
func usbDeviceClosers(devices []*gousb.Device) []usbResourceCloser {
	result := make([]usbResourceCloser, 0, len(devices))
	for _, device := range devices {
		result = append(result, device)
	}

	return result
}

// selectUSBDevice returns the uniquely selected device and its descriptor serial.
//
// Example: an omitted serial selects one candidate; unreadable or ambiguous candidates fail.
func selectUSBDevice(
	devices []*gousb.Device,
	serial owonmodel.SerialNumber,
) (*gousb.Device, owonmodel.SerialNumber, error) {
	candidates := make([]usbSerialDescriptor, 0, len(devices))
	for _, device := range devices {
		candidates = append(candidates, device)
	}
	selectedIndex, selectedSerial, err := selectUSBDeviceIndex(candidates, serial)
	if err != nil {
		return nil, "", err
	}

	return devices[selectedIndex], selectedSerial, nil
}

// usbSerialDescriptor is the small descriptor surface required for fail-closed selection.
//
// Example: tests can model unreadable peer descriptors without opening libusb hardware.
type usbSerialDescriptor interface {
	fmt.Stringer
	// SerialNumber returns the device identity or the descriptor-read failure.
	//
	// Example: an unreadable peer prevents claiming a uniquely selected serial.
	SerialNumber() (string, error)
}

// selectUSBDeviceIndex returns the unique candidate's index and serial, rejecting unreadable peers.
//
// Example: one requested serial plus one unreadable peer fails because uniqueness is unproven.
func selectUSBDeviceIndex(
	devices []usbSerialDescriptor,
	serial owonmodel.SerialNumber,
) (int, owonmodel.SerialNumber, error) {
	serial = owonmodel.SerialNumber(strings.TrimSpace(string(serial)))
	selected := -1
	var serials []owonmodel.SerialNumber
	var descriptorErrors []error
	for index, device := range devices {
		deviceSerial, err := device.SerialNumber()
		if err != nil {
			descriptorErrors = append(descriptorErrors, fmt.Errorf("read USB serial for %s: %w", device, err))
			continue
		}
		if strings.TrimSpace(deviceSerial) == "" {
			descriptorErrors = append(descriptorErrors, &ErrUnavailable{Operation: "read USB serial", Resource: device.String(), Reason: "reports an empty USB serial"})
			continue
		}
		serials = append(serials, owonmodel.SerialNumber(deviceSerial))
		if serial != "" && owonmodel.SerialNumber(deviceSerial) != serial {
			continue
		}
		if selected >= 0 && serial != "" {
			// Duplicate detection must retain failures from peers already inspected.
			return 0, "", errors.Join(
				&ErrAmbiguousDevices{Serial: serial, Serials: []owonmodel.SerialNumber{serial, serial}},
				errors.Join(descriptorErrors...),
			)
		}
		selected = index
	}
	if len(descriptorErrors) != 0 {
		return 0, "", errors.Join(
			fmt.Errorf("cannot establish unique OWON USB serial match for %q", serial),
			errors.Join(descriptorErrors...),
		)
	}
	if serial == "" && len(serials) > 1 {
		return 0, "", &ErrAmbiguousDevices{Serials: serials}
	}
	if selected < 0 {
		return 0, "", &ErrDeviceNotFound{Serial: serial, Matches: len(devices)}
	}
	if serial == "" {
		serial = serials[0]
	}
	return selected, serial, nil
}

// ErrDeviceNotFound reports that no discovered device satisfied the requested selection.
//
// Example: automatic selection with no VID/PID matches returns this error.
type ErrDeviceNotFound struct {
	Serial  owonmodel.SerialNumber
	Matches int
}

// Error describes the absent device and the number of inspected candidates.
//
// Example: an explicit serial absent among two matching devices remains actionable.
func (err *ErrDeviceNotFound) Error() string {
	if err.Serial == "" {
		return "OWON USB device was not found: no matching VID/PID devices"
	}
	return fmt.Sprintf("OWON USB device with serial %q was not found among %d VID/PID matches", err.Serial, err.Matches)
}

// Unwrap identifies a leaf discovery outcome without an underlying native failure.
//
// Example: errors.As classifies missing hardware independently of configuration errors.
func (*ErrDeviceNotFound) Unwrap() error { return nil }

// ErrAmbiguousDevices reports candidates that cannot be uniquely selected.
//
// Example: automatic selection lists all readable serials so the caller can choose explicitly.
type ErrAmbiguousDevices struct {
	Serial  owonmodel.SerialNumber
	Serials []owonmodel.SerialNumber
}

// Error explains why selection is ambiguous and how automatic selection can be narrowed.
//
// Example: distinct connected serials are printed alongside the --serial option.
func (err *ErrAmbiguousDevices) Error() string {
	if err.Serial != "" {
		return fmt.Sprintf("multiple OWON USB devices report serial %q", err.Serial)
	}
	return fmt.Sprintf("multiple OWON USB devices found with serials %q; specify --serial to select one", err.Serials)
}

// Unwrap identifies a leaf ambiguity outcome without masking descriptor-read causes.
//
// Example: a joined native descriptor error remains independently inspectable.
func (*ErrAmbiguousDevices) Unwrap() error { return nil }

// validateBulkEndpoints verifies endpoint numbers, directions, and transfer types.
//
// Example: an interrupt endpoint at address 0x81 is rejected.
func validateBulkEndpoints(setting gousb.InterfaceSetting) error {
	var input gousb.EndpointDesc
	var output gousb.EndpointDesc
	inputFound := false
	outputFound := false
	for _, endpoint := range setting.Endpoints {
		switch {
		case endpoint.Number == defaultInEndpoint && endpoint.Direction == gousb.EndpointDirectionIn:
			input = endpoint
			inputFound = true
		case endpoint.Number == defaultOutEndpoint && endpoint.Direction == gousb.EndpointDirectionOut:
			output = endpoint
			outputFound = true
		}
	}
	if !inputFound || !outputFound {
		return fmt.Errorf("OWON interface lacks required bulk endpoints: IN found=%t OUT found=%t", inputFound, outputFound)
	}
	if input.TransferType != gousb.TransferTypeBulk || input.Direction != gousb.EndpointDirectionIn {
		return fmt.Errorf("endpoint %s is %s/%s, want bulk/IN", input.Address, input.TransferType, input.Direction)
	}
	if output.TransferType != gousb.TransferTypeBulk || output.Direction != gousb.EndpointDirectionOut {
		return fmt.Errorf("endpoint %s is %s/%s, want bulk/OUT", output.Address, output.TransferType, output.Direction)
	}

	return nil
}
