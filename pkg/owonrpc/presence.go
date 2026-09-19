package owonrpc

// clonePointer copies a present scalar without sharing its storage.
//
// Example: mutating a converted optional false does not change its source.
func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

// convertPointer preserves optional fixed-width enum values, including unknown values for native validation.
//
// Example: a recognized unsupported acquisition mode reaches the native validator unchanged.
func convertPointer[From ~int32, To ~int32](value *From) *To {
	if value == nil {
		return nil
	}
	result := To(*value)
	return &result
}
