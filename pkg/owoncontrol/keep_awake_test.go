package owoncontrol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// TestEnsureKeepAwakeRefreshesAlreadyUnlimited verifies the startup refresh sequence.
//
// Example: an exact Unlimited preflight still gets one setter and a fresh Unlimited readback.
func TestEnsureKeepAwakeRefreshesAlreadyUnlimited(t *testing.T) {
	backend := &dmmSequenceBackend{Responses: map[string][][]byte{
		":SHUTdown:TIMe?": {[]byte("Unlimited"), []byte("Unlimited")},
	}}
	controller := newTestInstrument(t, backend)

	require.NoError(t, controller.EnsureKeepAwake(t.Context()))
	require.Equal(t, []string{":SHUTdown:TIMe?", ":SHUTdown:TIMe UNLIMITED", ":SHUTdown:TIMe?"}, backend.Commands)
}

// TestEnsureKeepAwakeWritesFiniteTimerOnceAndVerifiesReadback verifies the bounded setter sequence.
//
// Example: a 30min preflight becomes Unlimited after exactly one write and one readback.
func TestEnsureKeepAwakeWritesFiniteTimerOnceAndVerifiesReadback(t *testing.T) {
	backend := &dmmSequenceBackend{Responses: map[string][][]byte{
		":SHUTdown:TIMe?": {[]byte("30min"), []byte("Unlimited")},
	}}
	controller := newTestInstrument(t, backend)

	require.NoError(t, controller.EnsureKeepAwake(t.Context()))
	require.Equal(t, []string{":SHUTdown:TIMe?", ":SHUTdown:TIMe UNLIMITED", ":SHUTdown:TIMe?"}, backend.Commands)
}

// TestEnsureKeepAwakeRejectsMalformedPreflightWithoutSetter verifies fail-closed admission.
//
// Example: an unknown timer token cannot authorize a mutation command.
func TestEnsureKeepAwakeRejectsMalformedPreflightWithoutSetter(t *testing.T) {
	backend := &scriptedBackend{Responses: map[string][]byte{":SHUTdown:TIMe?": []byte("unexpected")}}
	controller := newTestInstrument(t, backend)

	err := controller.EnsureKeepAwake(t.Context())
	var malformed *owonscpi.ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
	require.Equal(t, []string{":SHUTdown:TIMe?"}, backend.Commands)
}

// TestEnsureKeepAwakeDoesNotReplaySetterAfterTransportFailure verifies no-replay semantics.
//
// Example: an ambiguous setter exchange returns without a second setter or readback query.
func TestEnsureKeepAwakeDoesNotReplaySetterAfterTransportFailure(t *testing.T) {
	backend := &scriptedBackend{
		ExchangeErrors: []error{nil, errors.New("setter exchange lost")},
		Responses:      map[string][]byte{":SHUTdown:TIMe?": []byte("30min")},
	}
	controller := newTestInstrument(t, backend)

	require.Error(t, controller.EnsureKeepAwake(t.Context()))
	require.Equal(t, []string{":SHUTdown:TIMe?", ":SHUTdown:TIMe UNLIMITED"}, backend.Commands)
}

// TestEnsureKeepAwakeRejectsUnexpectedReadback verifies that a successful write still needs proof.
//
// Example: a finite post-write token is a failed policy result, not an inferred success.
func TestEnsureKeepAwakeRejectsUnexpectedReadback(t *testing.T) {
	backend := &dmmSequenceBackend{Responses: map[string][][]byte{
		":SHUTdown:TIMe?": {[]byte("30min"), []byte("60min")},
	}}
	controller := newTestInstrument(t, backend)

	err := controller.EnsureKeepAwake(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutdown timer")
	require.Equal(t, []string{":SHUTdown:TIMe?", ":SHUTdown:TIMe UNLIMITED", ":SHUTdown:TIMe?"}, backend.Commands)
}
