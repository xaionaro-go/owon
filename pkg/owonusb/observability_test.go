package owonusb

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	beltlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// TestUSBStartupFailureCleanupRetainsContextFields verifies failed opening keeps its diagnostic identity.
//
// Example: expiration still closes the partial device with the initiating operation field.
func TestUSBStartupFailureCleanupRetainsContextFields(t *testing.T) {
	synctest.Test(t, verifyUSBStartupCleanupContext)
}

// verifyUSBStartupCleanupContext exercises opening and cleanup under virtual time.
//
// Example: the owned handle closes once after acquisition reports its deadline.
func verifyUSBStartupCleanupContext(t *testing.T) {
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	log.SetLevel(logrus.TraceLevel)
	ctx := logger.CtxWithLogger(t.Context(), beltlogrus.New(log).WithLevel(logger.LevelTrace))
	ctx = belt.WithField(ctx, "operation_id", "usb-startup")
	var order []string
	resources := &usbResources{device: &orderedUSBCloser{Name: "device", Order: &order}}
	opener := &startupResourcesOpener{Resources: resources, Wait: true}
	backend, err := open(ctx, Config{Serial: "serial", OperationTimeout: time.Second}, opener)
	require.Nil(t, backend)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{"device"}, order)
	require.Nil(t, resources.device)
	require.Contains(t, output.String(), "msg=Backend.Close")
	require.Contains(t, output.String(), "msg=usbResources.Close")
	require.Contains(t, output.String(), "operation_id=usb-startup")
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "msg=Backend.Close") || strings.Contains(line, "msg=usbResources.Close") {
			require.Contains(t, line, "operation_id=usb-startup")
		}
	}
	require.NotContains(t, output.String(), "close USB resources after context ended")
}
