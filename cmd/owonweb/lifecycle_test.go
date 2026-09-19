package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
)

// lifecycleHandler exposes entry, cancellation and exit without polling sleeps.
//
// Example: tests can prove shutdown interrupts both idle and blocked writers.
type lifecycleHandler struct {
	Started  chan struct{}
	Canceled chan struct{}
	Finished chan struct{}
	Flood    bool
}

// ServeHTTP models an idle SSE stream or a peer that stops reading its body.
//
// Example: Flood writes beyond socket capacity until forced Close releases it.
func (handler lifecycleHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer close(handler.Finished)
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	if err := http.NewResponseController(writer).Flush(); err != nil {
		return
	}
	close(handler.Started)
	if !handler.Flood {
		<-request.Context().Done()
		close(handler.Canceled)
		return
	}
	payload := []byte(strings.Repeat("x", 1<<20))
	for {
		if _, err := writer.Write(payload); err != nil {
			<-request.Context().Done()
			close(handler.Canceled)
			return
		}
	}
}

// TestServeHTTPClosesIdleAndStalledSockets proves bounded shutdown on real TCP.
//
// Example: a reader that consumes headers only cannot keep the serve loop alive.
func TestServeHTTPClosesIdleAndStalledSockets(t *testing.T) {
	for _, flood := range []bool{false, true} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		handler := lifecycleHandler{Started: make(chan struct{}), Canceled: make(chan struct{}), Finished: make(chan struct{}), Flood: flood}
		server := &http.Server{Handler: handler}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		observability.Go(ctx,
			// serveSocket exercises the same lifecycle owner used by the executable.
			//
			// Example: the result arrives only after Shutdown/Close and Serve have returned.
			func(ctx context.Context) { done <- serveHTTP(ctx, server, listener, 100*time.Millisecond) })
		connection, err := net.Dial("tcp", listener.Addr().String())
		require.NoError(t, err)
		tcpConnection := connection.(*net.TCPConn)
		require.NoError(t, tcpConnection.SetReadBuffer(1024))
		_, err = io.WriteString(connection, "GET /api/events HTTP/1.1\r\nHost: localhost\r\n\r\n")
		require.NoError(t, err)
		require.NoError(t, connection.SetReadDeadline(time.Now().Add(5*time.Second)))
		response, err := http.ReadResponse(bufio.NewReader(connection), nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		<-handler.Started
		cancel()
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "HTTP owner did not finish")
		}
		if flood {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		} else {
			require.NoError(t, err)
		}
		<-handler.Canceled
		<-handler.Finished
		require.NoError(t, connection.Close())
	}
}

// TestServeHTTPRetainsListenerFailure proves unexpected Serve failures escape.
//
// Example: a preclosed listener fails instead of looking like clean shutdown.
func TestServeHTTPRetainsListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	err = serveHTTP(context.Background(), new(http.Server), listener, time.Second)
	require.Error(t, err)
	require.True(t, errors.Is(err, net.ErrClosed))
}
