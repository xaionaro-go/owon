package owonscpi

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

const (
	// minimumChannelOffset is the documented lower integer write bound in divisions.
	//
	// Example: -200 is accepted while -201 is rejected before any write.
	minimumChannelOffset = -200
	// maximumChannelOffset is the documented upper integer write bound in divisions.
	//
	// Example: 200 is accepted while 201 is rejected before any write.
	maximumChannelOffset = 200
)

// normalizeChannelScale removes presentation whitespace from one documented
// voltage token while leaving its numeric and unit characters unchanged.
//
// Example: `1.00 V` becomes the canonical SCPI token `1.00V`.
func normalizeChannelScale(value string) string {
	return strings.Join(strings.Fields(value), "")
}

// validChannelScale reports whether a scale is documented and writable for a probe.
//
// Example: 1.00V is valid for 1X while documented-but-broken kV writes are rejected.
func validChannelScale(
	scale string,
	probe float64,
) bool {
	if probe != 0 {
		return slices.Contains(channelScales(probe), scale)
	}
	for _, candidate := range []float64{1, 10, 100, 1000, 10000} {
		if slices.Contains(channelScales(candidate), scale) {
			return true
		}
	}

	return false
}

// channelScales returns the verified writable scale matrix for one probe attenuation.
//
// Example: 10X starts at 100mV; documented-but-broken kV writes are absent for every probe.
func channelScales(probe float64) []string {
	switch probe {
	case 1:
		return []string{"10.0mV", "20.0mV", "50.0mV", "100mV", "200mV", "500mV", "1.00V", "2.00V", "5.00V", "10.0V"}
	case 10:
		return []string{"100mV", "200mV", "500mV", "1.00V", "2.00V", "5.00V", "10.0V", "20.0V", "50.0V", "100V"}
	case 100:
		return []string{"1.00V", "2.00V", "5.00V", "10.0V", "20.0V", "50.0V", "100V", "200V", "500V"}
	case 1000:
		return []string{"10.0V", "20.0V", "50.0V", "100V", "200V", "500V"}
	case 10000:
		return []string{"100V", "200V", "500V"}
	default:
		return nil
	}
}

// channelName maps API channel values to OWON SCPI channel names.
//
// Example: Channel2 maps to `CH2`.
func channelName(channel owonmodel.Channel) (string, error) {
	switch channel {
	case owonmodel.Channel1:
		return "CH1", nil
	case owonmodel.Channel2:
		return "CH2", nil
	default:
		return "", fmt.Errorf("unsupported channel %d: %w", channel, &ErrInvalidSetting{Reason: "channel is not supported"})
	}
}

// channelPrefix returns the SCPI subsystem prefix for one channel.
//
// Example: Channel1 maps to `:CH1`.
func channelPrefix(channel owonmodel.Channel) (string, error) {
	name, err := channelName(channel)
	if err != nil {
		return "", err
	}

	return ":" + name, nil
}

// couplingSCPI maps typed coupling to an OWON token.
//
// Example: CouplingDC maps to `DC`.
func couplingSCPI(coupling owonmodel.Coupling) (string, error) {
	switch coupling {
	case owonmodel.CouplingAC:
		return "AC", nil
	case owonmodel.CouplingDC:
		return "DC", nil
	case owonmodel.CouplingGround:
		return "GND", nil
	default:
		return "", fmt.Errorf("unsupported coupling %d: %w", coupling, &ErrInvalidSetting{Reason: "coupling is not supported"})
	}
}

