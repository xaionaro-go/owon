package owoncontrol

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// generatorObservationExecutor adapts the deterministic backend exchange to the controller query seam.
//
// Example: direct barrier tests can exercise bounded retries without opening a session transaction.
type generatorObservationExecutor struct {
	backend *dmmSequenceBackend
}

// Execute forwards one query to the deterministic backend.
//
// Example: every CHANNEL? attempt remains visible in the backend command log.
func (executor generatorObservationExecutor) Execute(ctx context.Context, command owonprotocol.Command) ([]byte, error) {
	return executor.backend.Exchange(ctx, command)
}

// TestSetGeneratorObservedRestoresTheLogicalOutputAfterWaveformConvergence verifies operation-local compensation.
//
// Example: an omitted output field preserves OFF while a sine transition settles before its frequency write.
func TestSetGeneratorObservedRestoresTheLogicalOutputAfterWaveformConvergence(t *testing.T) {
	backend := &dmmSequenceBackend{Responses: map[string][][]byte{
		":CHANNEL?":  {[]byte("OFF"), []byte("OFF"), []byte("OFF"), []byte("OFF")},
		":FUNCTION?": {[]byte("SINe"), []byte("SINe")},
	}}
	controller := newTestInstrument(t, backend)
	waveform, frequency := owonmodel.GeneratorWaveformSine, 1000.0

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{
		Waveform:    &waveform,
		FrequencyHz: &frequency,
	})
	require.NoError(t, err)
	require.Equal(t, owonmodel.GeneratorDeliveryTransportCompleteReadbackUnverified, result.Delivery)
	require.Equal(t, []string{
		":CHANNEL?", ":CHANNEL?",
		":FUNCTION SINE", ":FUNCTION?", ":FUNCTION?",
		":FUNCTION:FREQUENCY 1000",
		":CHANNEL OFF", ":CHANNEL?", ":CHANNEL?",
	}, backend.Commands)
	require.Equal(t, owonmodel.GeneratorContextMatch, result.Observation.Context.Match)
	require.NotNil(t, result.Observation.Context.Observed)
	require.Equal(t, owonmodel.GeneratorWaveformSine, *result.Observation.Context.Observed)
	require.Equal(t, owonmodel.GeneratorOutputCompensationVerified, result.Compensation.Status)
	require.NotNil(t, result.Observation.Output.Value)
	require.False(t, *result.Observation.Output.Value)
}

// TestSetGeneratorObservedBlocksDependentWritesUntilTwoMatchingWaveformReplies verifies the convergence barrier.
//
// Example: a stale SQUARE reply delays frequency until two consecutive SINe observations arrive.
func TestSetGeneratorObservedBlocksDependentWritesUntilTwoMatchingWaveformReplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &dmmSequenceBackend{Responses: map[string][][]byte{
			":CHANNEL?":  {[]byte("OFF"), []byte("OFF"), []byte("OFF"), []byte("OFF")},
			":FUNCTION?": {[]byte("SQUARE"), []byte("SINe"), []byte("SINe")},
		}}
		controller := newTestInstrument(t, backend)
		waveform, frequency := owonmodel.GeneratorWaveformSine, 1000.0

		_, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{
			Waveform:    &waveform,
			FrequencyHz: &frequency,
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			":CHANNEL?", ":CHANNEL?",
			":FUNCTION SINE", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?",
			":FUNCTION:FREQUENCY 1000",
			":CHANNEL OFF", ":CHANNEL?", ":CHANNEL?",
		}, backend.Commands)
	})
}

