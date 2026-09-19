package owonserver

import (
	"context"
	"errors"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// rpcError translates instrument errors into stable transport-independent gRPC status.
//
// Example: canceled requests become Canceled while device failures become Unavailable.
func rpcError(
	operation string,
	err error,
) error {
	if err == nil {
		return nil
	}
	if grpcStatus, ok := status.FromError(err); ok && grpcStatus.Code() != codes.Unknown {
		return err
	}
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, operation+": "+err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, operation+": "+err.Error())
	}
	var invalidCommand *owonprotocol.ErrInvalidCommand
	var invalidRequest *owonmodel.ErrInvalidRequest
	var invalidSetting *owonscpi.ErrInvalidSetting
	var dialectMalformed *owonscpi.ErrMalformedResponse
	var waveformTooLarge *ErrWaveformTooLarge
	var invalidInput *ErrInvalidInput
	var rpcInvalidInput *owonrpc.ErrInvalidInput
	var invalidConfig *ErrInvalidConfig
	var unsupportedControl *owonscpi.ErrUnsupportedControl
	var criticalBackpressure *ErrCriticalBackpressure
	var subscriptionResourceExhausted *ErrSubscriptionResourceExhausted
	var malformedResponse *owonprotocol.ErrMalformedResponse
	var responseTooLarge *owonprotocol.ErrResponseTooLarge
	var surplusResponse *owonprotocol.ErrSurplusResponse
	// Device decoding can reuse request validators; the enclosing protocol failure owns the classification.
	switch {
	case errors.As(err, &malformedResponse), errors.As(err, &responseTooLarge), errors.As(err, &surplusResponse), errors.As(err, &dialectMalformed), errors.As(err, &waveformTooLarge):
		return status.Error(codes.DataLoss, operation+": "+err.Error())
	case errors.As(err, &invalidCommand):
		return status.Error(codes.InvalidArgument, operation+": "+err.Error())
	case errors.As(err, &invalidRequest), errors.As(err, &invalidSetting), errors.As(err, &invalidInput), errors.As(err, &rpcInvalidInput), errors.As(err, &invalidConfig):
		return status.Error(codes.InvalidArgument, operation+": "+err.Error())
	case errors.As(err, &unsupportedControl):
		return status.Error(codes.Unimplemented, operation+": "+err.Error())
	case errors.As(err, &criticalBackpressure), errors.As(err, &subscriptionResourceExhausted):
		return status.Error(codes.ResourceExhausted, operation+": "+err.Error())
	default:
		return status.Error(codes.Unavailable, operation+": "+err.Error())
	}
}
