package owonmodel

const (
	// ChannelUnspecified identifies the unspecified channel value.
	//
	// Example: compare a Channel value with ChannelUnspecified.
	ChannelUnspecified Channel = iota
	// Channel1 identifies the first oscilloscope input.
	//
	// Example: compare a Channel value with Channel1.
	Channel1
	// Channel2 identifies the second oscilloscope input.
	//
	// Example: compare a Channel value with Channel2.
	Channel2
)

const (
	// CouplingUnspecified identifies the unspecified coupling value.
	//
	// Example: compare a Coupling value with CouplingUnspecified.
	CouplingUnspecified Coupling = iota
	// CouplingAC identifies AC input coupling.
	//
	// Example: compare a Coupling value with CouplingAC.
	CouplingAC
	// CouplingDC identifies DC input coupling.
	//
	// Example: compare a Coupling value with CouplingDC.
	CouplingDC
	// CouplingGround identifies the ground coupling value.
	//
	// Example: compare a Coupling value with CouplingGround.
	CouplingGround
)

// ValidateChannel rejects unspecified and unknown native channels without device I/O.
//
// Example: subscription validation rejects channel three before acquiring a stream slot.
func ValidateChannel(channel Channel) error {
	return validateEnum("channel", channel, Channel1, Channel2)
}

// Channel identifies a device channel value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: Channel1 selects the corresponding documented setting.
type Channel int32

// Coupling identifies a device coupling value. Its fixed-width backing preserves unknown values across RPC conversion.
//
// Example: CouplingAC selects the corresponding documented setting.
type Coupling int32

// ChannelPatch holds optional control writes; nil fields leave their settings unchanged.
//
// Example: a present false Display value hides Channel1 without changing its scale or coupling.
type ChannelPatch struct {
	Channel          Channel
	Display          *bool
	Coupling         *Coupling
	ProbeAttenuation *float64
	Scale            *string
	OffsetDivisions  *int64
	BandwidthLimit   *bool
	Inversion        *bool
}

// Validate checks intrinsic channel values without applying a device's supported setting matrix.
//
// Example: a positive 3X probe is a valid value even when a particular dialect cannot set it.
func (request *ChannelPatch) Validate() error {
	if request == nil {
		return &ErrInvalidRequest{Reason: "channel patch is nil"}
	}
	if request.Display == nil && request.Coupling == nil && request.ProbeAttenuation == nil && request.Scale == nil && request.OffsetDivisions == nil && request.BandwidthLimit == nil && request.Inversion == nil {
		return &ErrInvalidRequest{Reason: "channel patch is empty"}
	}
	if err := ValidateChannel(request.Channel); err != nil {
		return err
	}
	if request.Coupling != nil {
		if err := validateEnum("coupling", *request.Coupling, CouplingAC, CouplingDC, CouplingGround); err != nil {
			return err
		}
	}
	if request.ProbeAttenuation != nil {
		return requireFinitePositive("probe attenuation", *request.ProbeAttenuation)
	}
	return nil
}

// ChannelState contains channel settings observed in a screen header.
//
// Example: ScreenHeaderOffset preserves a fractional observation without making it an integer write value.
type ChannelState struct {
	Channel            Channel
	Display            bool
	Coupling           Coupling
	ProbeAttenuation   float64
	Scale              string
	ScreenHeaderOffset *float64
}
