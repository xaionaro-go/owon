package owoncontrol

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// ErrInvalidConfig identifies a controller that cannot be constructed over a session.
//
// Example: a nil session is rejected before the controller is exposed to callers.
type ErrInvalidConfig struct{ Reason string }

// Error describes the invalid controller construction request.
//
// Example: the diagnostic identifies a missing session owner.
func (err *ErrInvalidConfig) Error() string {
	if err == nil {
		return "invalid OWON controller configuration"
	}
	return "invalid OWON controller configuration: " + err.Reason
}

// Unwrap reports that constructor validation is a leaf classification.
//
// Example: errors.As matches this type through daemon startup diagnostics.
func (*ErrInvalidConfig) Unwrap() error { return nil }

// ErrUnavailable identifies a controller without an available session owner.
//
// Example: executing through a nil or zero Controller fails before session admission.
type ErrUnavailable struct {
	Operation string
	Resource  string
	Reason    string
	Cause     error
}

// Error returns the operation, resource, and state failure.
//
// Example: the diagnostic identifies a controller that has no session.
func (err *ErrUnavailable) Error() string {
	if err == nil {
		return ""
	}
	detail := err.Resource
	switch {
	case detail == "":
		detail = err.Reason
	case err.Reason != "":
		separator := ": "
		if strings.HasPrefix(err.Reason, "is ") {
			separator = " "
		}
		detail += separator + err.Reason
	}
	if err.Operation != "" && detail != "" {
		detail = strings.Join([]string{err.Operation, detail}, ": ")
	}
	if detail == "" {
		detail = err.Operation
	}
	if err.Cause != nil {
		if detail == "" {
			return err.Cause.Error()
		}

		return fmt.Sprintf("%s: %v", detail, err.Cause)
	}

	return detail
}

// Unwrap returns an underlying resource failure.
//
// Example: errors.As can inspect Cause without parsing the controller diagnostic.
func (err *ErrUnavailable) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}

// Controller sequences typed device operations under session admission and timestamps observations.
// Dialect compilation and interpretation belong to owonscpi; this owner decides when to perform them.
//
// Example: Controller.SetChannel applies only native patch fields whose pointers are present.
type Controller struct {
	session  *owonsession.Session
	identity owonmodel.DeviceIdentity
}

// commandExecutor performs commands within either a session or held transaction.
//
// Example: State uses one Transaction for identity, measurements, and screen header.
type commandExecutor interface {
	// Execute performs one explicitly framed command without replay.
	//
	// Example: a transaction implementation retains operation-wide admission.
	Execute(
		context.Context,
		owonprotocol.Command,
	) ([]byte, error)
}

// New creates a typed controller over a serialized session.
//
// Example: a single Controller is shared by all RPC handlers.
func New(
	session *owonsession.Session,
	identity owonmodel.DeviceIdentity,
) (*Controller, error) {
	if session == nil {
		return nil, &ErrInvalidConfig{Reason: "nil session"}
	}

	return &Controller{session: session, identity: identity}, nil
}

// sessionForOperation validates the controller receiver and its owned session.
//
// Example: a zero-valued or nil Controller returns a typed resource-state error instead of panicking.
func (controller *Controller) sessionForOperation(operation string) (*owonsession.Session, error) {
	if controller == nil {
		return nil, &ErrUnavailable{Operation: operation, Resource: "controller", Reason: "is unavailable"}
	}
	if controller.session == nil {
		return nil, &ErrUnavailable{Operation: operation, Resource: "session", Reason: "is unavailable"}
	}

	return controller.session, nil
}

// DeviceInfo acquires the connected instrument identity and combines it with verified descriptor metadata.
//
// Example: clients use DeviceInfo to verify model, serial, and firmware.
func (controller *Controller) DeviceInfo(ctx context.Context) (_result *owonmodel.DeviceInfo, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.DeviceInfo")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.DeviceInfo: %v", _err) }()
	}

	session, err := controller.sessionForOperation("query device identity")
	if err != nil {
		return nil, err
	}

	return controller.deviceInfo(ctx, session)
}

// deviceInfo queries identity through the caller's command admission owner.
//
// Example: State supplies its held transaction so identity cannot interleave.
func (controller *Controller) deviceInfo(
	ctx context.Context,
	executor commandExecutor,
) (_result *owonmodel.DeviceInfo, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.deviceInfo")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.deviceInfo: %v", _err) }()
	}

	response, err := executor.Execute(ctx, owonscpi.IdentityQuery())
	if err != nil {
		return nil, fmt.Errorf("query device identity: %w", err)
	}
	info, err := owonscpi.ParseIdentity(response)
	if err != nil {
		return nil, err
	}
	info.VendorID = controller.identity.VendorID
	info.ProductID = controller.identity.ProductID
	info.Capabilities = owonscpi.CapabilitiesForModel(info.Model)

	return info, nil
}

// Execute performs one explicitly framed raw command through the safe session.
// It is the escape hatch for verified or model-specific SCPI not yet represented by typed helpers;
// typed helpers never infer unsupported generator or waveform state commands.
//
// Example: advanced clients can query newly documented SCPI fields before a typed API is added.
func (controller *Controller) Execute(
	ctx context.Context,
	command owonprotocol.Command,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Execute")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Execute: %v", _err) }()
	}

	session, err := controller.sessionForOperation("execute OWON command")
	if err != nil {
		return nil, err
	}

	return session.Execute(ctx, command)
}

// executeWrites applies commands sequentially without replaying a failed command.
//
// Example: a patch with display and scale executes two ordered commands under one transaction lease.
func (controller *Controller) executeWrites(
	ctx context.Context,
	commands []owonprotocol.Command,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.executeWrites")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.executeWrites: %v", _err) }()
	}

	session, err := controller.sessionForOperation("write OWON controls")
	if err != nil {
		return err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close()

	return controller.executeWritesWith(ctx, transaction, commands)
}

// executeWritesWith applies commands through an already-admitted operation owner.
//
// Example: scale-only channel patches query the current probe and write under one transaction.
func (controller *Controller) executeWritesWith(
	ctx context.Context,
	executor commandExecutor,
	commands []owonprotocol.Command,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.executeWritesWith")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.executeWritesWith: %v", _err) }()
	}

	if ctx != nil {
		logger.Debugf(ctx, "applying %d control writes under transaction admission", len(commands))
	}
	for _, command := range commands {
		if _, err := executor.Execute(ctx, command); err != nil {
			return fmt.Errorf("apply command %q: %w", command.Text, err)
		}
	}

	return nil
}
