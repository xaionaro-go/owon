package owonscpi

import (
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// WaveformQuery compiles the supported screen capture selection into its binary data query.
//
// Example: a valid non-screen request reports unsupported control rather than guessing a command.
func WaveformQuery(request *owonmodel.WaveformRequest) (owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return owonprotocol.Command{}, err
	}
	if !request.Screen {
		return owonprotocol.Command{}, &ErrUnsupportedControl{Control: "non-screen waveform capture"}
	}
	channel, err := channelName(request.Channel)
	if err != nil {
		return owonprotocol.Command{}, err
	}
	return owonprotocol.Command{Text: ":DATA:WAVE:SCREEN:" + channel + "?", ResponseMode: owonprotocol.ResponseModeLengthPrefixed}, nil
}
