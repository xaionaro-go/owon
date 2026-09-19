package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
)

// pipeListener admits one in-memory connection for deterministic lifecycle scheduling.
//
// Example: virtual time can advance Shutdown without relying on real TCP timing.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

// Accept admits the supplied connection until listener shutdown.
//
// Example: closing the listener releases Serve's pending accept.
func (listener *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

// Close signals listener ownership completion exactly once.
//
// Example: Shutdown and deferred Serve cleanup may both close the listener.
func (listener *pipeListener) Close() error {
	listener.once.Do(listener.close)
	return nil
}

// close releases Accept's cancellation channel.
//
// Example: sync.Once invokes this named operation for the first Close.
func (listener *pipeListener) close() { close(listener.closed) }

// Addr supplies a stable address for HTTP request metadata.
//
// Example: tests identify the fake listener without reserving a real TCP port.
func (listener *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// cleanupBarrier keeps request cleanup pending after cancellation has propagated.
//
// Example: a process must not exit before its final request diagnostic is emitted.
type cleanupBarrier struct {
	Started  chan struct{}
	Release  chan struct{}
	Finished chan struct{}
}

// ServeHTTP records completion only after the test releases final request cleanup.
//
// Example: closing sockets alone cannot make Finished ready.
func (barrier cleanupBarrier) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	close(barrier.Started)
	<-request.Context().Done()
	<-barrier.Release
	close(barrier.Finished)
}

// TestServeHTTPJoinsCanceledRequestCleanup proves process return follows handler completion.
//
// Example: forced socket closure still waits for a pending final request diagnostic.
func TestServeHTTPJoinsCanceledRequestCleanup(t *testing.T) {
	synctest.Test(t,
		// checkCleanupOrdering advances only virtual time while all goroutines are durably blocked.
		//
		// Example: the shutdown timeout expires while the handler remains behind Release.
		func(t *testing.T) {
			serverConnection, clientConnection := net.Pipe()
			listener := &pipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{})}
			listener.connections <- serverConnection
			barrier := cleanupBarrier{Started: make(chan struct{}), Release: make(chan struct{}), Finished: make(chan struct{})}
			server := &http.Server{Handler: barrier}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			observability.Go(ctx,
				// runServer exposes the lifecycle owner's return without racing test cleanup.
				//
				// Example: the buffered result records any premature process completion.
				func(ctx context.Context) { result <- serveHTTP(ctx, server, listener, time.Second) })
			_, err := io.WriteString(clientConnection, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
			require.NoError(t, err)
			<-barrier.Started
			cancel()
			time.Sleep(2 * time.Second)
			synctest.Wait()
			assert.Empty(t, result, "HTTP ownership returned before request cleanup finished")
			close(barrier.Release)
			synctest.Wait()
			require.ErrorIs(t, <-result, context.DeadlineExceeded)
			select {
			case <-barrier.Finished:
			default:
				require.FailNow(t, "handler cleanup did not finish")
			}
			require.NoError(t, clientConnection.Close())
		})
}
