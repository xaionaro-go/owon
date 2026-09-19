package owonusb

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// startupResourcesOpener records the production opening context and configuration.
//
// Example: startup tests verify the daemon's finite policy reaches resource acquisition and identity.
type startupResourcesOpener struct {
	Resources *usbResources
	Config    Config
	Deadline  time.Time
	Calls     int
	Wait      bool
	Cause     error
}

// Open returns controlled resources while observing the actual recovery deadline.
//
// Example: Wait models a cooperative acquisition phase that ends at the configured deadline.
func (opener *startupResourcesOpener) Open(
	ctx context.Context,
	config Config,
) (*usbResources, error) {
	opener.Calls++
	opener.Config = config
	opener.Deadline, _ = ctx.Deadline()
	if opener.Wait {
		<-ctx.Done()
		return opener.Resources, ctx.Err()
	}
	if opener.Resources != nil && opener.Resources.serial == "" {
		opener.Resources.serial = config.Serial
	}
	return opener.Resources, opener.Cause
}

// TestInstrumentStartupPropagatesOperationPolicy exercises the exact production startup body.
//
// Example: resource acquisition, initial identity and subsequent queries receive the same finite policy.
func TestInstrumentStartupPropagatesOperationPolicy(t *testing.T) {
	synctest.Test(t,
		// verifyStartupPolicy compares zero-default, override and earlier-parent configurations.
		//
		// Example: the earlier caller deadline caps native startup without changing later operation policy.
		func(t *testing.T) {
			for _, test := range []struct {
				Timeout time.Duration
				Parent  time.Duration
				Want    time.Duration
			}{
				{Want: owonsession.DefaultDeviceOperationTimeout},
				{Timeout: 2 * time.Second, Want: 2 * time.Second},
				{Timeout: time.Minute, Parent: time.Second, Want: time.Second},
			} {
				verifyStartupPolicy(t, test.Timeout, test.Parent, test.Want)
			}
		})
}

// verifyStartupPolicy opens a controller and queries it through production framing.
//
// Example: the initial validation and public DeviceInfo command use independently owned contexts.
func verifyStartupPolicy(
	t *testing.T,
	timeout time.Duration,
	parent time.Duration,
	want time.Duration,
) {
	endpoint := &timedBulkEndpoint{ReadChunks: [][]byte{[]byte("OWON,HDS2202S,serial,1.0\n"), []byte("OWON,HDS2202S,serial,1.0\n")}}
	session, err := newEndpointSession(endpoint, endpoint, 1024)
	require.NoError(t, err)
	opener := &startupResourcesOpener{Resources: &usbResources{session: session}}
	ctx := context.Background()
	if parent != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, parent)
		defer cancel()
	}
	start := time.Now()
	instrument, transport, err := openTestInstrument(ctx, Config{Serial: " serial ", OperationTimeout: timeout}, opener)
	require.NoError(t, err)
	defer
	// closeStartupTransport joins production cleanup even after an assertion fails.
	//
	// Example: no resource owner escapes the controlled startup proof.
	func() { require.NoError(t, transport.Close()) }()
	require.Equal(t, 1, opener.Calls)
	require.Equal(t, start.Add(want), opener.Deadline)
	require.Equal(t, owonmodel.SerialNumber("serial"), opener.Config.Serial)
	require.Equal(t, DefaultModel, opener.Config.ExpectedModel)
	policy := timeout
	if policy == 0 {
		policy = owonsession.DefaultDeviceOperationTimeout
	}
	require.Equal(t, policy, opener.Config.OperationTimeout)
	for _, deadline := range endpoint.Deadlines {
		require.Equal(t, start.Add(want), deadline)
	}
	endpoint.Deadlines = nil
	info, err := instrument.DeviceInfo(context.Background())
	require.NoError(t, err)
	require.Equal(t, owonmodel.SerialNumber("serial"), info.Serial)
	require.Equal(t, "*IDN?\n*IDN?\n", string(endpoint.Written))
	for _, deadline := range endpoint.Deadlines {
		require.Equal(t, start.Add(policy), deadline)
	}
}

// TestTimedOutUSBSessionRequiresValidatedRecovery rejects every wrong identity before pending work.
//
// Example: manufacturer, model and serial are all revalidated after a timed-out native read.
func TestTimedOutUSBSessionRequiresValidatedRecovery(t *testing.T) {
	synctest.Test(t,
		// verifyRecoveryIdentities checks failed and successful replacement sessions through Session.
		//
		// Example: a rejected identity never receives the pending command or clears quarantine.
		func(t *testing.T) {
			for _, identity := range []string{"OTHER,HDS2202S,serial,1.0", "OWON,OTHER,serial,1.0", "OWON,HDS2202S,other,1.0", "OWON,HDS2202S,serial,1.0"} {
				verifyUSBRecoveryIdentity(t, identity)
			}
		})
}

