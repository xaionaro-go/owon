package owonrpc

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"

	"google.golang.org/grpc/credentials"
)

// ServerTLSConfig identifies server credentials, client trust, and identity allowlists.
//
// Example: a remote daemon trusts one client CA and permits a collector DNS SAN.
type ServerTLSConfig struct {
	CertificateFile           string
	PrivateKeyFile            string
	ClientCAFile              string
	AllowedClientSANs         []string
	AllowedClientFingerprints []string
}

// ClientTLSConfig identifies mutual-TLS client credentials and server verification.
//
// Example: owonctl supplies a CA, client certificate, key, and expected DNS name.
type ClientTLSConfig struct {
	CAFile          string
	CertificateFile string
	PrivateKeyFile  string
	ServerName      string
}

// clientIdentityVerifier admits a verified client certificate on an explicit allowlist.
//
// Example: either an allowed URI SAN or SHA-256 certificate fingerprint can admit a peer.
type clientIdentityVerifier struct {
	sans         []string
	fingerprints []string
}

// verify checks the verified leaf certificate against SAN and fingerprint allowlists.
//
// Example: CA-valid but unlisted client certificates are rejected after chain verification.
func (verifier clientIdentityVerifier) verify(connection tls.ConnectionState) error {
	if len(connection.VerifiedChains) == 0 || len(connection.PeerCertificates) == 0 {
		return &ErrTLSConfiguration{Reason: "client certificate was not verified"}
	}
	certificate := connection.PeerCertificates[0]
	identities := certificateSANIdentities(certificate)
	for _, allowed := range verifier.sans {
		normalized, err := normalizeAllowedSAN(allowed)
		if err != nil {
			return err
		}
		if slices.Contains(identities, normalized) {
			return nil
		}
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	fingerprintText := hex.EncodeToString(fingerprint[:])
	if slices.Contains(verifier.fingerprints, fingerprintText) {
		return nil
	}

	return &ErrTLSConfiguration{Reason: "verified client certificate is absent from SAN and SHA-256 allowlists"}
}

// certificateSANIdentities returns typed normalized identities without wildcard expansion.
//
// Example: `*.example.com` remains a literal DNS SAN and cannot authorize `host.example.com`.
func certificateSANIdentities(certificate *x509.Certificate) []string {
	identities := make([]string, 0, len(certificate.DNSNames)+len(certificate.IPAddresses)+len(certificate.URIs)+len(certificate.EmailAddresses))
	for _, name := range certificate.DNSNames {
		identities = append(identities, "dns:"+normalizeDNSName(name))
	}
	for _, address := range certificate.IPAddresses {
		identities = append(identities, "ip:"+address.String())
	}
	for _, uri := range certificate.URIs {
		identities = append(identities, "uri:"+uri.String())
	}
	for _, email := range certificate.EmailAddresses {
		identities = append(identities, "email:"+strings.TrimSpace(email))
	}

	return identities
}

// normalizeAllowedSAN classifies one allowlist identity using certificate SAN syntax.
//
// Example: an IPv6 spelling is canonicalized through net.IP before exact comparison.
func normalizeAllowedSAN(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", &ErrTLSConfiguration{Reason: "client SAN allowlist contains an empty identity"}
	}
	if address := net.ParseIP(trimmed); address != nil {
		return "ip:" + address.String(), nil
	}
	if strings.Contains(trimmed, ":") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" {
			return "", &ErrTLSConfiguration{Reason: fmt.Sprintf("client SAN %q is not a valid URI", value), Cause: err}
		}

		return "uri:" + parsed.String(), nil
	}
	if strings.Contains(trimmed, "@") {
		return "email:" + trimmed, nil
	}

	return "dns:" + normalizeDNSName(trimmed), nil
}

// normalizeAllowedSANs validates and canonicalizes every configured SAN before
// a listener is published.
//
// Example: an opaque `urn:owon:collector` is valid and canonicalizes to a typed URI identity.
func normalizeAllowedSANs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeAllowedSAN(value)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}

	return result, nil
}

// normalizeDNSName canonicalizes case and one presentation-only trailing dot.
//
// Example: `Collector.Example.` normalizes to `collector.example` without wildcard matching.
func normalizeDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

