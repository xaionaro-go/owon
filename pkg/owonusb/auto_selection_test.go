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

// TestAutomaticUSBSelection requires exactly one readable descriptor when serial is omitted.
//
// Example: two matching instruments require an explicit serial, even when their serials agree.
func TestAutomaticUSBSelection(t *testing.T) {
	for _, serial := range []owonmodel.SerialNumber{"", " \t "} {
		index, selectedSerial, err := selectUSBDeviceIndex([]usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: "scope"}}, serial)
		require.NoError(t, err)
		require.Zero(t, index)
		require.Equal(t, owonmodel.SerialNumber("scope"), selectedSerial)
	}
	_, _, err := selectUSBDeviceIndex(nil, "")
	require.ErrorContains(t, err, "not found")
	requireErrorType[*ErrDeviceNotFound](t, err)
	require.Nil(t, errors.Unwrap(err))
	for _, serials := range [][]owonmodel.SerialNumber{{"first", "second"}, {"same", "same"}, {"first", "second", "third"}} {
		var descriptors []usbSerialDescriptor
		for _, serial := range serials {
			descriptors = append(descriptors, fakeUSBSerialDescriptor{Serial: string(serial)})
		}
		_, _, err := selectUSBDeviceIndex(descriptors, "")
		require.ErrorContains(t, err, "specify --serial")
		var ambiguous *ErrAmbiguousDevices
		require.ErrorAs(t, err, &ambiguous)
		require.Equal(t, serials, ambiguous.Serials)
		require.Nil(t, errors.Unwrap(ambiguous))
		for _, serial := range serials {
			require.Contains(t, err.Error(), serial)
		}
	}
	cause := errors.New("descriptor unreadable")
	_, _, err = selectUSBDeviceIndex([]usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: "scope"}, fakeUSBSerialDescriptor{Err: cause}}, "")
	require.ErrorIs(t, err, cause)
	_, _, err = selectUSBDeviceIndex([]usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: " \t "}}, "")
	require.ErrorContains(t, err, "empty USB serial")
}

// TestAutomaticUSBOpenReachesDiscovery verifies default configuration is valid without a serial.
//
// Example: a discovery error is reported without the old serial-required configuration rejection.
func TestAutomaticUSBOpenReachesDiscovery(t *testing.T) {
	cause := errors.New("discovery unavailable")
	opener := &startupResourcesOpener{Cause: cause}
	backend, err := open(context.Background(), Config{}, opener)
	require.Nil(t, backend)
	require.ErrorIs(t, err, cause)
	require.Equal(t, 1, opener.Calls)
	require.Empty(t, opener.Config.Serial)
}

// TestAutomaticUSBOpenValidatesDescriptorIdentity rejects SCPI identities inconsistent with discovery.
//
// Example: the sole USB descriptor cannot authorize a different instrument's SCPI serial.
func TestAutomaticUSBOpenValidatesDescriptorIdentity(t *testing.T) {
	for _, identity := range []string{"OWON,HDS2202S,scope,1.0", "OWON,HDS2202S,other,1.0", "OTHER,HDS2202S,scope,1.0", "OWON,OTHER,scope,1.0"} {
		endpoint := &timedBulkEndpoint{ReadChunks: [][]byte{[]byte(identity + "\n")}, QuietProbe: true}
		session, err := newEndpointSession(endpoint, endpoint, 1024)
		require.NoError(t, err)
		opener := &selectingUSBResourcesOpener{
			Descriptors: []usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: "scope"}},
			Resources:   &usbResources{session: session},
		}
		backend, err := open(t.Context(), Config{Serial: " \t "}, opener)
		require.Equal(t, "*IDN?\n", string(endpoint.Written))
		require.Len(t, opener.Configs, 1)
		require.Empty(t, opener.Configs[0].Serial)
		if identity != "OWON,HDS2202S,scope,1.0" {
			require.Error(t, err)
			require.Nil(t, backend)
			require.Nil(t, opener.Resources.session, "rejected candidate must be closed")
			continue
		}
		require.NoError(t, err)
		require.Equal(t, owonmodel.SerialNumber("scope"), backend.Serial())
		require.Error(t, backend.ReopenAndValidate(t.Context(), ""))
		require.Error(t, backend.ReopenAndValidate(t.Context(), "other"))
		require.Len(t, opener.Configs, 1, "invalid recovery identity must not acquire another device")
		require.Equal(t, owonmodel.SerialNumber("scope"), backend.Serial())
		require.NoError(t, backend.Close(t.Context()))
	}
}

// TestAutomaticUSBRecoveryPinsSelectedSerial proves recovery never switches to a replacement device.
//
// Example: a replacement alone is refused; the original returning permits recovery without replay.
func TestAutomaticUSBRecoveryPinsSelectedSerial(t *testing.T) {
	synctest.Test(t,
		// exerciseAutomaticRecovery drives real session quarantine, selection, framing, and identity checks.
		//
		// Example: a timed-out read is followed by explicit-serial recovery, not another automatic choice.
		func(t *testing.T) {
			first := &timedBulkEndpoint{ReadChunks: [][]byte{[]byte("OWON,HDS2202S,scope,1.0\n")}, QuietProbe: true}
			firstSession, err := newEndpointSession(first, first, 1024)
			require.NoError(t, err)
			opener := &selectingUSBResourcesOpener{
				Descriptors: []usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: "scope"}},
				Resources:   &usbResources{session: firstSession},
			}
			backend, err := open(t.Context(), Config{OperationTimeout: time.Second}, opener)
			require.NoError(t, err)
			transport, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: backend.Serial(), OperationTimeout: time.Second})
			require.NoError(t, err)
			t.Cleanup(
				// closeAutomaticSession joins retained native ownership on assertion failures too.
				//
				// Example: a rejected replacement cannot escape the fixture.
				func() { require.NoError(t, transport.Close()) })
			_, err = transport.Execute(t.Context(), owonprotocol.Command{Text: "FIRST", ResponseMode: owonprotocol.ResponseModeASCII})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Len(t, opener.Configs, 1)
			require.Equal(t, "*IDN?\nFIRST\n", string(first.Written))

			for _, candidate := range []struct {
				Descriptor string
				Identity   string
				Writes     string
			}{
				{Descriptor: "replacement", Identity: "replacement"},
				{Descriptor: "scope", Identity: "replacement", Writes: "*IDN?\n"},
				{Descriptor: "scope", Identity: "scope", Writes: "*IDN?\nNEXT\n"},
			} {
				endpoint := &timedBulkEndpoint{ReadChunks: [][]byte{[]byte("OWON,HDS2202S," + candidate.Identity + ",1.0\n"), []byte("ready\n")}, QuietProbe: true}
				session, err := newEndpointSession(endpoint, endpoint, 1024)
				require.NoError(t, err)
				opener.Descriptors = []usbSerialDescriptor{fakeUSBSerialDescriptor{Serial: candidate.Descriptor}}
				opener.Resources = &usbResources{session: session}
				response, err := transport.Execute(t.Context(), owonprotocol.Command{Text: "NEXT", ResponseMode: owonprotocol.ResponseModeASCII})
				require.Equal(t, candidate.Writes, string(endpoint.Written))
				require.Equal(t, owonmodel.SerialNumber("scope"), backend.Serial())
				require.Equal(t, owonmodel.SerialNumber("scope"), opener.Configs[len(opener.Configs)-1].Serial)
				if candidate.Identity != "scope" {
					require.Error(t, err)
					require.Nil(t, response)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, "ready", string(response))
			}
		})
}
