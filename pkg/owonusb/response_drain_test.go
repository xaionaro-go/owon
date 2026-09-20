package owonusb

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/gousb"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

// TestEndpointSessionDrainPendingReportsDiscardedBytes verifies bounded stale-byte isolation.
//
// Example: a buffered prefix and one 604-byte frame are discarded before the own probe proves quiet.
func TestEndpointSessionDrainPendingReportsDiscardedBytes(t *testing.T) {
	synctest.Test(t,
		// verifyDrainReport exercises direct transport ownership without parsing or writing a command.
		//
		// Example: the report counts both pre-existing buffered bytes and fresh endpoint transfers.
		func(t *testing.T) {
			reader := &drainScriptEndpoint{
				Chunks: [][]byte{lengthPrefixedFixture(600)},
				Wait:   true,
				Err:    gousb.TransferCancelled,
			}
			writer := new(endpointWriter)
			session, err := newEndpointSession(reader, writer, 1024)
			require.NoError(t, err)
			session.buffer = []byte("old")

			report, err := session.drainPending(t.Context())
			require.NoError(t, err)
			require.Equal(t, responseDrainReport{DiscardedBytes: 607, ReadCalls: 2}, report)
			require.Empty(t, session.buffer)
			require.Empty(t, writer.Data, "draining must not write or replay a command")
		})
}

// TestEndpointSessionDrainPendingRejectsUnsafeReadOutcomes verifies fail-closed probe classification.
//
// Example: EOF, mixed causes, bytes with an error, no progress, invalid counts, and parent cancellation all reject.
func TestEndpointSessionDrainPendingRejectsUnsafeReadOutcomes(t *testing.T) {
	tests := []struct {
		Name   string
		Reader func(context.Context) *drainScriptEndpoint
	}{
		{
			Name: "eof",
			Reader: func(context.Context) *drainScriptEndpoint {
				return &drainScriptEndpoint{Err: io.EOF}
			},
		},
		{
			Name: "mixed cancellation",
			Reader: func(context.Context) *drainScriptEndpoint {
				return &drainScriptEndpoint{Err: errors.Join(gousb.TransferCancelled, errors.New("device failure"))}
			},
		},
		{
			Name: "bytes with error",
			Reader: func(context.Context) *drainScriptEndpoint {
				return &drainScriptEndpoint{Chunks: [][]byte{[]byte("stale")}, ChunkErr: gousb.TransferCancelled}
			},
		},
		{
			Name: "no progress",
			Reader: func(context.Context) *drainScriptEndpoint {
				return &drainScriptEndpoint{}
			},
		},
		{
			Name: "invalid count",
			Reader: func(context.Context) *drainScriptEndpoint {
				return &drainScriptEndpoint{Count: endpointReadBytes + 1}
			},
		},
	}
	synctest.Test(t,
		// verifyDrainFailures keeps every unsafe outcome in one deterministic simulated clock.
		//
		// Example: every rejected candidate remains write-free.
		func(t *testing.T) {
			for _, test := range tests {
				reader := test.Reader(t.Context())
				writer := new(endpointWriter)
				session, err := newEndpointSession(reader, writer, 1024)
				require.NoError(t, err, test.Name)

				_, err = session.drainPending(t.Context())
				require.Error(t, err, test.Name)
				require.Empty(t, writer.Data, test.Name)
			}
		})

	synctest.Test(t,
		// verifyParentCancellation distinguishes a live probe timeout from a caller that ended first.
		//
		// Example: native TransferCancelled does not hide parent context cancellation.
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			reader := &drainScriptEndpoint{Cancel: cancel, Err: gousb.TransferCancelled}
			session, err := newEndpointSession(reader, new(endpointWriter), 1024)
			require.NoError(t, err)
			_, err = session.drainPending(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})

	synctest.Test(t,
		// verifyParentDeadline distinguishes the recovery budget from the five-millisecond own probe.
		//
		// Example: an earlier parent deadline cannot be treated as candidate quiet.
		func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
			defer cancel()
			reader := &drainScriptEndpoint{Wait: true, Err: gousb.TransferCancelled}
			session, err := newEndpointSession(reader, new(endpointWriter), 1024)
			require.NoError(t, err)
			_, err = session.drainPending(ctx)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
}

// TestEndpointSessionDrainPendingRejectsQueueAboveCap verifies the maximum-plus-prefix bound.
//
// Example: a configured eight-byte response limit permits twelve drained bytes but rejects the thirteenth.
func TestEndpointSessionDrainPendingRejectsQueueAboveCap(t *testing.T) {
	reader := &drainScriptEndpoint{Chunks: [][]byte{bytesOfLength(13)}}
	session, err := newEndpointSession(reader, new(endpointWriter), 8)
	require.NoError(t, err)

	report, err := session.drainPending(t.Context())
	require.Error(t, err)
	require.Equal(t, responseDrainReport{DiscardedBytes: 13, ReadCalls: 1}, report)
	require.Contains(t, err.Error(), "exceeds")
}

// TestBackendRecoveryDrainsStaleFrameBeforeIdentityAndPendingOnce verifies recovery ordering.
//
// Example: a stale 604-byte frame is discarded, identity is written once, and the pending command is written once.
func TestBackendRecoveryDrainsStaleFrameBeforeIdentityAndPendingOnce(t *testing.T) {
	synctest.Test(t,
		// verifyRecoveryDrainOrdering drives a poisoned session through real recovery admission.
		//
		// Example: no failed predecessor command is replayed on the replacement endpoint.
		func(t *testing.T) {
			oldEndpoint := new(timedBulkEndpoint)
			oldSession, err := newEndpointSession(oldEndpoint, oldEndpoint, 1024)
			require.NoError(t, err)
			candidateEndpoint := &drainRecoveryEndpoint{Steps: []drainRecoveryStep{
				{Data: lengthPrefixedFixture(600)},
				{Wait: true, Err: gousb.TransferCancelled},
				{Data: []byte("OWON,HDS2202S,serial,1.0\n")},
				{Data: []byte("ready\n")},
			}}
			candidateSession, err := newEndpointSession(candidateEndpoint, candidateEndpoint, 1024)
			require.NoError(t, err)
			opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{{session: candidateSession}}}
			backend := &Backend{
				config:    (Config{Serial: "serial", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024, OperationTimeout: time.Second}).withDefaults(),
				opener:    opener,
				resources: &usbResources{session: oldSession},
			}
			transport, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: owonmodel.SerialNumber("serial"), OperationTimeout: time.Second})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, transport.Close()) })

			_, err = transport.Execute(t.Context(), owonprotocol.Command{Text: "FAILED", ResponseMode: owonprotocol.ResponseModeASCII})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			response, err := transport.Execute(t.Context(), owonprotocol.Command{Text: "PENDING", ResponseMode: owonprotocol.ResponseModeASCII})
			require.NoError(t, err)
			require.Equal(t, []byte("ready"), response)
			require.Equal(t, "*IDN?\nPENDING\n", string(candidateEndpoint.Written))
			require.Equal(t, 4, candidateEndpoint.ReadCalls)
			require.Equal(t, 1, opener.Calls)
		})
}

