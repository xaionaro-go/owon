package owonserver

import (
	"context"
	"io"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonlog"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// RPCLogging configures server request contexts from validated logging configuration.
// Its callbacks receive context from gRPC and never retain a request context or logger.
//
// Example: registering it with grpc.StatsHandler gives unary and streaming handlers the same identity.
type RPCLogging struct {
	initializer *owonlog.Initializer
	serial      owonmodel.SerialNumber
}

// NewRPCLogging validates the logging boundary before any connection callbacks can run.
//
// Example: an invalid output writer fails daemon startup before binding a listener.
func NewRPCLogging(
	config owonlog.Config,
	output io.Writer,
	serial owonmodel.SerialNumber,
) (*RPCLogging, error) {
	initializer, err := owonlog.NewInitializer(config, output)
	if err != nil {
		return nil, err
	}
	return &RPCLogging{initializer: initializer, serial: serial}, nil
}

// TagConn initializes the connection logger while preserving existing context values.
//
// Example: every RPC on the connection inherits the configured daemon service and serial.
func (logging *RPCLogging) TagConn(
	ctx context.Context,
	info *stats.ConnTagInfo,
) context.Context {
	ctx = logging.initializer.WithContext(ctx)
	ctx = belt.WithField(ctx, "serial", logging.serial)
	if info.RemoteAddr != nil {
		ctx = belt.WithField(ctx, "peer", info.RemoteAddr.String())
	}
	return ctx
}

// TagRPC adds the transport's full method name to the inherited connection context.
//
// Example: Subscribe's stream context carries rpc_method without a stream wrapper.
func (logging *RPCLogging) TagRPC(
	ctx context.Context,
	info *stats.RPCTagInfo,
) context.Context {
	return belt.WithField(ctx, "rpc_method", info.FullMethodName)
}

// HandleConn emits connection lifecycle events and ignores unrelated transport statistics.
//
// Example: a disconnected collector produces one connection-end Debug record.
func (logging *RPCLogging) HandleConn(
	ctx context.Context,
	event stats.ConnStats,
) {
	switch event.(type) {
	case *stats.ConnBegin:
		logger.Debugf(ctx, "RPC connection opened")
	case *stats.ConnEnd:
		logger.Debugf(ctx, "RPC connection closed")
	}
}

// HandleRPC traces request entry and exit without inspecting request or response payloads.
//
// Example: expected stream cancellation is a terminal status field, not an operational Error.
func (logging *RPCLogging) HandleRPC(
	ctx context.Context,
	event stats.RPCStats,
) {
	switch event := event.(type) {
	case *stats.Begin:
		logger.Tracef(ctx, "HandleRPC")
		logger.Debugf(ctx, "RPC started")
	case *stats.End:
		ctx = belt.WithField(ctx, "rpc_code", status.Code(event.Error).String())
		logger.Debugf(ctx, "RPC ended")
		logger.Tracef(ctx, "/HandleRPC: %v", event.Error)
	}
}
