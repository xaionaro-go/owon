package owonctl

import (
	"fmt"
	"io"
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// PrintMessage writes one protobuf value as readable ProtoJSON using proto field names.
//
// Example: info and state commands use PrintMessage for their responses.
func PrintMessage(
	output io.Writer,
	message proto.Message,
) error {
	if output == nil || isNilWriter(output) {
		return &ErrConfiguration{Field: "output", Reason: "must not be nil"}
	}
	if message == nil || isNilProtoMessage(message) {
		return &ErrConfiguration{Field: "message", Reason: "must not be nil"}
	}
	encoded, err := (protojson.MarshalOptions{Indent: "  ", UseProtoNames: true}).Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal response JSON: %w", err)
	}
	if _, err := fmt.Fprintln(output, string(encoded)); err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	return nil
}

// isNilWriter detects an io.Writer interface containing a typed nil pointer.
//
// Example: a nil *bytes.Buffer is rejected before JSON output attempts a method call.
func isNilWriter(output io.Writer) bool {
	value := reflect.ValueOf(output)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// isNilProtoMessage detects a protobuf interface containing a typed nil pointer.
//
// Example: a nil *DeviceInfo is rejected instead of being serialized as an empty object.
func isNilProtoMessage(message proto.Message) bool {
	value := reflect.ValueOf(message)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
