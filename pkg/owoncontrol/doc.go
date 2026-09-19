// Package owoncontrol sequences typed instrument operations through a serialized session.
// It owns transaction grouping, held preflight and observation timestamps, not dialect tokens,
// physical I/O, connection recovery policy or RPC behavior.
//
// Example: Controller.Waveform holds admission across its header and raw-data queries.
package owoncontrol
