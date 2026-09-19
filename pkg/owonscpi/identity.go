package owonscpi

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// ParseIdentity parses the four-field SCPI identity response.
//
// Example: `OWON,HDS2202S,25061855,V2.6.0` becomes a DeviceInfo.
func ParseIdentity(response []byte) (*owonmodel.DeviceInfo, error) {
	parts := strings.Split(strings.TrimSpace(string(response)), ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("parse OWON identity: got %d fields, want 4: %w", len(parts), &ErrMalformedResponse{Reason: "identity field count is invalid"})
	}
	for index, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("parse OWON identity: field %d is empty: %w", index, &ErrMalformedResponse{Reason: "identity field is empty"})
		}
	}

	return &owonmodel.DeviceInfo{
		Manufacturer: strings.TrimSpace(parts[0]),
		Model:        strings.TrimSpace(parts[1]),
		Serial:       owonmodel.SerialNumber(strings.TrimSpace(parts[2])),
		Firmware:     strings.TrimSpace(parts[3]),
	}, nil
}

// CapabilitiesForModel reports only broad device surfaces verified for a recognized model.
// Individual typed state fields remain independently gated; generator queries exist, but no
// complete validated mapping populates typed generator state.
//
// Example: HDS2202S advertises its scope, generator, multimeter, and screen-waveform surfaces.
func CapabilitiesForModel(model string) []owonmodel.Capability {
	if model != "HDS2202S" {
		return nil
	}

	return []owonmodel.Capability{
		owonmodel.CapabilityOscilloscope,
		owonmodel.CapabilityWaveformGenerator,
		owonmodel.CapabilityMultimeter,
		owonmodel.CapabilityScreenWaveform,
	}
}

// IdentityQuery selects the four-field instrument identity response.
//
// Example: USB recovery and normal controller identity reads share this dialect command.
func IdentityQuery() owonprotocol.Command {
	return owonprotocol.Command{Text: "*IDN?", ResponseMode: owonprotocol.ResponseModeASCII}
}
