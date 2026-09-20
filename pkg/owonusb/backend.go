package owonusb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// usbResourcesOpener acquires one endpoint session and all resources that own it.
//
// Example: Backend uses the native opener while lifecycle tests inject deterministic ownership.
type usbResourcesOpener interface {
	// Open acquires one serial-selected USB session.
	//
	// Example: an initialization failure may return its still-owned resources for retryable cleanup.
	Open(
		context.Context,
		Config,
	) (*usbResources, error)
}

// nativeUSBResourcesOpener delegates acquisition to libusb-backed discovery.
//
// Example: Open installs this opener for production recovery.
type nativeUSBResourcesOpener struct{}

// Open acquires resources through the native libusb implementation.
//
// Example: context checks bracket synchronous native phases inside openUSBResources.
func (nativeUSBResourcesOpener) Open(
	ctx context.Context,
	config Config,
) (_result *usbResources, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "nativeUSBResourcesOpener.Open")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/nativeUSBResourcesOpener.Open: %v", _err) }()
	}

	return openUSBResources(ctx, config)
}

// checkUSBContext reports cancellation between non-interruptible native libusb phases.
//
// Example: enumeration completion checks the request before descriptor inspection continues.
func checkUSBContext(
	ctx context.Context,
	phase string,
) error {
	if ctx == nil {
		return fmt.Errorf("%s: nil context", phase)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", phase, err)
	}

	return nil
}

// Backend performs explicit exchanges and owns reconnectable USB resources.
//
// Example: Session asks it to reopen and validate after an ambiguous transaction.
type Backend struct {
	config    Config
	opener    usbResourcesOpener
	resources *usbResources
}

// Open selects one device, automatically when Serial is empty, and validates its SCPI identity.
//
// Example: failure retains retryable native cleanup in ErrUSBOpen rather than returning an unusable backend.
func Open(
	ctx context.Context,
	config Config,
) (_result *Backend, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Open")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Open: %v", _err) }()
	}

	return open(ctx, config, nativeUSBResourcesOpener{})
}

// open acquires and validates the complete backend through its resource owner.
//
// Example: production uses the native opener; a controlled opener can exercise startup without hardware.
func open(
	ctx context.Context,
	config Config,
	opener usbResourcesOpener,
) (_result *Backend, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "open")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/open: %v", _err) }()
	}

	if err := checkUSBContext(ctx, "open OWON USB backend"); err != nil {
		return nil, err
	}
	config = config.withDefaults()
	if err := validateUSBConfig(config); err != nil {
		return nil, fmt.Errorf("open OWON USB backend: %w", err)
	}
	timeout, err := owonsession.NormalizeOperationTimeout(config.OperationTimeout)
	if err != nil {
		return nil, err
	}
	config.OperationTimeout = timeout
	backend := &Backend{config: config, opener: opener}
	if err := backend.replaceAndValidate(ctx); err != nil {
		cleanupErr := backend.Close(context.WithoutCancel(ctx))
		if cleanupErr == nil {
			return nil, err
		}

		return nil, &ErrUSBOpen{cause: errors.Join(err, cleanupErr), backend: backend}
	}
	// Pin identity before publishing the backend; recovery never mutates this selection.
	backend.config.Serial = backend.resources.serial
	logger.Debugf(ctx, "USB identity validated for serial %q", backend.config.Serial)

	return backend, nil
}

// Serial returns the descriptor serial verified against SCPI during successful opening.
//
// Example: daemon composition pins Session.ExpectedSerial to the automatically selected device.
func (backend *Backend) Serial() owonmodel.SerialNumber {
	return backend.config.Serial
}

// Identity returns verified descriptor metadata without performing device I/O.
//
// Example: daemon composition passes this independent value to its controller.
func (backend *Backend) Identity() owonmodel.DeviceIdentity {
	return owonmodel.DeviceIdentity{VendorID: backend.config.VendorID, ProductID: backend.config.ProductID}
}

// Exchange executes one command on the currently validated USB session.
//
// Example: Session serializes calls before they reach this method.
func (backend *Backend) Exchange(
	ctx context.Context,
	command owonprotocol.Command,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Backend.Exchange")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Backend.Exchange: %v", _err) }()
	}

	if backend == nil || backend.resources == nil || backend.resources.session == nil {
		return nil, &ErrUnavailable{Operation: "exchange OWON USB command", Resource: "backend", Reason: "is not open"}
	}

	return backend.resources.session.Exchange(ctx, command)
}

// ReopenAndValidate replaces the session and proves USB serial plus SCPI identity.
//
// Example: recovery never replays the command that poisoned the previous session.
func (backend *Backend) ReopenAndValidate(
	ctx context.Context,
	expectedSerial owonmodel.SerialNumber,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Backend.ReopenAndValidate")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Backend.ReopenAndValidate: %v", _err) }()
	}

	if backend == nil {
		return &ErrUnavailable{Operation: "reopen OWON USB backend", Resource: "backend", Reason: "is nil"}
	}
	if expectedSerial == "" || expectedSerial != backend.config.Serial {
		return fmt.Errorf("reopen OWON USB backend: expected serial %q does not match configured serial %q", expectedSerial, backend.config.Serial)
	}
	return backend.replaceAndValidate(ctx)
}

