package owonmodel

const (
	// CapabilityUnspecified identifies the unspecified capability value.
	//
	// Example: compare a Capability value with CapabilityUnspecified.
	CapabilityUnspecified Capability = iota
	// CapabilityOscilloscope identifies the oscilloscope capability value.
	//
	// Example: compare a Capability value with CapabilityOscilloscope.
	CapabilityOscilloscope
	// CapabilityWaveformGenerator identifies the waveform generator capability value.
	//
	// Example: compare a Capability value with CapabilityWaveformGenerator.
	CapabilityWaveformGenerator
	// CapabilityMultimeter identifies the multimeter capability value.
	//
	// Example: compare a Capability value with CapabilityMultimeter.
	CapabilityMultimeter
	// CapabilityScreenWaveform identifies the screen waveform capability value.
	//
	// Example: compare a Capability value with CapabilityScreenWaveform.
	CapabilityScreenWaveform
)

// Capability identifies a device capability value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: CapabilityOscilloscope in DeviceInfo.Capabilities reports support for oscilloscope functions.
type Capability int32

// SerialNumber identifies one instrument across discovery and session recovery.
//
// Example: SerialNumber("25061855") pins recovery to the initially selected device.
type SerialNumber string

// VendorID identifies a USB manufacturer while retaining invalid input for validation.
//
// Example: VendorID(0x5345) selects OWON; 0x10000 is rejected before USB conversion.
type VendorID uint32

// ProductID identifies a USB product while preserving the full RPC identifier range.
//
// Example: ProductID(0x1234) selects the supported instrument descriptor.
type ProductID uint32

// DeviceIdentity describes verified device metadata independently of an open session.
//
// Example: USB descriptor identifiers remain available independently of an open connection.
type DeviceIdentity struct {
	VendorID  VendorID
	ProductID ProductID
}

// DeviceInfo combines queried identity with transport metadata and verified capabilities.
//
// Example: USB identifiers come from constructor metadata while firmware comes from the identity reply.
type DeviceInfo struct {
	Manufacturer string
	Model        string
	Serial       SerialNumber
	Firmware     string
	VendorID     VendorID
	ProductID    ProductID
	Capabilities []Capability
}