// TestSetGeneratorObservedPreservesUnknownContextAndCompensatesWithoutDependentWrites verifies fail-closed parsing.
//
// Example: an unknown FUNCTION? token retains raw context, restores OFF, and never sends frequency.
func TestSetGeneratorObservedPreservesUnknownContextAndCompensatesWithoutDependentWrites(t *testing.T) {
	backend := &dmmSequenceBackend{Responses: map[string][][]byte{
		":CHANNEL?":  {[]byte("OFF"), []byte("OFF"), []byte("OFF"), []byte("OFF")},
		":FUNCTION?": {[]byte("FUTURE_WAVE")},
	}}
	controller := newTestInstrument(t, backend)
	waveform, frequency := owonmodel.GeneratorWaveformSine, 1000.0

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{
		Waveform:    &waveform,
		FrequencyHz: &frequency,
	})
	var convergence *ErrGeneratorConvergence
	require.ErrorAs(t, err, &convergence)
	require.Equal(t, GeneratorConvergencePhaseContext, convergence.Phase)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
	require.Equal(t, "FUTURE_WAVE", convergence.LastObservation.Context.ObservedToken)
	require.Equal(t, "FUTURE_WAVE", convergence.LastObservation.Context.ObservedRaw)
	require.Equal(t, "FUNCTION? reply token is unknown", convergence.LastObservation.Context.Reason)
	require.Equal(t, owonmodel.GeneratorOutputCompensationVerified, convergence.Compensation.Status)
	require.Equal(t, []string{
		":CHANNEL?", ":CHANNEL?",
		":FUNCTION SINE", ":FUNCTION?",
		":CHANNEL OFF", ":CHANNEL?", ":CHANNEL?",
	}, backend.Commands)
	require.NotNil(t, result)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, result.Delivery)
}

// TestSetGeneratorObservedCompensatesAfterHealthyContextNonconvergence verifies bounded semantic mismatch handling.
//
// Example: eight valid SQUARE replies for requested SINE permit OFF restoration without sending frequency.
func TestSetGeneratorObservedCompensatesAfterHealthyContextNonconvergence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		functionReplies := make([][]byte, generatorMaximumContextObservations)
		for index := range functionReplies {
			functionReplies[index] = []byte("SQUARE")
		}
		backend := &dmmSequenceBackend{Responses: map[string][][]byte{
			":CHANNEL?":  {[]byte("OFF"), []byte("OFF"), []byte("OFF"), []byte("OFF")},
			":FUNCTION?": functionReplies,
		}}
		controller := newTestInstrument(t, backend)
		waveform, frequency := owonmodel.GeneratorWaveformSine, 1000.0

		result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{
			Waveform:    &waveform,
			FrequencyHz: &frequency,
		})
		var convergence *ErrGeneratorConvergence
		var nonconvergence *ErrGeneratorObservationNonconvergence
		require.ErrorAs(t, err, &convergence)
		require.ErrorAs(t, err, &nonconvergence)
		require.Equal(t, GeneratorConvergencePhaseContext, convergence.Phase)
		require.Equal(t, owonmodel.GeneratorOutputCompensationVerified, convergence.Compensation.Status)
		require.Equal(t, []string{
			":CHANNEL?", ":CHANNEL?", ":FUNCTION SINE",
			":FUNCTION?", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?", ":FUNCTION?",
			":CHANNEL OFF", ":CHANNEL?", ":CHANNEL?",
		}, backend.Commands)
		require.NotNil(t, result)
		require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, result.Delivery)
	})
}

// TestSetGeneratorObservedDoesNotAttemptCompensationAfterTransportFailure verifies poisoned-transaction safety.
//
// Example: a failed FUNCTION write leaves no later CHANNEL command to imply that restoration was attempted.
func TestSetGeneratorObservedDoesNotAttemptCompensationAfterTransportFailure(t *testing.T) {
	backend := &scriptedBackend{
		ExchangeErrors: []error{nil, nil, errors.New("exchange lost")},
		Responses: map[string][]byte{
			":CHANNEL?":  []byte("OFF"),
			":FUNCTION?": []byte("SINe"),
		},
	}
	controller := newTestInstrument(t, backend)
	waveform := owonmodel.GeneratorWaveformSine

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform})
	var convergence *ErrGeneratorConvergence
	require.ErrorAs(t, err, &convergence)
	require.Equal(t, GeneratorConvergencePhaseWrite, convergence.Phase)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
	require.Equal(t, owonmodel.GeneratorOutputCompensationNotAttempted, convergence.Compensation.Status)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, convergence.LastObservation.Output.Status)
	require.Nil(t, convergence.LastObservation.Output.Value)
	require.Equal(t, []string{":CHANNEL?", ":CHANNEL?", ":FUNCTION SINE"}, backend.Commands)
	require.NotNil(t, result)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, result.Observation.Output.Status)
	require.Nil(t, result.Observation.Output.Value)
}

