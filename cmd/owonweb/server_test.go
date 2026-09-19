package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/stretchr/testify/require"
)

// TestParseOptionsHonorsHTTPBind verifies the operator controls HTTP exposure.
//
// Example: wildcard and LAN listeners are accepted alongside the local default.
func TestParseOptionsHonorsHTTPBind(t *testing.T) {
	t.Parallel()

	for _, listen := range []string{"127.0.0.1:0", "[::1]:0", "localhost:0", "0.0.0.0:0", ":0", "[::]:0", "192.0.2.1:8080", "example.test:8080"} {
		options := new(webOptions)
		require.NoError(t, newCommand(options).ParseFlags([]string{"--listen", listen}))
		require.NoErrorf(t, validateHTTPListen(options.HTTPListen), "listen=%s", listen)
		require.Equal(t, listen, options.HTTPListen)
	}
	for _, listen := range []string{"", "missing-port", "127.0.0.1:65536", "127.0.0.1:-1", "127.0.0.1:abc"} {
		require.Error(t, validateHTTPListen(listen), "listen=%s", listen)
	}
}

// TestRunStopsWhenCanceled verifies the process lifecycle closes a listener on context cancellation.
//
// Example: service managers can terminate the web client without leaving a port bound.
func TestRunStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx := belt.WithField(context.Background(), "request_id", "retained")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &startupOutput{Cancel: cancel}
	err := run(ctx, []string{"--grpc", "unix:///tmp/owonweb-test.sock", "--listen", "127.0.0.1:0"}, output)
	require.NoError(t, err)
	require.Contains(t, output.String(), "HTTP listening on 127.0.0.1:")
	require.Contains(t, output.String(), "; open http://127.0.0.1:")
	require.Contains(t, output.String(), "unix:///tmp/owonweb-test.sock")
	require.Contains(t, output.String(), "HTTP server stopped")
	require.NotContains(t, output.String(), "instrument connected")
	require.NotContains(t, output.String(), "http://127.0.0.1:0/")
	require.Contains(t, output.String(), "http_listen=")
	require.Contains(t, output.String(), "grpc_target=")
	require.Contains(t, output.String(), "request_id=retained")
	require.Contains(t, output.String(), "service=owonweb")
	require.NotContains(t, output.String(), "level=trace")
}

// startupOutput cancels only after the executable has announced its bound listener.
//
// Example: startup and shutdown can be asserted without readiness polling.
type startupOutput struct {
	bytes.Buffer
	Cancel  context.CancelFunc
	Failure error
}

// Write records diagnostics and signals shutdown after the actual startup event.
//
// Example: connection construction occurs before cancellation, as in a live process.
func (output *startupOutput) Write(data []byte) (int, error) {
	count, err := output.Buffer.Write(data)
	if bytes.Contains(data, []byte("HTTP listening")) {
		output.Cancel()
		if output.Failure != nil {
			return 0, output.Failure
		}
	}
	return count, err
}

// TestRunLogDeliveryFailureDoesNotChangeServerResult separates void logging from help delivery.
//
// Example: a failed startup diagnostic still allows the listener owner to stop cleanly.
func TestRunLogDeliveryFailureDoesNotChangeServerResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &startupOutput{Cancel: cancel, Failure: errors.New("log delivery failed")}
	require.NoError(t, run(ctx, []string{"--listen", "127.0.0.1:0"}, output))
	require.Contains(t, output.String(), "HTTP server stopped")
}

// TestCommandTraceLevelEnablesOperationalTraces verifies the selected log threshold is applied.
//
// Example: validation failure is traced at Trace without announcing an HTTP listener.
func TestCommandTraceLevelEnablesOperationalTraces(t *testing.T) {
	var output bytes.Buffer
	require.Error(t, run(context.Background(), []string{"--log-level", "trace", "--listen", "invalid"}, &output))
	require.Contains(t, output.String(), "level=trace")
	require.Contains(t, output.String(), "/runServer:")
	require.NotContains(t, output.String(), "HTTP listening")
}

// TestRunTracesShutdownWithContext preserves diagnostic fields during detached draining.
//
// Example: cancellation leaves the request ID attached to shutdown and final trace events.
func TestRunTracesShutdownWithContext(t *testing.T) {
	ctx := belt.WithField(context.Background(), "request_id", "shutdown-retained")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &startupOutput{Cancel: cancel}
	require.NoError(t, run(ctx, []string{"--log-level", "trace", "--listen", "127.0.0.1:0"}, output))
	for _, line := range bytes.Split(output.Bytes(), []byte("\n")) {
		if !bytes.Contains(line, []byte("draining HTTP server")) {
			continue
		}
		require.Contains(t, string(line), "request_id=shutdown-retained")
		require.Contains(t, string(line), "http_listen=")
	}
	require.Contains(t, output.String(), "draining HTTP server")
	require.Contains(t, output.String(), "/serveHTTP: <nil>")
}

// TestBrowserURLPreservesScopedIPv6 ensures a link-local address retains its interface.
//
// Example: browsers receive an escaped percent sign, and parsing recovers the exact zone.
func TestBrowserURLPreservesScopedIPv6(t *testing.T) {
	for _, zone := range []string{"eth0", "interface name", "en%0"} {
		address := &net.TCPAddr{IP: net.ParseIP("fe80::1"), Zone: zone, Port: 8080}
		value := browserURL(address)
		parsed, err := url.Parse(value)
		require.NoError(t, err)
		require.Equal(t, "fe80::1%"+zone, parsed.Hostname())
		require.Contains(t, value, "%25")
		require.Equal(t, "8080", parsed.Port())
	}
}

// TestBrowserURLUsesReachableHost checks wildcard URLs use a concrete loopback host.
//
// Example: IPv6 wildcard listeners advertise [::1] while specific LAN hosts remain unchanged.
func TestBrowserURLUsesReachableHost(t *testing.T) {
	for _, test := range []struct {
		IP  string
		URL string
	}{
		{IP: "0.0.0.0", URL: "http://127.0.0.1:8080/"},
		{IP: "::", URL: "http://[::1]:8080/"},
		{IP: "192.0.2.1", URL: "http://192.0.2.1:8080/"},
		{IP: "::1", URL: "http://[::1]:8080/"},
	} {
		require.Equal(t, test.URL, browserURL(&net.TCPAddr{IP: net.ParseIP(test.IP), Port: 8080}))
	}
}
