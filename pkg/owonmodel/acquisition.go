package owonmodel

const (
	// AcquisitionModeUnspecified identifies the unspecified acquisition mode value.
	//
	// Example: compare an AcquisitionMode value with AcquisitionModeUnspecified.
	AcquisitionModeUnspecified AcquisitionMode = iota
	// AcquisitionModeSample identifies the sample acquisition mode value.
	//
	// Example: compare an AcquisitionMode value with AcquisitionModeSample.
	AcquisitionModeSample
	// AcquisitionModePeakDetect identifies the peak detection acquisition mode value.
	//
	// Example: compare an AcquisitionMode value with AcquisitionModePeakDetect.
	AcquisitionModePeakDetect
	// AcquisitionModeAverage identifies the average acquisition mode value.
	//
	// Example: compare an AcquisitionMode value with AcquisitionModeAverage.
	AcquisitionModeAverage
)

const (
	// AcquisitionMemoryDepthUnspecified identifies the unspecified acquisition memory depth value.
	//
	// Example: compare an AcquisitionMemoryDepth value with AcquisitionMemoryDepthUnspecified.
	AcquisitionMemoryDepthUnspecified AcquisitionMemoryDepth = iota
	// AcquisitionMemoryDepth4K identifies the 4k acquisition memory depth value.
	//
	// Example: compare an AcquisitionMemoryDepth value with AcquisitionMemoryDepth4K.
	AcquisitionMemoryDepth4K
	// AcquisitionMemoryDepth8K identifies the 8k acquisition memory depth value.
	//
	// Example: compare an AcquisitionMemoryDepth value with AcquisitionMemoryDepth8K.
	AcquisitionMemoryDepth8K
)

// AcquisitionMode identifies a device acquisition mode value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: AcquisitionModeSample selects the corresponding documented setting.
type AcquisitionMode int32

// AcquisitionMemoryDepth identifies a device acquisition memory depth value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: AcquisitionMemoryDepth4K selects the corresponding documented setting.
type AcquisitionMemoryDepth int32

// AcquisitionPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: setting Mode to AcquisitionModeSample leaves MemoryDepth unchanged when its pointer is nil.
type AcquisitionPatch struct {
	Mode         *AcquisitionMode
	AverageCount *uint32
	MemoryDepth  *AcquisitionMemoryDepth
}

// Validate checks acquisition value membership independently of device support.
//
// Example: averaging is valid domain data even when the HDS dialect does not implement it.
func (request *AcquisitionPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "acquisition patch is nil"}
	}
	if request.Mode == nil && request.AverageCount == nil && request.MemoryDepth == nil {
		return &ErrInvalidRequest{Reason: "acquisition patch is empty"}
	}
	if request.Mode != nil {
		if err := validateEnum("acquisition mode", *request.Mode, AcquisitionModeSample, AcquisitionModePeakDetect, AcquisitionModeAverage); err != nil {
			return err
		}
	}
	if request.MemoryDepth != nil {
		return validateEnum("memory depth", *request.MemoryDepth, AcquisitionMemoryDepth4K, AcquisitionMemoryDepth8K)
	}
	return nil
}

// AcquisitionState contains acquisition settings observed in a screen header.
//
// Example: a missing average count remains nil instead of becoming a measured zero.
type AcquisitionState struct {
	Mode         AcquisitionMode
	AverageCount *uint32
	MemoryDepth  string
}
