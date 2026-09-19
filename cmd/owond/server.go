package main

import (
	"context"
	"errors"
	"net"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"google.golang.org/grpc"
)

// grpcServeLoop owns one gRPC Serve result channel and listener invocation.
//
// Example: observability.Go runs its Run method without an anonymous callback.
type grpcServeLoop struct {
	Server   *grpc.Server
	Listener net.Listener
	Result   chan<- error
}

// Run serves until the server stops and publishes the terminal result once.
//
// Example: run selects the published result against signal cancellation.
func (loop grpcServeLoop) Run(_ context.Context) {
	err := loop.Server.Serve(loop.Listener)
	// Stop may win the race with Serve startup; both shutdown orders are successful.
	if errors.Is(err, grpc.ErrServerStopped) {
		err = nil
	}
	loop.Result <- err
}

// serveGRPC owns listener startup, serving completion, and cancellation cleanup.
//
// Example: signal cancellation stops serving and releases its Unix socket.
func serveGRPC(
	ctx context.Context,
	grpcServer *grpc.Server,
	config options,
) (_err error) {
	if ctx == nil {
		return &ErrConfiguration{Field: "context", Reason: "must not be nil"}
	}
	logger.Tracef(ctx, "serveGRPC")
	// traceServe reports the final listener result after cleanup.
	//
	// Example: cancellation completes before the exit trace is written.
	defer func() { logger.Tracef(ctx, "/serveGRPC: %v", _err) }()
	listener, err := openListener(ctx, config.Endpoint)
	if err != nil {
		return err
	}
	serveErrors := make(chan error, 1)
	serveLoop := grpcServeLoop{Server: grpcServer, Listener: listener, Result: serveErrors}
	observability.Go(ctx, serveLoop.Run)
	ctx = belt.WithField(ctx, "grpc_listen", listener.Addr().String())
	ctx = belt.WithField(ctx, "serial", config.Serial)
	logger.Infof(ctx, "gRPC listening; instrument connected")
	select {
	case serveErr := <-serveErrors:
		grpcServer.Stop()
		return errors.Join(serveErr, closeListener(listener))
	case <-ctx.Done():
		grpcServer.Stop()
		serveErr := <-serveErrors
		return errors.Join(serveErr, closeListener(listener))
	}
}
