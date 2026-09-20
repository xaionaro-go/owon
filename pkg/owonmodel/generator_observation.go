package owonmodel

import "time"

// ErrGeneratorObservationNonconvergence identifies a healthy transaction that exhausted valid observations.
//
// Example: repeated valid SQUARE replies for a requested SINE stop a bounded logical barrier.
type ErrGeneratorObservationNonconvergence struct {
	Query string
}

// Error describes the query dimension that did not reach two consecutive target matches.
//
// Example: errors.As distinguishes logical nonconvergence from a transport failure.
func (err *ErrGeneratorObservationNonconvergence) Error() string {
	if err == nil || err.Query == "" {
		return "generator observation did not converge"
	}
	return "generator observation did not converge for " + err.Query
}

// Unwrap reports that nonconvergence is a leaf semantic classification.
//
// Example: status mapping can classify the healthy-device condition without inventing a lower cause.
func (*ErrGeneratorObservationNonconvergence) Unwrap() error { return nil }

// GeneratorObservationStatus identifies how one generator readback value was classified.
//
// Example: an unrecognized FUNCTION? token is Malformed while its raw token remains available.
type GeneratorObservationStatus uint8

const (
	// GeneratorObservationUnspecified identifies an observation field that was not queried.
	//
	// Example: a write-only generator patch has an unspecified output observation.
	GeneratorObservationUnspecified GeneratorObservationStatus = iota
	// GeneratorObservationObserved identifies a parsed logical readback value.
	//
	// Example: the exact OFF token becomes an observed false output value.
	GeneratorObservationObserved
	// GeneratorObservationUnverified identifies raw data whose meaning or unit is not proven.
	//
	// Example: a future numeric query can retain raw text without inventing a typed value.
	GeneratorObservationUnverified
	// GeneratorObservationUnavailable identifies a device sentinel that supplied no value.
	//
	// Example: an exact error reply can be retried during a bounded observation barrier.
	GeneratorObservationUnavailable
	// GeneratorObservationMalformed identifies a complete reply that cannot be parsed safely.
	//
	// Example: an unknown waveform token cannot authorize dependent writes.
	GeneratorObservationMalformed
	// GeneratorObservationUnknown identifies a delivery or transport ambiguity.
	//
	// Example: a failed write leaves the final generator state unknown.
	GeneratorObservationUnknown
)

// GeneratorWaveformObservation contains one parsed or retained FUNCTION? reply.
//
// Example: Value is populated only when Status is GeneratorObservationObserved.
type GeneratorWaveformObservation struct {
	Status GeneratorObservationStatus
	Value  *GeneratorWaveform
	Token  string
	Raw    string
	Reason string
}

// GeneratorOutputObservation contains one parsed or retained CHANNEL? reply.
//
// Example: Value false with Token OFF describes logical output state only.
type GeneratorOutputObservation struct {
	Status GeneratorObservationStatus
	Value  *bool
	Token  string
	Raw    string
	Reason string
}

// GeneratorContextStatus identifies the comparison between requested and observed waveform context.
//
// Example: GeneratorContextMatch permits dependent writes after the two-observation barrier.
type GeneratorContextStatus uint8

const (
	// GeneratorContextUnspecified identifies an operation without a waveform context.
	//
	// Example: an amplitude-only patch does not query FUNCTION?.
	GeneratorContextUnspecified GeneratorContextStatus = iota
	// GeneratorContextMatch identifies an observed waveform equal to the requested waveform.
	//
	// Example: two SINe replies satisfy a requested sine context.
	GeneratorContextMatch
	// GeneratorContextMismatch identifies a valid waveform reply different from the request.
	//
	// Example: a SQUARE reply is a valid nonmatch for a requested sine waveform.
	GeneratorContextMismatch
	// GeneratorContextUnknown identifies a malformed, unavailable, or absent context.
	//
	// Example: a future waveform token leaves context unknown instead of guessed.
	GeneratorContextUnknown
)

// GeneratorWaveformContext records the explicit target and latest observed waveform context.
//
// Example: Requested Sine and Observed Sine with Match authorize dependent writes.
type GeneratorWaveformContext struct {
	Requested     *GeneratorWaveform
	Observed      *GeneratorWaveform
	ObservedToken string
	ObservedRaw   string
	Reason        string
	Match         GeneratorContextStatus
}

// GeneratorObservation combines the context and logical output readbacks captured by one operation.
//
// Example: output compensation reports its final logical CHANNEL? observation here.
type GeneratorObservation struct {
	CapturedAt time.Time
	Context    GeneratorWaveformContext
	Output     GeneratorOutputObservation
}

// GeneratorDeliveryStatus classifies how much of a generator operation is known.
//
// Example: readback-free legacy writes report TransportCompleteReadbackUnverified.
type GeneratorDeliveryStatus uint8

const (
	// GeneratorDeliveryNotAttempted identifies validation or preflight failure before a mutation write.
	//
	// Example: an unstable output preflight prevents FUNCTION from being sent.
	GeneratorDeliveryNotAttempted GeneratorDeliveryStatus = iota
	// GeneratorDeliveryTransportCompleteReadbackVerified identifies a fully verified logical operation.
	//
	// Example: waveform context and final output both converge twice.
	GeneratorDeliveryTransportCompleteReadbackVerified
	// GeneratorDeliveryTransportCompleteReadbackUnverified identifies transport completion with unsupported readback fields.
	//
	// Example: frequency was written but this slice has no validated numeric query mapping.
	GeneratorDeliveryTransportCompleteReadbackUnverified
	// GeneratorDeliveryPartialOrUnknown identifies a post-write failure or ambiguous application.
	//
	// Example: an exchange failure prevents any claim about final device state.
	GeneratorDeliveryPartialOrUnknown
)

// GeneratorOutputCompensationStatus classifies operation-local output restoration.
//
// Example: Verified means the final logical CHANNEL? barrier matched its requested state.
type GeneratorOutputCompensationStatus uint8

const (
	// GeneratorOutputCompensationNotAttempted identifies a transport failure that forbade restoration.
	//
	// Example: a poisoned transaction cannot issue a compensation command.
	GeneratorOutputCompensationNotAttempted GeneratorOutputCompensationStatus = iota
	// GeneratorOutputCompensationNotRequired identifies an operation without waveform output preservation.
	//
	// Example: a scalar-only patch has no output transition to restore.
	GeneratorOutputCompensationNotRequired
	// GeneratorOutputCompensationVerified identifies a restored logical output state.
	//
	// Example: OFF was written last and observed twice as OFF.
	GeneratorOutputCompensationVerified
	// GeneratorOutputCompensationUnverified identifies a requested restoration without two matching observations.
	//
	// Example: a caller deadline can leave the restoration state unverified.
	GeneratorOutputCompensationUnverified
	// GeneratorOutputCompensationFailed identifies an attempted restoration with a semantic or transport failure.
	//
	// Example: a malformed CHANNEL? reply prevents an honest restored-state claim.
	GeneratorOutputCompensationFailed
)

// GeneratorOutputCompensation records the requested logical output restoration and its outcome.
//
// Example: Requested false and Status Verified documents an operation-local OFF restoration.
type GeneratorOutputCompensation struct {
	Requested *bool
	Observed  *bool
	Status    GeneratorOutputCompensationStatus
}

// GeneratorOperationResult contains the bounded native result of an observed generator operation.
//
// Example: callers inspect Delivery and Observation before presenting a generator result to a user.
type GeneratorOperationResult struct {
	Delivery        GeneratorDeliveryStatus
	Observation     *GeneratorObservation
	CompletedWrites []string
	Compensation    GeneratorOutputCompensation
}