// TestSetGeneratorObservedDoesNotReusePreflightAfterContextTransportFailure verifies poisoned-context safety.
//
// Example: a failed FUNCTION? query cannot leave the earlier OFF preflight as a successful output observation.
func TestSetGeneratorObservedDoesNotReusePreflightAfterContextTransportFailure(t *testing.T) {
	backend := &scriptedBackend{
		ExchangeErrors: []error{nil, nil, nil, errors.New("function query lost")},
		Responses: map[string][]byte{
			":CHANNEL?":  []byte("OFF"),
			":FUNCTION?": []byte("SINe"),
		},
	}
	controller := newTestInstrument(t, backend)
	waveform := owonmodel.GeneratorWaveformSine

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform})
	var convergence *ErrGeneratorConvergence
	require.ErrorAs(t, err, &convergence)
	require.Equal(t, GeneratorConvergencePhaseContext, convergence.Phase)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
	require.Equal(t, owonmodel.GeneratorOutputCompensationNotAttempted, convergence.Compensation.Status)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, convergence.LastObservation.Output.Status)
	require.Nil(t, convergence.LastObservation.Output.Value)
	require.Equal(t, []string{":CHANNEL?", ":CHANNEL?", ":FUNCTION SINE", ":FUNCTION?"}, backend.Commands)
	require.NotNil(t, result)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, result.Observation.Output.Status)
	require.Nil(t, result.Observation.Output.Value)
}

// TestSetGeneratorObservedDoesNotReusePreflightAfterRemainingWriteFailure verifies later-write failure safety.
//
// Example: a failed frequency write invalidates the preflight output observation before restoration is attempted.
func TestSetGeneratorObservedDoesNotReusePreflightAfterRemainingWriteFailure(t *testing.T) {
	backend := &scriptedBackend{
		ExchangeErrors: []error{nil, nil, nil, nil, nil, errors.New("frequency write lost")},
		Responses: map[string][]byte{
			":CHANNEL?":  []byte("OFF"),
			":FUNCTION?": []byte("SINe"),
		},
	}
	controller := newTestInstrument(t, backend)
	waveform, frequency := owonmodel.GeneratorWaveformSine, 1000.0

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{
		Waveform:    &waveform,
		FrequencyHz: &frequency,
	})
	var convergence *ErrGeneratorConvergence
	require.ErrorAs(t, err, &convergence)
	require.Equal(t, GeneratorConvergencePhaseRemainingWrites, convergence.Phase)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
	require.Equal(t, owonmodel.GeneratorOutputCompensationNotAttempted, convergence.Compensation.Status)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, convergence.LastObservation.Output.Status)
	require.Nil(t, convergence.LastObservation.Output.Value)
	require.Equal(t, []string{
		":CHANNEL?", ":CHANNEL?", ":FUNCTION SINE", ":FUNCTION?", ":FUNCTION?", ":FUNCTION:FREQUENCY 1000",
	}, backend.Commands)
	require.NotNil(t, result)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, result.Observation.Output.Status)
	require.Nil(t, result.Observation.Output.Value)
}

// TestSetGeneratorObservedClassifiesCompensationWriteFailure verifies an ambiguous restoration write.
//
// Example: a healthy context barrier is followed by a failed CHANNEL restoration exchange.
func TestSetGeneratorObservedClassifiesCompensationWriteFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &scriptedBackend{
			ExchangeErrors: []error{nil, nil, nil, nil, nil, errors.New("restoration exchange lost")},
			Responses: map[string][]byte{
				":CHANNEL?":  []byte("OFF"),
				":FUNCTION?": []byte("SINe"),
			},
		}
		controller := newTestInstrument(t, backend)
		waveform := owonmodel.GeneratorWaveformSine

		result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{Waveform: &waveform})
		var convergence *ErrGeneratorConvergence
		require.ErrorAs(t, err, &convergence)
		require.Equal(t, GeneratorConvergencePhaseOutput, convergence.Phase)
		require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
		require.Equal(t, owonmodel.GeneratorOutputCompensationFailed, convergence.Compensation.Status)
		require.NotNil(t, convergence.Compensation.Requested)
		require.False(t, *convergence.Compensation.Requested)
		require.Equal(t, owonmodel.GeneratorObservationUnknown, convergence.LastObservation.Output.Status)
		require.Nil(t, convergence.LastObservation.Output.Value)
		require.Contains(t, convergence.Error(), "final logical output unknown")
		require.Contains(t, convergence.Error(), "1 write completed")
		require.Equal(t, []string{
			":CHANNEL?", ":CHANNEL?", ":FUNCTION SINE", ":FUNCTION?", ":FUNCTION?", ":CHANNEL OFF",
		}, backend.Commands)
		require.NotNil(t, result)
		require.Equal(t, owonmodel.GeneratorOutputCompensationFailed, result.Compensation.Status)
	})
}

