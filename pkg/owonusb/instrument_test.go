package owonusb

import (
	"context"
	"errors"

	"github.com/xaionaro-go/owon/pkg/owoncontrol"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// openTestInstrument composes real USB acquisition with native layers for cross-boundary lifecycle tests.
//
// Example: a controlled resource opener exercises startup deadlines without physical hardware.
func openTestInstrument(
	ctx context.Context,
	config Config,
	opener usbResourcesOpener,
) (*owoncontrol.Controller, *owonsession.Session, error) {
	backend, err := open(ctx, config, opener)
	if err != nil {
		return nil, nil, err
	}
	config = config.withDefaults()
	session, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: backend.Serial(), OperationTimeout: config.OperationTimeout})
	if err != nil {
		return nil, nil, errors.Join(err, backend.Close(context.Background()))
	}
	controller, err := owoncontrol.New(session, backend.Identity())
	if err != nil {
		return nil, nil, errors.Join(err, session.Close())
	}
	return controller, session, nil
}