// replaceAndValidate closes the previous session, acquires its replacement, and validates identity.
//
// Example: initial automatic selection becomes exact serial selection on every later recovery.
func (backend *Backend) replaceAndValidate(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Backend.replaceAndValidate")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Backend.replaceAndValidate: %v", _err) }()
	}

	if err := checkUSBContext(ctx, "reopen OWON USB backend"); err != nil {
		return err
	}
	if isNilUSBResourcesOpener(backend.opener) {
		return &ErrUnavailable{Operation: "reopen OWON USB backend", Resource: "resource opener", Reason: "is nil"}
	}
	timeout, err := owonsession.NormalizeOperationTimeout(backend.config.OperationTimeout)
	if err != nil {
		return err
	}
	// Initial identity runs before Session exists; nested recovery keeps the earlier deadline.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := checkUSBContext(ctx, "begin bounded OWON USB recovery"); err != nil {
		return err
	}
	if err := backend.Close(ctx); err != nil {
		return fmt.Errorf("close poisoned OWON USB session: %w", err)
	}
	if err := checkUSBContext(ctx, "reopen OWON USB backend"); err != nil {
		return err
	}
	logger.Debugf(ctx, "acquiring USB resources for serial selection %q", backend.config.Serial)
	resources, err := backend.opener.Open(ctx, backend.config)
	if err != nil {
		if resources != nil {
			backend.resources = resources
		}
		return fmt.Errorf("open OWON USB resources: %w", err)
	}
	if err := checkUSBContext(ctx, "validate reopened OWON USB backend"); err != nil {
		return backend.rejectResources(ctx, resources, err)
	}
	if resources.serial == "" || (backend.config.Serial != "" && resources.serial != backend.config.Serial) {
		return backend.rejectResources(ctx, resources, &ErrUnavailable{Operation: "validate USB serial", Resource: "descriptor", Reason: fmt.Sprintf("serial %q does not match selection %q", resources.serial, backend.config.Serial)})
	}
	report, err := resources.session.drainPending(ctx)
	if err != nil {
		return backend.rejectResources(ctx, resources, fmt.Errorf("drain stale OWON USB response: %w", err))
	}
	logger.Debugf(ctx, "drained %d stale USB response bytes in %d reads", report.DiscardedBytes, report.ReadCalls)
	identity, err := resources.session.Exchange(ctx, owonscpi.IdentityQuery())
	if err != nil {
		return backend.rejectResources(ctx, resources, fmt.Errorf("validate OWON SCPI identity: %w", err))
	}
	info, err := owonscpi.ParseIdentity(identity)
	if err != nil {
		return backend.rejectResources(ctx, resources, err)
	}
	if !strings.EqualFold(strings.TrimSpace(info.Manufacturer), "OWON") {
		return backend.rejectResources(ctx, resources, fmt.Errorf("SCPI manufacturer %q is not OWON", info.Manufacturer))
	}
	if info.Serial != resources.serial {
		return backend.rejectResources(ctx, resources, fmt.Errorf("SCPI serial %q does not match expected serial %q", info.Serial, resources.serial))
	}
	if info.Model != backend.config.ExpectedModel {
		return backend.rejectResources(ctx, resources, fmt.Errorf("SCPI model %q does not match expected model %q", info.Model, backend.config.ExpectedModel))
	}
	backend.resources = resources

	return nil
}

// isNilUSBResourcesOpener rejects absent acquisition owners, including typed nil interfaces.
//
// Example: an opener containing a nil pointer cannot trigger cleanup or replacement acquisition.
func isNilUSBResourcesOpener(opener usbResourcesOpener) bool {
	if opener == nil {
		return true
	}
	value := reflect.ValueOf(opener)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// rejectResources closes a candidate session and retains it when native cleanup fails.
//
// Example: recovery retries a failed identity candidate before acquiring another handle.
func (backend *Backend) rejectResources(
	ctx context.Context,
	resources *usbResources,
	cause error,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Backend.rejectResources")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Backend.rejectResources: %v", _err) }()
	}

	closeErr := resources.Close(ctx)
	if closeErr != nil {
		backend.resources = resources
	} else {
		backend.resources = nil
	}

	return errors.Join(cause, closeErr)
}

// Close releases the current USB session.
//
// Example: Session.Close delegates here during daemon shutdown.
func (backend *Backend) Close(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Backend.Close")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Backend.Close: %v", _err) }()
	}

	if backend == nil || backend.resources == nil {
		return nil
	}
	err := backend.resources.Close(ctx)
	if err == nil {
		backend.resources = nil
	}

	return err
}
