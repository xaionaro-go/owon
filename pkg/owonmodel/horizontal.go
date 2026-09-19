package owonmodel

// HorizontalPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: a present zero OffsetDivisions value requests `:HORIZONTAL:OFFSET 0`.
type HorizontalPatch struct {
	Scale           *string
	OffsetDivisions *int64
}

// Validate checks that a horizontal patch requests a mutation; scale support belongs to the dialect.
//
// Example: a present zero integer offset is a valid mutation.
func (request *HorizontalPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "horizontal patch is nil"}
	}
	if request.Scale == nil && request.OffsetDivisions == nil {
		return &ErrInvalidRequest{Reason: "horizontal patch is empty"}
	}
	return nil
}

// HorizontalState contains the observed timebase scale and optional raw header offset.
//
// Example: a fractional header offset remains distinct from the integer-only write grammar.
type HorizontalState struct {
	Scale              string
	ScreenHeaderOffset *float64
}
