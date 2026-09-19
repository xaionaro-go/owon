package owonprotocol

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadResponseDecodesSupportedFrames verifies both documented OWON response forms.
//
// Example: ASCII lines lose CRLF while binary frames lose their length prefix.
func TestReadResponseDecodesSupportedFrames(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"MODEL":"HDS2202S_LS"}`)
	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)

	ascii, err := ReadResponse(bytes.NewBufferString("OWON,HDS2202S\r\n"), ResponseModeASCII, 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("OWON,HDS2202S"), ascii)
	binary, err := ReadResponse(bytes.NewReader(frame), ResponseModeLengthPrefixed, 1024)
	require.NoError(t, err)
	require.Equal(t, payload, binary)
}

// TestReadResponseLeavesFollowingASCIIFrame verifies the helper never consumes a future reply.
//
// Example: decoding `first` leaves `second` readable from the caller-owned stream.
func TestReadResponseLeavesFollowingASCIIFrame(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("first\nsecond\n"))
	response, err := ReadResponse(reader, ResponseModeASCII, 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("first"), response)
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("second\n"), remaining)
}

// TestReadResponseLeavesFollowingBinaryFrame verifies exact binary reads do not read ahead.
//
// Example: decoding a three-byte payload leaves a following four-byte frame untouched.
func TestReadResponseLeavesFollowingBinaryFrame(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte{3, 0, 0, 0, 'a', 'b', 'c', 1, 0, 0, 0, 'z'})
	response, err := ReadResponse(reader, ResponseModeLengthPrefixed, 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("abc"), response)
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0, 0, 0, 'z'}, remaining)
}

// TestReadResponseRejectsMalformedAndOversizedFrames verifies defensive frame bounds.
//
// Example: a declared payload larger than the limit fails before allocating it.
func TestReadResponseRejectsMalformedAndOversizedFrames(t *testing.T) {
	t.Parallel()

	_, err := ReadResponse(bytes.NewReader([]byte{5, 0, 0, 0}), ResponseModeLengthPrefixed, 4)
	var tooLarge *ErrResponseTooLarge
	require.ErrorAs(t, err, &tooLarge)
	require.Equal(t, uint64(5), tooLarge.Size)
	require.Equal(t, uint64(4), tooLarge.Limit)
	_, err = ReadResponse(bytes.NewBufferString("unterminated"), ResponseModeASCII, 1024)
	var malformed *ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
	require.ErrorIs(t, err, io.EOF)
}

// TestReadResponseOversizedPrefixLeavesBodyUnread verifies rejection precedes body access.
//
// Example: a seventeen-MiB declaration leaves every following sentinel byte in the reader.
func TestReadResponseOversizedPrefixLeavesBodyUnread(t *testing.T) {
	t.Parallel()

	frame := []byte{1, 0, 0, 1, 'x', 'y'}
	reader := bytes.NewReader(frame)
	_, err := ReadResponse(reader, ResponseModeLengthPrefixed, DefaultMaximumResponseBytes)
	requireErrorType[*ErrResponseTooLarge](t, err)
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, []byte{'x', 'y'}, remaining)
}