// TestBackendRecoveryRejectsFrameArrivingAfterQuietWindow verifies bounded isolation is not a barrier.
//
// Example: a late stale frame poisons identity validation and prevents the pending command write.
func TestBackendRecoveryRejectsFrameArrivingAfterQuietWindow(t *testing.T) {
	synctest.Test(t,
		// verifyLateFrameRecovery preserves the existing poison rule after a clean quiet probe.
		//
		// Example: the candidate identity query is attempted once, but the pending command is never admitted.
		func(t *testing.T) {
			oldEndpoint := new(timedBulkEndpoint)
			oldSession, err := newEndpointSession(oldEndpoint, oldEndpoint, 1024)
			require.NoError(t, err)
			candidateEndpoint := &drainRecoveryEndpoint{Steps: []drainRecoveryStep{
				{Wait: true, Err: gousb.TransferCancelled},
				{Data: lengthPrefixedFixture(600)},
				{Err: io.EOF},
			}}
			candidateSession, err := newEndpointSession(candidateEndpoint, candidateEndpoint, 1024)
			require.NoError(t, err)
			opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{{session: candidateSession}}}
			backend := &Backend{
				config:    (Config{Serial: "serial", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024, OperationTimeout: time.Second}).withDefaults(),
				opener:    opener,
				resources: &usbResources{session: oldSession},
			}
			transport, err := owonsession.New(backend, owonsession.Config{ExpectedSerial: owonmodel.SerialNumber("serial"), OperationTimeout: time.Second})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, transport.Close()) })

			_, err = transport.Execute(t.Context(), owonprotocol.Command{Text: "FAILED", ResponseMode: owonprotocol.ResponseModeASCII})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			_, err = transport.Execute(t.Context(), owonprotocol.Command{Text: "PENDING", ResponseMode: owonprotocol.ResponseModeASCII})
			require.Error(t, err)
			require.Equal(t, "*IDN?\n", string(candidateEndpoint.Written))
			require.Equal(t, 1, opener.Calls)
			_, err = transport.Begin(t.Context())
			require.ErrorContains(t, err, "recover poisoned session")
			require.Equal(t, 2, opener.Calls)
		})
}