// LoadServerTLSCredentials constructs TLS 1.3 mutual-TLS credentials.
//
// Example: non-loopback owond listeners use the result as a grpc.Creds option.
func LoadServerTLSCredentials(config ServerTLSConfig) (credentials.TransportCredentials, error) {
	if strings.TrimSpace(config.CertificateFile) == "" || strings.TrimSpace(config.PrivateKeyFile) == "" || strings.TrimSpace(config.ClientCAFile) == "" {
		return nil, &ErrTLSConfiguration{Reason: "server TLS requires certificate, private key, and client CA files"}
	}
	if len(config.AllowedClientSANs) == 0 && len(config.AllowedClientFingerprints) == 0 {
		return nil, &ErrTLSConfiguration{Reason: "server TLS requires a client SAN or SHA-256 fingerprint allowlist"}
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, &ErrTLSConfiguration{Reason: "load server certificate failed", Cause: err}
	}
	clientCAs, err := loadCertificatePool(config.ClientCAFile)
	if err != nil {
		return nil, &ErrTLSConfiguration{Reason: "load client CA failed", Cause: err}
	}
	if _, err := normalizeAllowedSANs(config.AllowedClientSANs); err != nil {
		return nil, err
	}
	fingerprints, err := normalizeFingerprints(config.AllowedClientFingerprints)
	if err != nil {
		return nil, err
	}
	verifier := clientIdentityVerifier{sans: append([]string(nil), config.AllowedClientSANs...), fingerprints: fingerprints}

	return credentials.NewTLS(&tls.Config{
		Certificates:     []tls.Certificate{certificate},
		ClientAuth:       tls.RequireAndVerifyClientCert,
		ClientCAs:        clientCAs,
		MinVersion:       tls.VersionTLS13,
		VerifyConnection: verifier.verify,
	}), nil
}

// LoadClientTLSCredentials constructs TLS 1.3 credentials with a client certificate.
//
// Example: remote owonctl verifies the server name and presents its CA-signed identity.
func LoadClientTLSCredentials(config ClientTLSConfig) (credentials.TransportCredentials, error) {
	if strings.TrimSpace(config.CAFile) == "" || strings.TrimSpace(config.CertificateFile) == "" || strings.TrimSpace(config.PrivateKeyFile) == "" || strings.TrimSpace(config.ServerName) == "" {
		return nil, &ErrTLSConfiguration{Reason: "client TLS requires CA, certificate, private key, and server name"}
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, &ErrTLSConfiguration{Reason: "load client certificate failed", Cause: err}
	}
	roots, err := loadCertificatePool(config.CAFile)
	if err != nil {
		return nil, &ErrTLSConfiguration{Reason: "load server CA failed", Cause: err}
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
		ServerName:   config.ServerName,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

// loadCertificatePool loads at least one PEM certificate into a new trust pool.
//
// Example: an empty or malformed CA file is rejected before a listener starts.
func loadCertificatePool(path string) (*x509.CertPool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, &ErrTLSConfiguration{Reason: "read certificate pool failed", Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, &ErrTLSConfiguration{Reason: "file contains no PEM certificates"}
	}

	return pool, nil
}

// normalizeFingerprints validates and canonicalizes colon-separated SHA-256 values.
//
// Example: `AA:BB:...` becomes lowercase hexadecimal without separators.
func normalizeFingerprints(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), ":", ""))
		decoded, err := hex.DecodeString(normalized)
		if err != nil || len(decoded) != sha256.Size {
			return nil, &ErrTLSConfiguration{Reason: fmt.Sprintf("client fingerprint %q is not a SHA-256 value", value), Cause: err}
		}
		result = append(result, normalized)
	}

	return result, nil
}

// ErrTLSConfiguration identifies invalid TLS credentials or identity policy.
//
// Example: missing client CA material returns a ErrTLSConfiguration.
type ErrTLSConfiguration struct {
	Reason string
	Cause  error
}

// Error returns the TLS configuration failure.
//
// Example: `server TLS requires a client CA` explains a rejected listener setup.
func (err *ErrTLSConfiguration) Error() string {
	if err == nil {
		return "invalid TLS configuration"
	}
	if err.Reason == "" {
		if err.Cause == nil {
			return "invalid TLS configuration"
		}

		return fmt.Sprintf("invalid TLS configuration: %v", err.Cause)
	}
	if err.Cause != nil {
		return fmt.Sprintf("%s: %v", err.Reason, err.Cause)
	}

	return err.Reason
}

// Unwrap returns an underlying PEM or certificate parser failure.
//
// Example: malformed PEM remains available to errors.As callers.
func (err *ErrTLSConfiguration) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}
