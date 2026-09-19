package owoncontrol

import (
	"context"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// scriptedBackend records session activity and returns deterministic results.
//
// Example: tests configure one timeout followed by one successful exchange.
type scriptedBackend struct {
	Commands       []string
	ExchangeErrors []error
	ReopenCalls    int
	ReopenedSerial owonmodel.SerialNumber
	ReopenError    error
	Responses      map[string][]byte
	CloseErrors    []error
	CloseCalls     int
}

// Exchange records one command and returns its scripted response.
//
// Example: a configured first error models an ambiguous USB transfer.
func (backend *scriptedBackend) Exchange(
	_ context.Context,
	command owonprotocol.Command,
) ([]byte, error) {
	backend.Commands = append(backend.Commands, command.Text)
	if len(backend.ExchangeErrors) > 0 {
		err := backend.ExchangeErrors[0]
		backend.ExchangeErrors = backend.ExchangeErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), backend.Responses[command.Text]...), nil
}

// ReopenAndValidate records the expected serial and returns its scripted error.
//
// Example: recovery tests assert that serial validation precedes a new write.
func (backend *scriptedBackend) ReopenAndValidate(
	_ context.Context,
	serial owonmodel.SerialNumber,
) error {
	backend.ReopenCalls++
	backend.ReopenedSerial = serial
	return backend.ReopenError
}

// Close satisfies Backend for deterministic tests.
//
// Example: no resources are owned by this in-memory backend.
func (backend *scriptedBackend) Close(_ context.Context) error {
	backend.CloseCalls++
	if len(backend.CloseErrors) == 0 {
		return nil
	}
	err := backend.CloseErrors[0]
	backend.CloseErrors = backend.CloseErrors[1:]

	return err
}
