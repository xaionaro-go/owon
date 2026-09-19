// Package owonprotocol defines OWON command envelopes and response framing.
// It validates and encodes individual commands and decodes bounded response frames;
// it does not own instrument state, connection lifetimes, admission, or recovery.
//
// Example: CommandBytes and ReadResponse encode an explicitly framed SCPI exchange.
package owonprotocol
