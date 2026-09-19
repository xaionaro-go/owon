package owonusb

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// usbInterfaceRelease releases one claimed libusb interface.
// The native DefaultInterface API returns this callback; retaining it preserves its exact ownership contract.
//
// Example: usbResources invokes it before closing the device handle.
type usbInterfaceRelease func()

// usbResourceCloser is the reportable cleanup operation shared by libusb owners.
//
// Example: gousb device and context handles satisfy it while tests inject ordered closers.
type usbResourceCloser interface {
	// Close releases one native resource and reports native cleanup failure.
	//
	// Example: usbResources closes its device before its libusb context.
	Close() error
}

// usbResources owns one libusb context, device handle, and claimed interface.
//
// Example: closing it releases the interface before the device and context.
type usbResources struct {
	serial       owonmodel.SerialNumber
	usbContext   usbResourceCloser
	device       usbResourceCloser
	extraDevices []usbResourceCloser
	release      usbInterfaceRelease
	session      *endpointSession
}

// Close releases owned USB resources and joins every reportable cleanup error.
//
// Example: initialization failures use Close so partial handles are not leaked.
func (resources *usbResources) Close(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "usbResources.Close")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/usbResources.Close: %v", _err) }()
	}

	if resources == nil {
		return nil
	}
	if resources.release != nil {
		resources.release()
		resources.release = nil
	}
	resources.session = nil
	var errs []error
	switch {
	case ctx == nil:
		errs = append(errs, &ErrUnavailable{Operation: "close USB resources", Resource: "context", Reason: "is nil"})
	case ctx.Err() != nil:
		err := ctx.Err()
		errs = append(errs, fmt.Errorf("close USB resources after context ended: %w", err))
	}
	if resources.device != nil {
		if err := resources.device.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close USB device: %w", err))
		} else {
			resources.device = nil
		}
	}
	// Enumeration cleanup and full shutdown share the same retryable peer ownership rule.
	if err := resources.closeExtraDevices(); err != nil {
		errs = append(errs, err)
	}
	switch {
	case resources.usbContext == nil:
	case resources.device != nil || hasOpenExtraDevice(resources.extraDevices):
		errs = append(errs, &ErrUnavailable{Operation: "close USB resources", Resource: "libusb context", Reason: "retained because device close failed"})
	default:
		if err := resources.usbContext.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close libusb context: %w", err))
		} else {
			resources.usbContext = nil
		}
	}

	return errors.Join(errs...)
}

// hasOpenExtraDevice reports whether any unselected device still owns a native handle.
//
// Example: a failed cleanup keeps the libusb context alive until every device close succeeds.
func hasOpenExtraDevice(devices []usbResourceCloser) bool {
	for _, device := range devices {
		if device != nil {
			return true
		}
	}

	return false
}

// closeExtraDevices closes unclaimed enumeration handles while retaining failures for retry.
//
// Example: a selected device remains open while unselected peers are released before interface claim.
func (resources *usbResources) closeExtraDevices() error {
	if resources == nil {
		return nil
	}
	var errs []error
	for index, device := range resources.extraDevices {
		if device == nil {
			continue
		}
		if err := device.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close additional USB device %d: %w", index, err))
			continue
		}
		resources.extraDevices[index] = nil
	}

	return errors.Join(errs...)
}