// verifyUSBRecoveryIdentity compares writes before and after native identity validation.
//
// Example: only an exact identity allows NEXT to follow the recovery's *IDN? query.
func verifyUSBRecoveryIdentity(
	t *testing.T,
	identity string,
) {
	oldWriter := new(endpointWriter)
	oldSession, err := newEndpointSession(new(deadlineReader), oldWriter, 1024)
	require.NoError(t, err)
	nextEndpoint := &timedBulkEndpoint{ReadChunks: [][]byte{[]byte(identity + "\n"), []byte("ready\n")}}
	nextSession, err := newEndpointSession(nextEndpoint, nextEndpoint, 1024)
	require.NoError(t, err)
	opener := &startupResourcesOpener{Resources: &usbResources{session: nextSession}}
	backend := &Backend{config: (Config{Serial: "serial", OperationTimeout: time.Second}).withDefaults(), opener: opener, resources: &usbResources{session: oldSession}}
	transport, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: "serial", OperationTimeout: time.Second})
	require.NoError(t, err)
	defer
	// closeRecovery releases both rejected and accepted candidate resources after assertions.
	//
	// Example: a failed identity cannot leak its controlled session.
	func() { require.NoError(t, transport.Close()) }()
	_, err = transport.Execute(context.Background(), owonprotocol.Command{Text: "FIRST", ResponseMode: owonprotocol.ResponseModeASCII})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, opener.Calls, "failure must quarantine without replay or immediate reopening")
	require.Equal(t, "FIRST\n", string(oldWriter.Data))
	response, err := transport.Execute(context.Background(), owonprotocol.Command{Text: "NEXT", ResponseMode: owonprotocol.ResponseModeASCII})
	require.Equal(t, 1, opener.Calls)
	if identity == "OWON,HDS2202S,serial,1.0" {
		require.NoError(t, err)
		require.Equal(t, "ready", string(response))
		require.Equal(t, "*IDN?\nNEXT\n", string(nextEndpoint.Written))
		transaction, err := transport.Begin(context.Background())
		require.NoError(t, err)
		transaction.Close()
		require.Equal(t, 1, opener.Calls, "successful recovery must leave the session healthy")
		return
	}
	require.Error(t, err)
	require.Nil(t, response)
	require.Equal(t, "*IDN?\n", string(nextEndpoint.Written))
	_, retryErr := transport.Begin(context.Background())
	require.ErrorContains(t, retryErr, "recover poisoned session")
	require.Equal(t, 2, opener.Calls, "a rejected identity must leave recovery required")
}

// TestInstrumentStartupFailureRetainsCleanupOwner verifies timeout and cleanup failures remain inspectable.
//
// Example: a failed startup with a busy device returns ErrUSBOpen until a later cleanup succeeds.
func TestInstrumentStartupFailureRetainsCleanupOwner(t *testing.T) {
	synctest.Test(t,
		// verifyFailedStartup retains the owned device after acquisition and cleanup both fail.
		//
		// Example: no Instrument or Session can be published from a timed-out startup.
		func(t *testing.T) {
			var order []string
			busy := errors.New("device busy")
			device := &orderedUSBCloser{Name: "device", Order: &order, Err: busy}
			opener := &startupResourcesOpener{Resources: &usbResources{device: device}, Wait: true}
			instrument, transport, err := openTestInstrument(context.Background(), Config{Serial: "serial", OperationTimeout: time.Second}, opener)
			require.Nil(t, instrument)
			require.Nil(t, transport)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.ErrorIs(t, err, busy)
			var retained *ErrUSBOpen
			require.ErrorAs(t, err, &retained)
			require.NotNil(t, retained.backend)
			require.Equal(t, 1, opener.Calls)
			device.Err = nil
			require.NoError(t, retained.Close(context.Background()))
			require.Nil(t, retained.backend)
			require.Len(t, order, 2)
		})
}

// TestInstrumentStartupValidationPrecedesResourceAcquisition verifies invalid policy never opens hardware.
//
// Example: negative timeouts and already-canceled contexts leave the injected opener untouched.
func TestInstrumentStartupValidationPrecedesResourceAcquisition(t *testing.T) {
	opener := new(startupResourcesOpener)
	instrument, transport, err := openTestInstrument(context.Background(), Config{Serial: "serial", OperationTimeout: -time.Second}, opener)
	requireErrorType[*owonsession.ErrInvalidConfig](t, err)
	require.Nil(t, instrument)
	require.Nil(t, transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	instrument, transport, err = openTestInstrument(ctx, Config{Serial: "serial"}, opener)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, instrument)
	require.Nil(t, transport)
	require.Zero(t, opener.Calls)
}

// requireErrorType verifies that an error chain contains one concrete error type.
//
// Example: requireErrorType[*ErrInvalidRequest](t, err) checks a validation result.
func requireErrorType[T error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed T
	require.ErrorAs(t, err, &typed)
}
