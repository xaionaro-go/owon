package owonmodel

import (
	"fmt"
)

const (
	// MaximumMeasurementSelectors is the two-channel by seven-kind device matrix.
	//
	// Example: a fifteenth requested measurement is rejected before device I/O.
	MaximumMeasurementSelectors = 14
)

const (
	// MeasurementKindUnspecified identifies the unspecified measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindUnspecified.
	MeasurementKindUnspecified MeasurementKind = iota
	// MeasurementKindUnknown identifies the unknown measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindUnknown.
	MeasurementKindUnknown
	// MeasurementKindMaximum identifies the maximum measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindMaximum.
	MeasurementKindMaximum
	// MeasurementKindMinimum identifies the minimum measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindMinimum.
	MeasurementKindMinimum
	// MeasurementKindPeakToPeak identifies the peak-to-peak measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindPeakToPeak.
	MeasurementKindPeakToPeak
	// MeasurementKindAmplitude identifies the amplitude measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindAmplitude.
	MeasurementKindAmplitude
	// MeasurementKindAverage identifies the average measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindAverage.
	MeasurementKindAverage
	// MeasurementKindPeriod identifies the period measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindPeriod.
	MeasurementKindPeriod
	// MeasurementKindFrequency identifies the frequency measurement kind value.
	//
	// Example: compare a MeasurementKind value with MeasurementKindFrequency.
	MeasurementKindFrequency
)

// ValidateMeasurementSelectors rejects nil, duplicate, unsupported, and excessive selectors.
//
// Example: requesting CH1 frequency twice is a caller error rather than two USB queries.
func ValidateMeasurementSelectors(selectors []*MeasurementSelector) error {
	if len(selectors) > MaximumMeasurementSelectors {
		return fmt.Errorf("request has %d measurements, maximum is %d: %w", len(selectors), MaximumMeasurementSelectors, &ErrInvalidRequest{Reason: "too many measurement selectors"})
	}
	measurements := make(map[MeasurementSelector]struct{}, len(selectors))
	for _, selector := range selectors {
		if err := selector.Validate(); err != nil {
			return err
		}
		key := *selector
		if _, duplicate := measurements[key]; duplicate {
			return fmt.Errorf("duplicate measurement selector %+v: %w", key, &ErrInvalidRequest{Reason: "selector is duplicated"})
		}
		measurements[key] = struct{}{}
	}

	return nil
}

// MeasurementKind identifies a device measurement kind value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: MeasurementKindFrequency selects the frequency query.
type MeasurementKind int32

// MeasurementSelector identifies one channel and measured quantity.
//
// Example: Channel1 with MeasurementKindFrequency selects the CH1 frequency query.
type MeasurementSelector struct {
	Channel Channel
	Kind    MeasurementKind
}

// Validate checks that the selector identifies one known queryable quantity and channel.
//
// Example: Unknown is retained in observations but is invalid in a query.
func (selector *MeasurementSelector) Validate() error {
	if selector == nil {
		return &ErrInvalidRequest{Reason: "measurement selector is nil"}
	}
	if err := ValidateChannel(selector.Channel); err != nil {
		return err
	}
	return validateEnum("measurement kind", selector.Kind, MeasurementKindMaximum, MeasurementKindMinimum, MeasurementKindPeakToPeak, MeasurementKindAmplitude, MeasurementKindAverage, MeasurementKindPeriod, MeasurementKindFrequency)
}

// Measurement contains a parsed oscilloscope measurement and its original device text.
//
// Example: a frequency result retains its numeric value, Hz unit, and unmodified reply.
type Measurement struct {
	Channel Channel
	Kind    MeasurementKind
	Value   float64
	Unit    string
	Raw     string
}

// MeasurementPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: a present true Display value enables the measurement overlay without replacing visible selectors.
type MeasurementPatch struct {
	Display        *bool
	Visible        []*MeasurementSelector
	ReplaceVisible *bool
}

// Validate checks a display patch and selector replacement intent independently of dialect support.
//
// Example: visible selectors require explicit replacement and contain no duplicates.
func (request *MeasurementPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "measurement patch is nil"}
	}
	if request.Display == nil && (request.ReplaceVisible == nil || !*request.ReplaceVisible) {
		return &ErrInvalidRequest{Reason: "measurement patch has no mutation"}
	}
	if len(request.Visible) != 0 && (request.ReplaceVisible == nil || !*request.ReplaceVisible) {
		return &ErrInvalidRequest{Reason: "visible selectors require explicit replacement"}
	}
	return ValidateMeasurementSelectors(request.Visible)
}
