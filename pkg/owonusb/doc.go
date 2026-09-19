// Package owonusb discovers and owns native USB resources for an OWON instrument.
// It retains endpoint ownership through cancellation and validated physical reconnection.
// Framing grammar belongs to owonprotocol; application/session/controller composition does not belong here.
//
// Example: Open returns a validated Backend or a retryable ErrUSBOpen cleanup owner.
package owonusb
