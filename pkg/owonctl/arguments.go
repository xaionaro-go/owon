package owonctl

import "fmt"

// ErrCommandArguments identifies malformed arguments for one subcommand.
//
// Example: `waveform --channel 3` returns an ErrCommandArguments for waveform.
type ErrCommandArguments struct {
	Command string
	Reason  string
}

// Error returns the command-specific argument diagnostic.
//
// Example: the command name prefixes its invalid-argument explanation.
func (err *ErrCommandArguments) Error() string {
	if err == nil {
		return ""
	}
	if err.Command == "" {
		return err.Reason
	}

	return fmt.Sprintf("%s: %s", err.Command, err.Reason)
}

// Unwrap returns no lower-level cause because this error is a leaf classification.
//
// Example: errors.As matches ErrCommandArguments without parsing text.
func (*ErrCommandArguments) Unwrap() error {
	return nil
}

// ErrFlagParse wraps a maintained CLI parser failure with its command name.
//
// Example: a malformed `--timeout` value retains the original parsing error for callers.
type ErrFlagParse struct {
	Command string
	Cause   error
}

// Error returns the flag parsing diagnostic.
//
// Example: `owonctl execute` prefixes a malformed mode with its command name.
func (err *ErrFlagParse) Error() string {
	if err == nil {
		return ""
	}
	if err.Command == "" {
		if err.Cause == nil {
			return "flag parsing failed"
		}

		return err.Cause.Error()
	}
	if err.Cause == nil {
		return fmt.Sprintf("%s: flag parsing failed", err.Command)
	}

	return fmt.Sprintf("%s: %v", err.Command, err.Cause)
}

// Unwrap returns the original flag parsing error.
//
// Example: errors.As can inspect the parser's original classification.
func (err *ErrFlagParse) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
