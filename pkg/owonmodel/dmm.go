package owonmodel

import (
	"time"
)

const (
	// DMMFunctionUnspecified identifies the unspecified multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionUnspecified.
	DMMFunctionUnspecified DMMFunction = iota
	// DMMFunctionVoltage identifies the voltage multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionVoltage.
	DMMFunctionVoltage
	// DMMFunctionCurrent identifies the current multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionCurrent.
	DMMFunctionCurrent
	// DMMFunctionResistance identifies the resistance multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionResistance.
	DMMFunctionResistance
	// DMMFunctionCapacitance identifies the capacitance multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionCapacitance.
	DMMFunctionCapacitance
	// DMMFunctionDiode identifies the diode multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionDiode.
	DMMFunctionDiode
	// DMMFunctionContinuity identifies the continuity multimeter function value.
	//
	// Example: compare a DMMFunction value with DMMFunctionContinuity.
	DMMFunctionContinuity
)

const (
	// DMMRangeUnspecified identifies the unspecified multimeter range value.
	//
	// Example: compare a DMMRange value with DMMRangeUnspecified.
	DMMRangeUnspecified DMMRange = iota
	// DMMRangeOn identifies the on multimeter range value.
	//
	// Example: compare a DMMRange value with DMMRangeOn.
	DMMRangeOn
	// DMMRangeMV identifies the mv multimeter range value.
	//
	// Example: compare a DMMRange value with DMMRangeMV.
	DMMRangeMV
	// DMMRangeV identifies the v multimeter range value.
	//
	// Example: compare a DMMRange value with DMMRangeV.
	DMMRangeV
)

const (
	// DMMCurrentTypeUnspecified identifies the unspecified multimeter current type value.
	//
	// Example: compare a DMMCurrentType value with DMMCurrentTypeUnspecified.
	DMMCurrentTypeUnspecified DMMCurrentType = iota
	// DMMCurrentTypeAC identifies the ac multimeter current type value.
	//
	// Example: compare a DMMCurrentType value with DMMCurrentTypeAC.
	DMMCurrentTypeAC
	// DMMCurrentTypeDC identifies the dc multimeter current type value.
	//
	// Example: compare a DMMCurrentType value with DMMCurrentTypeDC.
	DMMCurrentTypeDC
)

// DMMFunction identifies a device multimeter function value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: DMMFunctionVoltage selects the corresponding documented setting.
type DMMFunction int32

// DMMRange identifies a device multimeter range value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: DMMRangeOn selects the corresponding documented setting.
type DMMRange int32

// DMMCurrentType identifies a device multimeter current type value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: DMMCurrentTypeAC selects the corresponding documented setting.
type DMMCurrentType int32

// DMMPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: a present false Relative value disables relative mode without changing the selected function.
type DMMPatch struct {
	Function    *DMMFunction
	CurrentType *DMMCurrentType
	Relative    *bool
	Range       *DMMRange
	AutoRange   *bool
}

// Validate checks multimeter value membership and the function/current relationship.
//
// Example: capacitance rejects an AC/DC companion while voltage requires one.
func (request *DMMPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "DMM patch is nil"}
	}
	if request.Function == nil && request.CurrentType == nil && request.Relative == nil && request.Range == nil && request.AutoRange == nil {
		return &ErrInvalidRequest{Reason: "DMM patch is empty"}
	}
	if request.Function != nil {
		if err := validateEnum("DMM function", *request.Function, DMMFunctionVoltage, DMMFunctionCurrent, DMMFunctionResistance, DMMFunctionCapacitance, DMMFunctionDiode, DMMFunctionContinuity); err != nil {
			return err
		}
	}
	if request.CurrentType != nil {
		if err := validateEnum("DMM current type", *request.CurrentType, DMMCurrentTypeAC, DMMCurrentTypeDC); err != nil {
			return err
		}
	}
	if request.Range != nil {
		if err := validateEnum("DMM range", *request.Range, DMMRangeOn, DMMRangeMV, DMMRangeV); err != nil {
			return err
		}
	}
	if request.Function != nil && (*request.Function == DMMFunctionVoltage || *request.Function == DMMFunctionCurrent) {
		if request.CurrentType == nil {
			return &ErrInvalidRequest{Reason: "current type is required for voltage or current"}
		}
	} else {
		if request.CurrentType != nil {
			return &ErrInvalidRequest{Reason: "current type is only valid with voltage or current"}
		}
	}
	return nil
}

// DMMState contains the multimeter function, range, and relative-mode settings.
//
// Example: DC voltage with automatic ranging is distinct from AC current.
type DMMState struct {
	Function    DMMFunction
	CurrentType DMMCurrentType
	Relative    bool
	Range       DMMRange
	AutoRange   bool
}

// DMMMeasurement contains one multimeter reading and its capture time.
//
// Example: a missing function remains nil when the device supplies only a number and unit.
type DMMMeasurement struct {
	Function   *DMMFunction
	Value      float64
	Unit       string
	Raw        string
	CapturedAt time.Time
}