// CompileChannel validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileChannel(request *owonmodel.ChannelPatch) (*ChannelWritePlan, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	prefix, err := channelPrefix(request.Channel)
	if err != nil {
		return nil, err
	}
	commands := make([]string, 0, 7)
	if request.Display != nil {
		commands = append(commands, prefix+":DISPLAY "+onOff(*request.Display))
	}
	if request.Coupling != nil {
		value, couplingErr := couplingSCPI(*request.Coupling)
		if couplingErr != nil {
			return nil, couplingErr
		}
		commands = append(commands, prefix+":COUPLING "+value)
	}
	if request.ProbeAttenuation != nil {
		switch *request.ProbeAttenuation {
		case 1, 10, 100, 1000, 10000:
		default:
			return nil, fmt.Errorf("set channel: unsupported probe attenuation %g: %w", *request.ProbeAttenuation, &ErrInvalidSetting{Reason: "probe attenuation is not supported"})
		}
		commands = append(commands, prefix+":PROBE "+formatNumber(*request.ProbeAttenuation)+"X")
	}
	if request.Scale != nil {
		scale := normalizeChannelScale(*request.Scale)
		if !validChannelScale(scale, 0) {
			return nil, fmt.Errorf("set channel: unsupported scale %q: %w", scale, &ErrInvalidSetting{Reason: "scale is not supported"})
		}
		if request.ProbeAttenuation != nil && !validChannelScale(scale, *request.ProbeAttenuation) {
			return nil, fmt.Errorf("set channel: unsupported scale %q for probe %gX: %w", scale, *request.ProbeAttenuation, &ErrInvalidSetting{Reason: "scale is not supported for the selected probe attenuation"})
		}
		commands = append(commands, prefix+":SCALE "+scale)
	}
	if request.OffsetDivisions != nil {
		if *request.OffsetDivisions < minimumChannelOffset || *request.OffsetDivisions > maximumChannelOffset {
			return nil, fmt.Errorf("set channel: offset %d outside [%d,%d]: %w", *request.OffsetDivisions, minimumChannelOffset, maximumChannelOffset, &ErrInvalidSetting{Reason: "offset is outside the supported range"})
		}
		// The documented write grammar requires integers, independent of fractional query replies.
		commands = append(commands, prefix+":OFFSET "+strconv.FormatInt(*request.OffsetDivisions, 10))
	}
	if request.BandwidthLimit != nil {
		return nil, fmt.Errorf("set channel bandwidth limit: %w", &ErrUnsupportedControl{Control: "channel bandwidth limit"})
	}
	if request.Inversion != nil {
		return nil, fmt.Errorf("set channel inversion: %w", &ErrUnsupportedControl{Control: "channel inversion"})
	}
	return &ChannelWritePlan{Commands: writeCommands(commands), Channel: request.Channel, ProbeRequired: request.Scale != nil && request.ProbeAttenuation == nil, Scale: scaleValue(request.Scale)}, nil
}

// ChannelWritePlan describes ordered writes and the observed-probe preflight a scale-only patch needs.
//
// Example: ProbeRequired keeps the read and subsequent writes under one controller transaction.
type ChannelWritePlan struct {
	Commands      []owonprotocol.Command
	Channel       owonmodel.Channel
	ProbeRequired bool
	Scale         string
}

// scaleValue canonicalizes an optional scale without retaining a mutable caller pointer.
//
// Example: an absent scale produces an empty preflight value.
func scaleValue(value *string) string {
	if value == nil {
		return ""
	}
	return normalizeChannelScale(*value)
}

// ValidateChannelProbe checks a compiled scale against an attenuation observed while admission is held.
//
// Example: a 10mV scale is rejected for a current 10X probe before any writes.
func ValidateChannelProbe(
	plan *ChannelWritePlan,
	probe float64,
) error {
	if plan == nil {
		return &ErrInvalidSetting{Reason: "channel write plan is nil"}
	}
	if probe <= 0 || math.IsNaN(probe) || math.IsInf(probe, 0) {
		return &ErrInvalidSetting{Reason: "observed probe attenuation must be finite and positive"}
	}
	if !validChannelScale(plan.Scale, probe) {
		return &ErrInvalidSetting{Reason: fmt.Sprintf("scale %q is unsupported for current probe %gX", plan.Scale, probe)}
	}
	return nil
}