// TestSetGeneratorObservedClassifiesOutputWriteFailure verifies explicit output-only ambiguity.
//
// Example: observed mode keeps scalar output writes transport-only but still reports an attempted failed write.
func TestSetGeneratorObservedClassifiesOutputWriteFailure(t *testing.T) {
	backend := &scriptedBackend{ExchangeErrors: []error{errors.New("output exchange lost")}}
	controller := newTestInstrument(t, backend)
	output := false

	result, err := controller.SetGeneratorObserved(t.Context(), &owonmodel.GeneratorPatch{Output: &output})
	var convergence *ErrGeneratorConvergence
	require.ErrorAs(t, err, &convergence)
	require.Equal(t, GeneratorConvergencePhaseOutput, convergence.Phase)
	require.Equal(t, owonmodel.GeneratorDeliveryPartialOrUnknown, convergence.Delivery)
	require.Equal(t, owonmodel.GeneratorOutputCompensationFailed, convergence.Compensation.Status)
	require.NotNil(t, convergence.Compensation.Requested)
	require.False(t, *convergence.Compensation.Requested)
	require.Equal(t, owonmodel.GeneratorObservationUnknown, convergence.LastObservation.Output.Status)
	require.Nil(t, convergence.LastObservation.Output.Value)
	require.Contains(t, convergence.Error(), "final logical output unknown")
	require.Equal(t, []string{":CHANNEL OFF"}, backend.Commands)
	require.NotNil(t, result)
	require.Equal(t, owonmodel.GeneratorOutputCompensationFailed, result.Compensation.Status)
}

// TestWaitForGeneratorOutputBoundsUnavailableReplies verifies CHANNEL? cannot spin forever on a device sentinel.
//
// Example: eight unavailable replies stop the logical barrier without sending any mutation.
func TestWaitForGeneratorOutputBoundsUnavailableReplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		responses := make([][]byte, generatorMaximumOutputObservations)
		for index := range responses {
			responses[index] = []byte("error")
		}
		backend := &dmmSequenceBackend{Responses: map[string][][]byte{":CHANNEL?": responses}}

		observation, err := waitForGeneratorOutput(t.Context(), generatorObservationExecutor{backend: backend}, nil)
		var nonconvergence *ErrGeneratorObservationNonconvergence
		require.ErrorAs(t, err, &nonconvergence)
		require.Equal(t, ":CHANNEL?", nonconvergence.Query)
		require.NotNil(t, observation)
		require.Equal(t, owonmodel.GeneratorObservationUnavailable, observation.Status)
		require.Equal(t, generatorMaximumOutputObservations, len(backend.Commands))
	})
}

// TestWaitForGeneratorOutputResetsAfterUnavailableReply verifies unavailable replies do not bridge a match pair.
//
// Example: error, OFF, OFF requires both valid OFF observations after the unavailable sentinel.
func TestWaitForGeneratorOutputResetsAfterUnavailableReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &dmmSequenceBackend{Responses: map[string][][]byte{
			":CHANNEL?": {[]byte("error"), []byte("OFF"), []byte("OFF")},
		}}

		observation, err := waitForGeneratorOutput(t.Context(), generatorObservationExecutor{backend: backend}, nil)
		require.NoError(t, err)
		require.NotNil(t, observation.Value)
		require.False(t, *observation.Value)
		require.Equal(t, []string{":CHANNEL?", ":CHANNEL?", ":CHANNEL?"}, backend.Commands)
	})
}
