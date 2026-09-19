package owonprotocol

import (
	"fmt"
	"strings"
)

const (
	// DefaultMaximumCommandBytes bounds a single SCPI command before its line terminator.
	//
	// Example: the default rejects unexpectedly large raw Execute requests.
	DefaultMaximumCommandBytes = 4096
)

const (
	// ResponseModeUnspecified is invalid because response framing must be explicit.
	//
	// Example: raw Execute rejects this value before writing to USB.
	ResponseModeUnspecified ResponseMode = iota
	// ResponseModeNone declares that the command has no response bytes.
	//
	// Example: `:RUN` uses ResponseModeNone.
	ResponseModeNone
	// ResponseModeASCII declares one LF-terminated response line.
	//
	// Example: `*IDN?` uses ResponseModeASCII.
	ResponseModeASCII
	// ResponseModeLengthPrefixed declares a four-byte little-endian length followed by payload bytes.
	//
	// Example: waveform queries use ResponseModeLengthPrefixed.
	ResponseModeLengthPrefixed
)

// ResponseMode identifies the explicit framing expected for one device transaction.
//
// Example: ResponseModeASCII decodes an LF-terminated identity response.
type ResponseMode int32

// Command describes exactly one SCPI transaction and its expected response framing.
//
// Example: Command{Text: "*IDN?", ResponseMode: ResponseModeASCII} requests identity.
type Command struct {
	Text         string
	ResponseMode ResponseMode
}

// ValidateCommand rejects empty, oversized, multi-command, and unframed requests.
//
// Example: ValidateCommand rejects `*IDN?;:RUN` as two commands.
func ValidateCommand(command Command) error {
	for index := 0; index < len(command.Text); index++ {
		byteValue := command.Text[index]
		if byteValue < 0x20 || byteValue == 0x7f || byteValue >= 0x80 {
			return fmt.Errorf("command contains non-printable byte at offset %d: %w", index, &ErrInvalidCommand{Reason: "contains a non-printable byte"})
		}
	}
	text := strings.TrimSpace(command.Text)
	if text == "" {
		return fmt.Errorf("command is empty: %w", &ErrInvalidCommand{Reason: "is empty"})
	}
	if len(text) > DefaultMaximumCommandBytes {
		return fmt.Errorf("command has %d bytes, maximum is %d: %w", len(text), DefaultMaximumCommandBytes, &ErrInvalidCommand{Reason: "exceeds the maximum length"})
	}
	if strings.ContainsRune(text, ';') {
		return fmt.Errorf("command %q contains a command separator: %w", command.Text, &ErrInvalidCommand{Reason: "contains a command separator"})
	}
	switch command.ResponseMode {
	case ResponseModeNone, ResponseModeASCII, ResponseModeLengthPrefixed:
		return nil
	case ResponseModeUnspecified:
		return fmt.Errorf("response mode is unspecified: %w", &ErrInvalidCommand{Reason: "response mode is unspecified"})
	default:
		return fmt.Errorf("response mode %d is unsupported: %w", command.ResponseMode, &ErrInvalidCommand{Reason: "response mode is unsupported"})
	}
}

// CommandBytes validates and normalizes a command to one newline-terminated protocol message.
//
// Example: CommandBytes trims outer spaces from ` *IDN? ` and appends one LF.
func CommandBytes(command Command) ([]byte, error) {
	if err := ValidateCommand(command); err != nil {
		return nil, err
	}

	return append([]byte(strings.TrimSpace(command.Text)), '\n'), nil
}

// ErrInvalidCommand identifies a raw SCPI command that cannot be sent safely.
//
// Example: a command containing a semicolon returns an ErrInvalidCommand.
type ErrInvalidCommand struct {
	Reason string
}

// Error returns the invalid-command classification and optional detail.
//
// Example: `invalid OWON command: command separator` identifies unsafe framing.
func (err *ErrInvalidCommand) Error() string {
	if err == nil || err.Reason == "" {
		return "invalid OWON command"
	}

	return fmt.Sprintf("invalid OWON command: %s", err.Reason)
}

// Unwrap returns no lower-level cause because this error is a leaf classification.
//
// Example: errors.As still matches ErrInvalidCommand through a wrapped validation error.
func (*ErrInvalidCommand) Unwrap() error {
	return nil
}
