package owonusb

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestEndpointDecoderOwnsAccumulation prevents USB from keeping a second growing frame buffer.
//
// Example: a large fragmented binary payload leaves no unconsumed bytes before each next read.
func TestEndpointDecoderOwnsAccumulation(t *testing.T) {
	for _, mode := range []owonprotocol.ResponseMode{owonprotocol.ResponseModeASCII, owonprotocol.ResponseModeLengthPrefixed} {
		payload := bytes.Repeat([]byte{'x'}, endpointReadBytes*8)
		frame := append(bytes.Clone(payload), '\n')
		if mode == owonprotocol.ResponseModeLengthPrefixed {
			frame = append(make([]byte, 4), payload...)
			binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
		}
		reader := &accumulationEndpoint{Frame: frame}
		session, err := newEndpointSession(reader, new(endpointWriter), uint32(len(payload)))
		require.NoError(t, err)
		reader.Session = session
		response, err := session.Exchange(t.Context(), owonprotocol.Command{Text: ":DATA?", ResponseMode: mode})
		require.NoError(t, err)
		require.Equal(t, payload, response)
		require.Zero(t, reader.MaximumBuffered)
		require.LessOrEqual(t, reader.MaximumCapacity, endpointReadBytes)
		require.Empty(t, session.buffer)
		require.Empty(t, reader.Frame)
	}
}

// accumulationEndpoint observes USB buffer ownership while returning packet-sized fragments.
//
// Example: the next read records whether the previous fragment was fully handed to the decoder.
type accumulationEndpoint struct {
	Frame           []byte
	Session         *endpointSession
	MaximumBuffered int
	MaximumCapacity int
}

// ReadContext delivers the next bounded transfer and records live USB storage between fragments.
//
// Example: framing bytes cannot cause endpoint buffers to grow with payload size.
func (reader *accumulationEndpoint) ReadContext(
	_ context.Context,
	destination []byte,
) (int, error) {
	reader.MaximumBuffered = max(reader.MaximumBuffered, len(reader.Session.buffer))
	reader.MaximumCapacity = max(reader.MaximumCapacity, cap(reader.Session.buffer))
	if len(reader.Frame) == 0 {
		return 0, io.EOF
	}
	count := copy(destination, reader.Frame)
	reader.Frame = reader.Frame[count:]
	return count, nil
}

// TestEndpointBinaryFrameDefersOnlySingleEOF applies native completion policy to empty and full frames.
//
// Example: an empty response with wrapped EOF completes once, while an unrelated error fails immediately.
func TestEndpointBinaryFrameDefersOnlySingleEOF(t *testing.T) {
	for _, frame := range [][]byte{{0, 0, 0, 0}, {2, 0, 0, 0, 'O', 'K'}} {
		for _, cause := range []error{io.EOF, fmt.Errorf("native: %w", io.EOF), io.ErrUnexpectedEOF} {
			verifyBinaryFrameCompletion(t, frame, cause)
		}
	}
}

// verifyBinaryFrameCompletion checks completion, retained causes, and no second write after EOF.
//
// Example: a two-byte payload with a native non-EOF failure must not be returned as success.
func verifyBinaryFrameCompletion(
	t *testing.T,
	frame []byte,
	cause error,
) {
	t.Helper()
	reader := cancellationEndpoint{Cause: cause, Data: frame}
	writer := new(endpointWriter)
	session, err := newEndpointSession(reader, writer, 4)
	require.NoError(t, err)
	command := owonprotocol.Command{Text: ":DATA?", ResponseMode: owonprotocol.ResponseModeLengthPrefixed}
	response, err := session.Exchange(t.Context(), command)
	if cause == io.ErrUnexpectedEOF {
		require.Nil(t, response)
		require.ErrorIs(t, err, cause)
		return
	}
	require.NoError(t, err)
	require.Equal(t, string(frame[4:]), string(response))
	response, err = session.Exchange(t.Context(), command)
	require.Nil(t, response)
	require.ErrorIs(t, err, cause)
	requireErrorType[*owonprotocol.ErrSurplusResponse](t, err)
	require.Equal(t, []byte(":DATA?\n"), writer.Data)
}
