package owonscpi

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// validHorizontalScale reports whether a timebase token is in the documented matrix.
//
// Example: 20us is accepted while an arbitrary 3us token is rejected.
func validHorizontalScale(scale string) bool {
	return slices.Contains([]string{
		"2.0ns", "5.0ns", "10.0ns", "20.0ns", "50.0ns", "100ns", "200ns", "500ns",
		"1.0us", "2.0us", "5.0us", "10us", "20us", "50us", "100us", "200us", "500us",
		"1.0ms", "2.0ms", "5.0ms", "10ms", "20ms", "50ms", "100ms", "200ms", "500ms",
		"1.0s", "2.0s", "5.0s", "10s", "20s", "50s", "100s", "200s", "500s", "1000s",
	}, scale)
}

// CompileHorizontal validates a complete patch and encodes ordered HDS commands without I/O.
//
// Example: rejected patches produce no commands and cannot partially mutate a device.
func CompileHorizontal(request *owonmodel.HorizontalPatch) ([]owonprotocol.Command, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	commands := make([]string, 0, 2)
	if request.Scale != nil {
		scale := strings.TrimSpace(*request.Scale)
		if !validHorizontalScale(scale) {
			return nil, fmt.Errorf("set horizontal: unsupported scale %q: %w", scale, &ErrInvalidSetting{Reason: "scale is not supported"})
		}
		commands = append(commands, ":HORIZONTAL:SCALE "+scale)
	}
	if request.OffsetDivisions != nil {
		// Keep the integer write exact; the manual documents no fixed horizontal range.
		commands = append(commands, ":HORIZONTAL:OFFSET "+strconv.FormatInt(*request.OffsetDivisions, 10))
	}

	return writeCommands(commands), nil
}
