package owonprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFrameDecoderEverySplit verifies completion independently from payload and fragment size.
//
// Example: an empty binary frame completes after its fourth prefix byte, with surplus untouched.
func TestFrameDecoderEverySplit(t *testing.T) {
	t.Parallel()
	for _, test := range []frameFixture{
		{ResponseModeASCII, []byte("\n"), nil},
		{ResponseModeASCII, []byte("\r\n"), nil},
		{ResponseModeASCII, []byte("abc\r\n"), []byte("abc")},
		{ResponseModeASCII, []byte("a\rb\n"), []byte("a\rb")},
		{ResponseModeLengthPrefixed, []byte{0, 0, 0, 0}, nil},
		{ResponseModeLengthPrefixed, []byte{3, 0, 0, 0, 'a', '\n', 'b'}, []byte("a\nb")},
	} {
		verifyFrameSplits(t, test)
	}
}

// frameFixture describes independent wire bytes and the expected decoded value.
//
// Example: CRLF framing includes CR in Frame but excludes it from Payload.
type frameFixture struct {
	Mode    ResponseMode
	Frame   []byte
	Payload []byte
}

// verifyFrameSplits exercises every three-fragment partition, including empty fragments.
//
// Example: each possible split of the binary length prefix is covered.
func verifyFrameSplits(
	t *testing.T,
	test frameFixture,
) {
	t.Helper()
	for first := 0; first <= len(test.Frame); first++ {
		for second := first; second <= len(test.Frame); second++ {
			verifyFramePartition(t, test, first, second)
		}
	}
}

// verifyFramePartition checks exact consumption, ownership, and stable completion for one split.
//
// Example: clearing caller-owned fragments cannot change the decoder's completed payload.
func verifyFramePartition(
	t *testing.T,
	test frameFixture,
	first int,
	second int,
) {
	t.Helper()
	decoder, err := NewFrameDecoder(test.Mode, 4)
	require.NoError(t, err)
	_, err = decoder.Payload()
	requireErrorType[*ErrMalformedResponse](t, err)
	frame := bytes.Clone(test.Frame)
	fragments := [][]byte{frame[:first], frame[first:second], append(bytes.Clone(frame[second:]), 'x', 'y')}
	total := 0
	for index, fragment := range fragments {
		progress, err := decoder.Feed(fragment)
		require.NoError(t, err)
		wantConsumed := len(fragment)
		if index == len(fragments)-1 {
			wantConsumed -= 2
		}
		require.Equal(t, wantConsumed, progress.Consumed)
		total += progress.Consumed
		require.Equal(t, total == len(test.Frame), progress.Complete)
		clear(fragment[:progress.Consumed])
	}
	payload, err := decoder.Payload()
	require.NoError(t, err)
	require.Equal(t, string(test.Payload), string(payload))
	progress, err := decoder.Feed([]byte("next"))
	require.NoError(t, err)
	require.Equal(t, FrameProgress{Complete: true}, progress)
	again, err := decoder.Payload()
	require.NoError(t, err)
	require.Equal(t, payload, again)
	if len(payload) > 0 {
		require.Same(t, &payload[0], &again[0])
	}
}

// TestFrameDecoderUninitialized rejects missing constructor state without panicking.
//
// Example: both a nil decoder and a zero-valued decoder reject Feed and Payload.
func TestFrameDecoderUninitialized(t *testing.T) {
	for _, decoder := range []*FrameDecoder{nil, new(FrameDecoder)} {
		payload, err := decoder.Payload()
		require.Nil(t, payload)
		requireErrorType[*ErrMalformedResponse](t, err)
		progress, err := decoder.Feed([]byte("x\n"))
		require.Equal(t, FrameProgress{}, progress)
		requireErrorType[*ErrMalformedResponse](t, err)
	}
}

// TestFrameDecoderBounds verifies limits include CR and reject the prefix before body allocation.
//
// Example: four pre-LF bytes fit a limit of four; a fifth byte cannot enter decoder storage.
func TestFrameDecoderBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		Mode     ResponseMode
		Frame    []byte
		Want     string
		TooLarge bool
	}{
		{ResponseModeASCII, []byte("abcd\n"), "abcd", false},
		{ResponseModeASCII, []byte("abc\r\n"), "abc", false},
		{ResponseModeASCII, []byte("abcd\r\n"), "", true},
		{ResponseModeASCII, []byte("abcde"), "", true},
		{ResponseModeLengthPrefixed, []byte{4, 0, 0, 0, 'a', 'b', 'c', 'd'}, "abcd", false},
		{ResponseModeLengthPrefixed, []byte{5, 0, 0, 0, 'a', 'b'}, "", true},
		{ResponseModeLengthPrefixed, []byte{255, 255, 255, 255}, "", true},
	} {
		decoder, err := NewFrameDecoder(test.Mode, 4)
		require.NoError(t, err)
		progress, err := decoder.Feed(test.Frame)
		if test.TooLarge {
			requireErrorType[*ErrResponseTooLarge](t, err)
			require.False(t, progress.Complete)
			_, payloadErr := decoder.Payload()
			require.ErrorIs(t, payloadErr, err)
			_, feedErr := decoder.Feed([]byte("\n"))
			require.ErrorIs(t, feedErr, err)
			if test.Mode == ResponseModeLengthPrefixed {
				require.Equal(t, 4, progress.Consumed)
				require.Zero(t, cap(decoder.payload))
			}
			continue
		}
		require.NoError(t, err)
		require.True(t, progress.Complete)
		payload, err := decoder.Payload()
		require.NoError(t, err)
		require.Equal(t, test.Want, string(payload))
	}
}

