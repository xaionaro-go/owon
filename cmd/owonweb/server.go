package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonlog"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"github.com/xaionaro-go/owon/pkg/owonrpc/owonclient"
	"github.com/xaionaro-go/owon/pkg/owonweb"
)

const (
	// httpReadHeaderTimeout bounds clients that stall before completing headers.
	//
	// Example: an incomplete request cannot occupy a connection indefinitely.
	httpReadHeaderTimeout = 5 * time.Second
	// httpReadTimeout bounds request bodies without imposing a streaming write timeout.
	//
	// Example: a slow JSON upload is interrupted after twenty seconds.
	httpReadTimeout = 20 * time.Second
	// httpIdleTimeout bounds keep-alive connections between requests.
	//
	// Example: an inactive browser connection is reclaimed after two minutes.
	httpIdleTimeout = 2 * time.Minute
	// httpShutdownTimeout bounds graceful draining before sockets are forcibly closed.
	//
	// Example: a stalled SSE reader cannot hold graceful shutdown beyond five seconds.
	httpShutdownTimeout = 5 * time.Second
)

// httpServeRunner owns one HTTP server's lifecycle callback.
//
// Example: observability.Go runs Serve without an anonymous goroutine,
// preserving the server error for the owning run function.
type httpServeRunner struct {
	Server   *http.Server
	Listener net.Listener
	Errors   chan<- error
}

// Run serves HTTP and reports the terminal listener error.
//
// Example: observability.Go(ctx, runner.Run) starts the listener with tracked goroutine context.
func (runner httpServeRunner) Run(ctx context.Context) {
	logger.Tracef(ctx, "httpServeRunner.Run")
	err := runner.Server.Serve(runner.Listener)
	logger.Tracef(ctx, "/httpServeRunner.Run: %v", err)
	runner.Errors <- err
}

// runServer owns the lazy gRPC client and HTTP server until process cancellation.
// The SSE endpoint intentionally keeps WriteTimeout disabled.
//
// Example: canceling the context shuts down the listener and closes the gRPC
// connection without leaving a long-lived stream behind.
func (options *webOptions) runServer(
	command *cobra.Command,
	_ []string,
) (resultErr error) {
	ctx, err := owonlog.WithContext(command.Context(), owonlog.Config{Level: options.LogLevel, Service: "owonweb"}, command.ErrOrStderr())
	if err != nil {
		return err
	}
	logger.Tracef(ctx, "runServer")
	defer
	// traceResult records failures after owned transport cleanup has completed.
	//
	// Example: shutdown and close errors appear in the final command trace.
	func() { logger.Tracef(ctx, "/runServer: %v", resultErr) }()

	if err := validateHTTPListen(options.HTTPListen); err != nil {
		return err
	}
	endpoint, err := owonrpc.ParseEndpoint(options.GRPCTarget)
	if err != nil {
		return fmt.Errorf("parse gRPC endpoint: %w", err)
	}
	connection, err := owonclient.NewConnection(ctx, owonclient.ConnectionConfig{Endpoint: endpoint, TLS: options.TLS})
	if err != nil {
		return fmt.Errorf("create gRPC client: %w", err)
	}
	defer
	// closeClient retains cleanup failures alongside the operation result.
	//
	// Example: shutdown cannot hide a gRPC connection close failure.
	func() {
		if closeErr := connection.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close gRPC connection: %w", closeErr))
		}
	}()
	handler, err := owonweb.NewHandler(connection.RPCClient())
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: options.HTTPListen, Handler: handler,
		ReadHeaderTimeout: httpReadHeaderTimeout, ReadTimeout: httpReadTimeout, IdleTimeout: httpIdleTimeout,
	}
	listener, err := net.Listen("tcp", options.HTTPListen)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	ctx = belt.WithField(ctx, "http_listen", listener.Addr().String())
	ctx = belt.WithField(ctx, "grpc_target", options.GRPCTarget)
	logger.Infof(ctx, "HTTP listening on %s; open %s; configured daemon %s (connection is established on API requests)", listener.Addr(), browserURL(listener.Addr().(*net.TCPAddr)), options.GRPCTarget)
	if err := serveHTTP(ctx, server, listener, httpShutdownTimeout); err != nil {
		return err
	}
	logger.Infof(ctx, "HTTP server stopped")

	return nil
}

// browserURL returns a usable local browser URL for a bound TCP listener.
//
// Example: a wildcard bind advertises loopback for browsing on the server itself.
func browserURL(address *net.TCPAddr) string {
	host := address.IP.String()
	if address.IP.IsUnspecified() {
		host = "::1"
		if address.IP.To4() != nil {
			host = "127.0.0.1"
		}
	}
	if address.Zone != "" {
		host += "%" + address.Zone
	}

	// URL serialization escapes the zone delimiter and any special interface characters.
	result := url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(address.Port)), Path: "/"}
	return result.String()
}

// serveHTTP cancels request lifetimes, closes stalled connections, and joins the serve result.
//
// Example: process cancellation terminates an idle SSE subscription before returning.
func serveHTTP(
	ctx context.Context,
	server *http.Server,
	listener net.Listener,
	shutdownTimeout time.Duration,
) (_err error) {
	logger.Tracef(ctx, "serveHTTP")
	defer
	// traceShutdown reports the joined lifecycle result after request cleanup.
	//
	// Example: a forced close retains its shutdown deadline in the final trace.
	func() { logger.Tracef(ctx, "/serveHTTP: %v", _err) }()

	requestContext, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()
	// BaseContext must be a callback because net/http supplies a listener instead of a context.
	//
	// Example: each accepted connection inherits process cancellation without a context holder.
	server.BaseContext = func(net.Listener) context.Context { return requestContext }
	connections := new(httpConnections)
	server.ConnState = connections.changed
	serverErrors := make(chan error, 1)
	observability.Go(ctx, httpServeRunner{Server: server, Listener: listener, Errors: serverErrors}.Run)
	var serveErr error
	var received bool
	select {
	case <-ctx.Done():
	case serveErr = <-serverErrors:
		received = true
	}
	cancelRequests()
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	logger.Debugf(shutdownContext, "draining HTTP server")
	var errs []error
	if err := server.Shutdown(shutdownContext); err != nil {
		errs = append(errs, fmt.Errorf("shutdown HTTP server: %w", err))
		if closeErr := server.Close(); closeErr != nil {
			errs = append(errs, fmt.Errorf("close HTTP server: %w", closeErr))
		}
	}
	if !received {
		serveErr = <-serverErrors
	}
	// Serve has stopped accepting, so every New registration precedes this Wait.
	// Owned handlers finish after request cancellation and forced socket closure;
	// waiting preserves their cleanup and final response diagnostics.
	connections.active.Wait()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		errs = append(errs, fmt.Errorf("serve HTTP: %w", serveErr))
	}
	return errors.Join(errs...)
}

// httpConnections tracks accepted connections until their handlers have returned.
//
// Example: forced Close cannot let process exit overtake a request's final diagnostic.
type httpConnections struct {
	active sync.WaitGroup
}

// changed balances each accepted connection with its terminal ownership transition.
//
// Example: StateClosed follows handler return, while StateHijacked transfers ownership.
func (connections *httpConnections) changed(
	_ net.Conn,
	state http.ConnState,
) {
	switch state {
	case http.StateNew:
		connections.active.Add(1)
	case http.StateClosed, http.StateHijacked:
		connections.active.Done()
	}
}