// TestBackendRecoveryRetainsIdentityChecksAfterCleanDrain verifies descriptor and SCPI rejection paths.
//
// Example: a clean drain cannot authorize a wrong descriptor serial or a non-OWON identity.
func TestBackendRecoveryRetainsIdentityChecksAfterCleanDrain(t *testing.T) {
	synctest.Test(t,
		// verifyIdentityChecksAfterDrain checks serial selection before drain and SCPI checks after it.
		//
		// Example: neither rejected candidate emits a pending user command.
		func(t *testing.T) {
			wrongDescriptorEndpoint := &drainRecoveryEndpoint{}
			wrongDescriptorSession, err := newEndpointSession(wrongDescriptorEndpoint, wrongDescriptorEndpoint, 1024)
			require.NoError(t, err)
			wrongDescriptor := &usbResources{serial: "other", session: wrongDescriptorSession}
			backend := &Backend{
				config: (Config{Serial: "serial", ExpectedModel: DefaultModel, MaximumResponseBytes: 1024}).withDefaults(),
				opener: &scriptedUSBResourcesOpener{Resources: []*usbResources{wrongDescriptor}},
			}
			err = backend.ReopenAndValidate(t.Context(), owonmodel.SerialNumber("serial"))
			require.ErrorContains(t, err, "does not match")
			require.Empty(t, wrongDescriptorEndpoint.Written)
			require.Zero(t, wrongDescriptorEndpoint.ReadCalls)

			wrongIdentityEndpoint := &drainRecoveryEndpoint{Steps: []drainRecoveryStep{
				{Wait: true, Err: gousb.TransferCancelled},
				{Data: []byte("OTHER,HDS2202S,serial,1.0\n")},
			}}
			wrongIdentitySession, err := newEndpointSession(wrongIdentityEndpoint, wrongIdentityEndpoint, 1024)
			require.NoError(t, err)
			backend.opener = &scriptedUSBResourcesOpener{Resources: []*usbResources{{session: wrongIdentitySession}}}
			err = backend.ReopenAndValidate(t.Context(), owonmodel.SerialNumber("serial"))
			require.ErrorContains(t, err, "manufacturer")
			require.Equal(t, "*IDN?\n", string(wrongIdentityEndpoint.Written))
		})
}

// TestBackendRecoveryDrainCloseFailureRetainsCandidate verifies cleanup ownership after preflight failure.
//
// Example: a busy rejected candidate blocks another opener until cleanup succeeds.
func TestBackendRecoveryDrainCloseFailureRetainsCandidate(t *testing.T) {
	synctest.Test(t,
		// verifyDrainCloseOwnership checks rejectResources through a cap failure and two cleanup generations.
		//
		// Example: the second opener call occurs only after the busy candidate closes successfully.
		func(t *testing.T) {
			busy := errors.New("candidate busy")
			device := &orderedUSBCloser{Name: "candidate", Order: new([]string), Err: busy}
			failedEndpoint := &drainRecoveryEndpoint{Steps: []drainRecoveryStep{{Data: bytesOfLength(13)}}}
			failedSession, err := newEndpointSession(failedEndpoint, failedEndpoint, 8)
			require.NoError(t, err)
			goodEndpoint := &drainRecoveryEndpoint{Steps: []drainRecoveryStep{
				{Wait: true, Err: gousb.TransferCancelled},
				{Data: []byte("OWON,HDS2202S,serial,1.0\n")},
			}}
			goodSession, err := newEndpointSession(goodEndpoint, goodEndpoint, 1024)
			require.NoError(t, err)
			failed := &usbResources{serial: "serial", device: device, session: failedSession}
			good := &usbResources{serial: "serial", session: goodSession}
			opener := &scriptedUSBResourcesOpener{Resources: []*usbResources{failed}}
			backend := &Backend{config: (Config{Serial: "serial", ExpectedModel: DefaultModel, MaximumResponseBytes: 8}).withDefaults(), opener: opener}

			err = backend.ReopenAndValidate(t.Context(), owonmodel.SerialNumber("serial"))
			require.ErrorContains(t, err, "exceeds")
			require.ErrorIs(t, err, busy)
			require.Same(t, failed, backend.resources)
			require.Equal(t, 1, opener.Calls)

			err = backend.ReopenAndValidate(t.Context(), owonmodel.SerialNumber("serial"))
			require.ErrorIs(t, err, busy)
			require.Equal(t, 1, opener.Calls)

			device.Err = nil
			opener.Resources = append(opener.Resources, good)
			require.NoError(t, backend.ReopenAndValidate(t.Context(), owonmodel.SerialNumber("serial")))
			require.Same(t, good, backend.resources)
			require.Equal(t, 2, opener.Calls)
		})
}