// TestFrameDecoderConfiguration rejects non-frame modes and impossible allocation bounds.
//
// Example: a setter's no-response policy is not a frame grammar.
func TestFrameDecoderConfiguration(t *testing.T) {
	t.Parallel()
	for _, mode := range []ResponseMode{ResponseModeUnspecified, ResponseModeNone, ResponseMode(99)} {
		decoder, err := NewFrameDecoder(mode, 4)
		require.Nil(t, decoder)
		requireErrorType[*ErrMalformedResponse](t, err)
	}
	decoder, err := NewFrameDecoder(ResponseModeASCII, DefaultMaximumResponseBytes+1)
	require.Nil(t, decoder)
	requireErrorType[*ErrInvalidResponseLimit](t, err)
	decoder, err = NewFrameDecoder(ResponseModeASCII, 0)
	require.NoError(t, err)
	require.Equal(t, DefaultMaximumResponseBytes, decoder.maximumBytes)
}

// TestFrameDecoderFragmentedStorageBounds measures actual reallocations under one-byte fragmentation.
//
// Example: ASCII growth copies fewer than twice the payload bytes; binary allocates exactly once.
func TestFrameDecoderFragmentedStorageBounds(t *testing.T) {
	t.Parallel()
	for _, mode := range []ResponseMode{ResponseModeASCII, ResponseModeLengthPrefixed} {
		for _, size := range []int{1, 257, 65537} {
			verifyFragmentedStorage(t, mode, size)
		}
	}
}

// verifyFragmentedStorage counts growth work and enforces the allocation cap after every byte.
//
// Example: a non-power-of-two limit also caps the final geometrically grown allocation.
func verifyFragmentedStorage(
	t *testing.T,
	mode ResponseMode,
	size int,
) {
	t.Helper()
	decoder, err := NewFrameDecoder(mode, uint32(size))
	require.NoError(t, err)
	frame := bytes.Repeat([]byte{'x'}, size)
	if mode == ResponseModeASCII {
		frame = append(frame, '\n')
	} else {
		frame = append(make([]byte, 4), frame...)
		binary.LittleEndian.PutUint32(frame, uint32(size))
	}
	copied, previousCapacity, allocations := 0, 0, 0
	for offset := range frame {
		previousLength := len(decoder.payload)
		progress, err := decoder.Feed(frame[offset : offset+1])
		require.NoError(t, err)
		require.Equal(t, 1, progress.Consumed)
		require.Equal(t, offset == len(frame)-1, progress.Complete)
		require.LessOrEqual(t, cap(decoder.payload), size)
		if cap(decoder.payload) != previousCapacity {
			copied += previousLength
			allocations++
			previousCapacity = cap(decoder.payload)
		}
	}
	require.Less(t, copied, 2*size)
	if mode == ResponseModeLengthPrefixed {
		require.Equal(t, 1, allocations)
	}
	payload, err := decoder.Payload()
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'x'}, size), payload)
}

// BenchmarkFrameDecoderFragmentedASCII exposes scaling of byte-at-a-time scans and copies.
//
// Example: increasing frame size fourfold should increase work proportionally, not quadratically.
func BenchmarkFrameDecoderFragmentedASCII(b *testing.B) {
	for _, size := range []int{4096, 16384, 65536} {
		b.Run(fmt.Sprint(size),
			// decodeFragmented feeds the worst-case fragmentation without reader or USB costs.
			//
			// Example: allocation reporting includes geometric payload growth.
			func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					decoder, err := NewFrameDecoder(ResponseModeASCII, uint32(size))
					require.NoError(b, err)
					for range size {
						_, err = decoder.Feed([]byte{'x'})
						require.NoError(b, err)
					}
					progress, err := decoder.Feed([]byte{'\n'})
					require.NoError(b, err)
					require.True(b, progress.Complete)
				}
			})
	}
}
