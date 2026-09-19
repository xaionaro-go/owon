package owoncontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// SetChannel compiles all static fields before admission and performs required probe preflight under the write lease.
//
// Example: another caller cannot change the probe between a scale-only patch's read and write.
func (controller *Controller) SetChannel(
	ctx context.Context,
	request *owonmodel.ChannelPatch,
) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.SetChannel")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.SetChannel: %v", _err) }()
	}

	plan, err := owonscpi.CompileChannel(request)
	if err != nil {
		return err
	}
	if !plan.ProbeRequired {
		return controller.executeWrites(ctx, plan.Commands)
	}
	session, err := controller.sessionForOperation("set channel")
	if err != nil {
		return err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close()
	response, err := transaction.Execute(ctx, owonscpi.ScreenHeaderQuery())
	if err != nil {
		return fmt.Errorf("query current channel probe: %w", err)
	}
	header, err := owonscpi.ParseScreenHeader(response)
	if err != nil {
		return err
	}
	probe, err := header.Probe(plan.Channel)
	if err != nil {
		return err
	}
	if err := owonscpi.ValidateChannelProbe(plan, probe); err != nil {
		return err
	}
	return controller.executeWritesWith(ctx, transaction, plan.Commands)
}