// lengthPrefixedFixture builds one little-endian stale response without consulting the decoder.
//
// Example: lengthPrefixedFixture(600) represents the observed 604-byte transfer.
func lengthPrefixedFixture(payloadLength int) []byte {
	result := make([]byte, 4+payloadLength)
	binary.LittleEndian.PutUint32(result, uint32(payloadLength))
	for index := 4; index < len(result); index++ {
		result[index] = 'x'
	}

	return result
}

// bytesOfLength returns deterministic non-framing bytes for cap tests.
//
// Example: bytesOfLength(13) creates a queue one byte above a twelve-byte drain cap.
func bytesOfLength(length int) []byte {
	result := make([]byte, length)
	for index := range result {
		result[index] = 'x'
	}

	return result
}

// cleanIdentityRecoveryEndpoint creates a candidate with one quiet preflight and one identity line.
//
// Example: legacy identity fixtures can exercise the new recovery ordering without physical USB.
func cleanIdentityRecoveryEndpoint(identity string) *drainRecoveryEndpoint {
	return &drainRecoveryEndpoint{Steps: []drainRecoveryStep{
		{Wait: true, Err: gousb.TransferCancelled},
		{Data: []byte(identity)},
	}}
}

// drainScriptEndpoint supplies immediate bytes and a controlled terminal read outcome.
//
// Example: Wait=true plus TransferCancelled models gousb's own quiet-probe completion.
type drainScriptEndpoint struct {
	Chunks    [][]byte
	ChunkErr  error
	Count     int
	Err       error
	Wait      bool
	Cancel    context.CancelFunc
	ReadCalls int
}

// ReadContext returns one scripted chunk or its terminal outcome.
//
// Example: a terminal read waits for the probe deadline before returning TransferCancelled.
func (endpoint *drainScriptEndpoint) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	endpoint.ReadCalls++
	if len(endpoint.Chunks) != 0 {
		chunk := endpoint.Chunks[0]
		endpoint.Chunks = endpoint.Chunks[1:]
		count := copy(destination, chunk)
		if endpoint.ChunkErr != nil {
			return count, endpoint.ChunkErr
		}

		return count, nil
	}
	if endpoint.Cancel != nil {
		endpoint.Cancel()
	}
	if endpoint.Wait {
		<-ctx.Done()
	}

	return endpoint.Count, endpoint.Err
}

// WriteContext rejects drain attempts that accidentally reach the output endpoint.
//
// Example: a drain has no command payload and therefore cannot perform a write.
func (*drainScriptEndpoint) WriteContext(
	context.Context,
	[]byte,
) (int, error) {
	return 0, errors.New("drain test writer must not be called")
}

// drainRecoveryStep describes one candidate read during recovery and identity validation.
//
// Example: a waiting TransferCancelled step is the five-millisecond preflight quiet probe.
type drainRecoveryStep struct {
	Data []byte
	Err  error
	Wait bool
}

// drainRecoveryEndpoint separates preflight reads from identity and pending-command reads.
//
// Example: the endpoint can place a stale frame before a quiet probe and a valid identity after it.
type drainRecoveryEndpoint struct {
	Steps     []drainRecoveryStep
	Written   []byte
	ReadCalls int
}

// ReadContext returns the next scripted recovery step and honors its optional wait.
//
// Example: an empty script blocks until its operation context ends, modeling silence after setup.
func (endpoint *drainRecoveryEndpoint) ReadContext(
	ctx context.Context,
	destination []byte,
) (int, error) {
	endpoint.ReadCalls++
	if len(endpoint.Steps) == 0 {
		<-ctx.Done()
		return 0, gousb.TransferCancelled
	}
	step := endpoint.Steps[0]
	endpoint.Steps = endpoint.Steps[1:]
	if step.Wait {
		<-ctx.Done()
		if step.Err == nil {
			return 0, ctx.Err()
		}
	}
	count := copy(destination, step.Data)
	return count, step.Err
}

// WriteContext records candidate identity and pending command bytes.
//
// Example: exact writes prove the failed predecessor is not replayed after recovery.
func (endpoint *drainRecoveryEndpoint) WriteContext(
	ctx context.Context,
	source []byte,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	endpoint.Written = append(endpoint.Written, source...)

	return len(source), nil
}
