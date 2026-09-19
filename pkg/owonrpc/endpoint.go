package owonrpc

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// NetworkUnspecified rejects missing transport configuration.
	//
	// Example: the zero Endpoint cannot be dialed.
	NetworkUnspecified Network = iota
	// NetworkUnix selects a Unix domain socket.
	//
	// Example: local clients connect without TLS over a private socket.
	NetworkUnix
	// NetworkTCP selects an IP connection.
	//
	// Example: remote addresses require mutual TLS.
	NetworkTCP

	// maximumUnixSocketPathBytes leaves room for the terminating NUL in Linux sockaddr_un.
	//
	// Example: an oversized runtime-directory path falls back to the short per-user temporary path.
	maximumUnixSocketPathBytes = 107
)

// DefaultEndpoint derives the current user's local socket without accessing the filesystem.
// An absent, relative, or overlong XDG_RUNTIME_DIR uses a stable per-effective-user
// path under /tmp; TMPDIR is intentionally ignored so process-specific temporary
// directories cannot split clients from the daemon. The listener creates the private parent.
//
// Example: XDG_RUNTIME_DIR=/run/user/1000 selects unix:///run/user/1000/owon/owond.sock.
func DefaultEndpoint() string {
	runtimeDirectory := os.Getenv("XDG_RUNTIME_DIR")
	path := filepath.Join(runtimeDirectory, "owon", "owond.sock")
	if !filepath.IsAbs(runtimeDirectory) || len(path) > maximumUnixSocketPathBytes {
		path = filepath.Join("/tmp", "owon-"+strconv.Itoa(os.Geteuid()), "owond.sock")
	}

	target := url.URL{Scheme: "unix", Path: path}
	return target.String()
}

// Network identifies the supported daemon transports.
//
// Example: NetworkUnix selects a local filesystem socket.
type Network int

// String returns the standard network spelling, retaining unknown values for diagnostics.
//
// Example: NetworkTCP.String() is suitable for net.Listen.
func (network Network) String() string {
	switch network {
	case NetworkUnspecified:
		return "unspecified"
	case NetworkUnix:
		return "unix"
	case NetworkTCP:
		return "tcp"
	default:
		return fmt.Sprintf("unknown(%d)", int(network))
	}
}

// Endpoint describes a daemon transport whose methods validate its current fields.
//
// Example: changing Address recalculates security policy on the next RequiresTLS call.
type Endpoint struct {
	Network Network
	Address string
}

// Target derives the gRPC target from the validated network and address.
//
// Example: a Unix endpoint becomes `unix:///run/owond/owond.sock`.
func (endpoint Endpoint) Target() (string, error) {
	switch endpoint.Network {
	case NetworkUnix:
		if _, err := endpoint.RequiresTLS(); err != nil {
			return "", err
		}
		// Serialize the decoded filename so delimiters remain path bytes when gRPC reparses it.
		target := url.URL{Scheme: "unix", Path: endpoint.Address}
		return target.String(), nil
	case NetworkTCP:
		_, port, err := endpoint.tcpAddress()
		if err != nil {
			return "", err
		}
		if port == 0 {
			return "", &ErrEndpoint{Value: endpoint.Address, Reason: "TCP client endpoint port is zero"}
		}
		return "dns:///" + endpoint.Address, nil
	default:
		return "", &ErrEndpoint{Value: endpoint.Network.String(), Reason: "endpoint network is unsupported"}
	}
}

// RequiresTLS derives security policy from the current address, including ephemeral listeners.
//
// Example: a listener on 0.0.0.0:0 still requires TLS before publishing its allocated port.
func (endpoint Endpoint) RequiresTLS() (bool, error) {
	switch endpoint.Network {
	case NetworkUnix:
		if !filepath.IsAbs(endpoint.Address) || strings.ContainsRune(endpoint.Address, 0) {
			return false, &ErrEndpoint{Value: endpoint.Address, Reason: "Unix endpoint requires an absolute path without NUL"}
		}
		return false, nil
	case NetworkTCP:
		host, _, err := endpoint.tcpAddress()
		if err != nil {
			return false, err
		}
		address := net.ParseIP(host)
		return address == nil || !address.IsLoopback(), nil
	default:
		return false, &ErrEndpoint{Value: endpoint.Network.String(), Reason: "endpoint network is unsupported"}
	}
}

// tcpAddress parses the independent host and port components shared by transport policies.
//
// Example: port zero is valid for a listener, while Target separately rejects it for a client.
func (endpoint Endpoint) tcpAddress() (string, uint64, error) {
	host, portText, err := net.SplitHostPort(endpoint.Address)
	if err != nil {
		return "", 0, &ErrEndpoint{Value: endpoint.Address, Reason: "TCP endpoint address is invalid", Cause: err}
	}
	if host == "" {
		return "", 0, &ErrEndpoint{Value: endpoint.Address, Reason: "TCP endpoint host is empty"}
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return "", 0, &ErrEndpoint{Value: endpoint.Address, Reason: "TCP endpoint port is invalid", Cause: err}
	}
	return host, port, nil
}

// ParseEndpoint accepts Unix sockets and TCP addresses with validated transport policy.
//
// Example: `tcp://0.0.0.0:50051` is valid and RequiresTLS reports true.
func ParseEndpoint(value string) (Endpoint, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Endpoint{}, &ErrEndpoint{Value: value, Reason: "URL syntax is invalid", Cause: err}
	}
	switch parsed.Scheme {
	case "unix":
		if parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
			return Endpoint{}, &ErrEndpoint{Value: value, Reason: "Unix endpoint contains unsupported URL components"}
		}
		endpoint := Endpoint{Network: NetworkUnix, Address: parsed.Path}
		if _, err := endpoint.Target(); err != nil {
			return Endpoint{}, err
		}
		return endpoint, nil
	case "tcp":
		if parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
			return Endpoint{}, &ErrEndpoint{Value: value, Reason: "TCP endpoint contains unsupported URL components"}
		}
		endpoint := Endpoint{Network: NetworkTCP, Address: parsed.Host}
		if _, err := endpoint.Target(); err != nil {
			return Endpoint{}, err
		}
		return endpoint, nil
	default:
		return Endpoint{}, &ErrEndpoint{Value: value, Reason: "endpoint scheme is unsupported"}
	}
}

// ErrEndpoint identifies an endpoint value that cannot be used safely.
//
// Example: a Unix endpoint with a relative path returns an ErrEndpoint.
type ErrEndpoint struct {
	Value  string
	Reason string
	Cause  error
}

// Error returns the endpoint value and validation reason.
//
// Example: `parse endpoint "tcp://": host is empty` is rendered for invalid input.
func (err *ErrEndpoint) Error() string {
	if err == nil {
		return ""
	}
	detail := err.Reason
	if err.Cause != nil {
		detail = fmt.Sprintf("%s: %v", detail, err.Cause)
	}
	if err.Value == "" {
		return detail
	}

	return fmt.Sprintf("endpoint %q: %s", err.Value, detail)
}

// Unwrap returns an underlying URL or address parsing failure.
//
// Example: callers can inspect net.SplitHostPort errors through errors.As.
func (err *ErrEndpoint) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
