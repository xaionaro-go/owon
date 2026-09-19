package owonrpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"google.golang.org/grpc/credentials"
)

// TestTLSIdentityNormalization verifies exact typed identities and rejects malformed trust configuration.
//
// Example: an IP and URI SAN remain distinct from DNS and email identities.
func TestTLSIdentityNormalization(t *testing.T) {
	t.Parallel()
	uri, err := url.Parse("urn:owon:collector")
	require.NoError(t, err)
	identities := certificateSANIdentities(&x509.Certificate{DNSNames: []string{"Collector.Example."}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{uri}, EmailAddresses: []string{"user@example.com"}})
	require.Equal(t, []string{"dns:collector.example", "ip:127.0.0.1", "uri:urn:owon:collector", "email:user@example.com"}, identities)
	for _, value := range []string{"127.0.0.1", "urn:owon:collector", "user@example.com"} {
		normalized, err := normalizeAllowedSAN(value)
		require.NoError(t, err)
		require.Contains(t, identities, normalized)
	}
	_, err = normalizeAllowedSAN(" ")
	requireErrorType[*ErrTLSConfiguration](t, err)
	verifier := clientIdentityVerifier{sans: []string{" "}}
	err = verifier.verify(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{new(x509.Certificate)}}, PeerCertificates: []*x509.Certificate{new(x509.Certificate)}})
	requireErrorType[*ErrTLSConfiguration](t, err)
	fingerprints, err := normalizeFingerprints([]string{strings.Repeat("AA:", 31) + "AA"})
	require.NoError(t, err)
	require.Equal(t, []string{strings.Repeat("aa", 32)}, fingerprints)
	for _, value := range []string{"", "xyz", strings.Repeat("aa", 31)} {
		fingerprints, err = normalizeFingerprints([]string{value})
		require.Nil(t, fingerprints)
		requireErrorType[*ErrTLSConfiguration](t, err)
	}
}

// TestTLSCredentialFileFailures verifies file and PEM failures preserve their causes before credentials are exposed.
//
// Example: valid leaf credentials plus a missing trust file cannot create a partially verified connection.
func TestTLSCredentialFileFailures(t *testing.T) {
	t.Parallel()
	fixture := newCertificateFixture(t)
	missing := filepath.Join(t.TempDir(), "missing.pem")
	_, err := LoadServerTLSCredentials(ServerTLSConfig{})
	requireErrorType[*ErrTLSConfiguration](t, err)
	for _, config := range []ServerTLSConfig{
		{CertificateFile: missing, PrivateKeyFile: missing, ClientCAFile: missing, AllowedClientSANs: []string{"collector.example"}},
		{CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile, ClientCAFile: missing, AllowedClientSANs: []string{"collector.example"}},
		{CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile, ClientCAFile: fixture.CAFile, AllowedClientFingerprints: []string{"bad"}},
	} {
		credentials, err := LoadServerTLSCredentials(config)
		require.Nil(t, credentials)
		requireErrorType[*ErrTLSConfiguration](t, err)
	}
	for _, config := range []ClientTLSConfig{
		{CAFile: missing, CertificateFile: missing, PrivateKeyFile: missing, ServerName: "scope.example"},
		{CAFile: missing, CertificateFile: fixture.ClientCertFile, PrivateKeyFile: fixture.ClientKeyFile, ServerName: "scope.example"},
	} {
		credentials, err := LoadClientTLSCredentials(config)
		require.Nil(t, credentials)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(invalid, []byte("not a certificate"), 0600))
	pool, err := loadCertificatePool(invalid)
	require.Nil(t, pool)
	requireErrorType[*ErrTLSConfiguration](t, err)
}

// certificateFixture stores generated CA and leaf credential paths and parsed certificates.
//
// Example: TLS tests use one fixture for an oscilloscope server and collector client.
type certificateFixture struct {
	CAFile         string
	CACertificate  *x509.Certificate
	CAKey          ed25519.PrivateKey
	ServerCertFile string
	ServerKeyFile  string
	ClientCertFile string
	ClientKeyFile  string
	ClientCert     *x509.Certificate
}

// newCertificateFixture creates a private CA and TLS 1.3 server/client identities.
//
// Example: the client leaf contains the collector.example DNS SAN used by the allowlist.
func newCertificateFixture(t *testing.T) certificateFixture {
	t.Helper()
	directory := t.TempDir()
	now := time.Now().Add(-time.Minute)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "OWON test CA"},
		NotBefore:    now, NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caCertificate, caKey := createTestCertificate(t, caTemplate, caTemplate, nil)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "scope.example"},
		DNSNames: []string{"scope.example"}, NotBefore: now, NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverCertificate, serverKey := createTestCertificate(t, serverTemplate, caCertificate, caKey)
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "collector.example"},
		DNSNames: []string{"collector.example"}, NotBefore: now, NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientCertificate, clientKey := createTestCertificate(t, clientTemplate, caCertificate, caKey)

	return certificateFixture{
		CAFile:         writeCertificateFile(t, directory, "ca.pem", caCertificate.Raw),
		CACertificate:  caCertificate,
		CAKey:          caKey,
		ServerCertFile: writeCertificateFile(t, directory, "server.pem", serverCertificate.Raw),
		ServerKeyFile:  writePrivateKeyFile(t, directory, "server.key", serverKey),
		ClientCertFile: writeCertificateFile(t, directory, "client.pem", clientCertificate.Raw),
		ClientKeyFile:  writePrivateKeyFile(t, directory, "client.key", clientKey),
		ClientCert:     clientCertificate,
	}
}

// createTestCertificate creates and parses one Ed25519 certificate.
//
// Example: a nil signer creates a self-signed CA while a CA key signs leaf certificates.
func createTestCertificate(
	t *testing.T,
	template *x509.Certificate,
	parent *x509.Certificate,
	signer ed25519.PrivateKey,
) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	if signer == nil {
		signer = privateKey
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, signer)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return certificate, privateKey
}

// writeCertificateFile writes one PEM certificate fixture.
//
// Example: credential loaders receive the returned absolute temporary path.
func writeCertificateFile(
	t *testing.T,
	directory string,
	name string,
	der []byte,
) string {
	t.Helper()
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))

	return path
}

// writePrivateKeyFile writes one PKCS#8 PEM private key fixture.
//
// Example: tls.LoadX509KeyPair consumes the returned absolute temporary path.
func writePrivateKeyFile(
	t *testing.T,
	directory string,
	name string,
	key ed25519.PrivateKey,
) string {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600))

	return path
}

// TestLoadMutualTLSCredentials verifies both sides require complete authenticated identities.
//
// Example: an allowed collector SAN constructs server and client credentials successfully.
func TestLoadMutualTLSCredentials(t *testing.T) {
	t.Parallel()

	fixture := newCertificateFixture(t)
	serverCredentials, err := LoadServerTLSCredentials(ServerTLSConfig{
		CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile,
		ClientCAFile: fixture.CAFile, AllowedClientSANs: []string{"Collector.Example."},
	})
	require.NoError(t, err)
	require.NotNil(t, serverCredentials)
	clientCredentials, err := LoadClientTLSCredentials(ClientTLSConfig{
		CAFile: fixture.CAFile, CertificateFile: fixture.ClientCertFile,
		PrivateKeyFile: fixture.ClientKeyFile, ServerName: "scope.example",
	})
	require.NoError(t, err)
	require.NotNil(t, clientCredentials)

	_, err = LoadServerTLSCredentials(ServerTLSConfig{
		CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile, ClientCAFile: fixture.CAFile,
	})
	require.ErrorContains(t, err, "allowlist")
	_, err = LoadClientTLSCredentials(ClientTLSConfig{CAFile: fixture.CAFile})
	require.ErrorContains(t, err, "requires")
	_, err = LoadServerTLSCredentials(ServerTLSConfig{
		CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile,
		ClientCAFile: fixture.CAFile, AllowedClientSANs: []string{"spiffe://[invalid"},
	})
	require.ErrorContains(t, err, "valid URI")
}

// TestClientIdentityVerifierEnforcesSANOrFingerprint verifies explicit post-CA authorization.
//
// Example: a verified but unlisted collector is rejected until its fingerprint is allowed.
func TestClientIdentityVerifierEnforcesSANOrFingerprint(t *testing.T) {
	t.Parallel()

	fixture := newCertificateFixture(t)
	state := tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{fixture.ClientCert},
		VerifiedChains:   [][]*x509.Certificate{{fixture.ClientCert}},
	}
	require.NoError(t, (clientIdentityVerifier{sans: []string{"collector.example"}}).verify(state))
	require.Error(t, (clientIdentityVerifier{sans: []string{"other.example"}}).verify(state))
	digest := sha256.Sum256(fixture.ClientCert.Raw)
	require.NoError(t, (clientIdentityVerifier{fingerprints: []string{hex.EncodeToString(digest[:])}}).verify(state))
	wildcard := &x509.Certificate{DNSNames: []string{"*.example.com"}}
	wildcardState := tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{wildcard},
		VerifiedChains:   [][]*x509.Certificate{{wildcard}},
	}
	require.Error(t, (clientIdentityVerifier{sans: []string{"collector.example.com"}}).verify(wildcardState))
	require.Error(t, (clientIdentityVerifier{sans: []string{"collector.example"}}).verify(tls.ConnectionState{PeerCertificates: []*x509.Certificate{fixture.ClientCert}}))
}

// TestNormalizeAllowedSANAcceptsOpaqueURI verifies URI SANs do not require an authority.
//
// Example: a URN identity is matched as `uri:urn:owon:collector`.
func TestNormalizeAllowedSANAcceptsOpaqueURI(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeAllowedSAN("urn:owon:collector")
	require.NoError(t, err)
	require.Equal(t, "uri:urn:owon:collector", normalized)
}

// TestNormalizeAllowedSANRejectsMalformedURI verifies invalid identities fail before handshake.
//
// Example: a malformed bracketed URI cannot silently become a DNS allowlist entry.
func TestNormalizeAllowedSANRejectsMalformedURI(t *testing.T) {
	t.Parallel()

	_, err := normalizeAllowedSAN("spiffe://[invalid")
	require.Error(t, err)
}

// TestMutualTLSHandshake verifies CA, client certificate, SAN allowlist, and server name together.
//
// Example: a net.Pipe handshake succeeds only with both generated authenticated peers.
func TestMutualTLSHandshake(t *testing.T) {
	t.Parallel()

	fixture := newCertificateFixture(t)
	serverCredentials, err := LoadServerTLSCredentials(ServerTLSConfig{
		CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile,
		ClientCAFile: fixture.CAFile, AllowedClientSANs: []string{"Collector.Example."},
	})
	require.NoError(t, err)
	clientCredentials, err := LoadClientTLSCredentials(ClientTLSConfig{
		CAFile: fixture.CAFile, CertificateFile: fixture.ClientCertFile,
		PrivateKeyFile: fixture.ClientKeyFile, ServerName: "scope.example",
	})
	require.NoError(t, err)
	result := performTLSHandshake(t, serverCredentials, clientCredentials, "scope.example")
	require.NoError(t, result.Server)
	require.NoError(t, result.Client)
}

// TestMutualTLSHandshakeRejectsUnauthorizedPeers verifies authentication failures on real handshakes.
//
// Example: a CA-valid wildcard client certificate cannot satisfy an exact collector SAN allowlist.
func TestMutualTLSHandshakeRejectsUnauthorizedPeers(t *testing.T) {
	t.Parallel()

	fixture := newCertificateFixture(t)
	validServer, err := LoadServerTLSCredentials(ServerTLSConfig{
		CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile,
		ClientCAFile: fixture.CAFile, AllowedClientSANs: []string{"collector.example"},
	})
	require.NoError(t, err)
	validClient, err := LoadClientTLSCredentials(ClientTLSConfig{
		CAFile: fixture.CAFile, CertificateFile: fixture.ClientCertFile,
		PrivateKeyFile: fixture.ClientKeyFile, ServerName: "scope.example",
	})
	require.NoError(t, err)

	t.Run("unlisted exact SAN",
		// verifyUnlistedSAN rejects a CA-valid client identity absent from the allowlist.
		//
		// Example: `other.example` cannot satisfy an allowlist containing `collector.example`.
		func(t *testing.T) {
			serverCredentials, loadErr := LoadServerTLSCredentials(ServerTLSConfig{
				CertificateFile: fixture.ServerCertFile, PrivateKeyFile: fixture.ServerKeyFile,
				ClientCAFile: fixture.CAFile, AllowedClientSANs: []string{"other.example"},
			})
			require.NoError(t, loadErr)
			result := performTLSHandshake(t, serverCredentials, validClient, "scope.example")
			require.ErrorContains(t, result.Server, "absent from SAN")
		})

	t.Run("missing client certificate",
		// verifyMissingClientCertificate rejects a TLS peer that presents no certificate.
		//
		// Example: server-side mutual TLS fails before identity allowlist evaluation.
		func(t *testing.T) {
			roots, loadErr := loadCertificatePool(fixture.CAFile)
			require.NoError(t, loadErr)
			clientCredentials := credentials.NewTLS(&tls.Config{
				RootCAs: roots, ServerName: "scope.example", MinVersion: tls.VersionTLS13,
			})
			result := performTLSHandshake(t, validServer, clientCredentials, "scope.example")
			require.ErrorContains(t, result.Server, "client didn't provide a certificate")
		})

	t.Run("untrusted client certificate",
		// verifyUntrustedClientCertificate rejects a client signed by another CA.
		//
		// Example: a certificate with the right SAN still fails chain verification.
		func(t *testing.T) {
			untrusted := newCertificateFixture(t)
			clientCertificate, loadErr := tls.LoadX509KeyPair(untrusted.ClientCertFile, untrusted.ClientKeyFile)
			require.NoError(t, loadErr)
			roots, loadErr := loadCertificatePool(fixture.CAFile)
			require.NoError(t, loadErr)
			clientCredentials := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{clientCertificate}, RootCAs: roots,
				ServerName: "scope.example", MinVersion: tls.VersionTLS13,
			})
			result := performTLSHandshake(t, validServer, clientCredentials, "scope.example")
			require.ErrorContains(t, result.Server, "unknown authority")
		})

	t.Run("wildcard client SAN",
		// verifyWildcardSANRequiresExactIdentity rejects wildcard authorization.
		//
		// Example: `*.example` cannot authorize `scope.example` as a client identity.
		func(t *testing.T) {
			directory := t.TempDir()
			now := time.Now().Add(-time.Minute)
			wildcardTemplate := &x509.Certificate{
				SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "wildcard collector"},
				DNSNames: []string{"*.example"}, NotBefore: now, NotAfter: now.Add(time.Hour),
				KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			wildcardCertificate, wildcardKey := createTestCertificate(t, wildcardTemplate, fixture.CACertificate, fixture.CAKey)
			certificateFile := writeCertificateFile(t, directory, "wildcard.pem", wildcardCertificate.Raw)
			keyFile := writePrivateKeyFile(t, directory, "wildcard.key", wildcardKey)
			clientCredentials, loadErr := LoadClientTLSCredentials(ClientTLSConfig{
				CAFile: fixture.CAFile, CertificateFile: certificateFile,
				PrivateKeyFile: keyFile, ServerName: "scope.example",
			})
			require.NoError(t, loadErr)
			result := performTLSHandshake(t, validServer, clientCredentials, "scope.example")
			require.ErrorContains(t, result.Server, "absent from SAN")
		})

	t.Run("wrong server name",
		// verifyWrongServerName rejects a client that expects a different server identity.
		//
		// Example: the client reports the certificate-name mismatch before RPC use.
		func(t *testing.T) {
			clientCredentials, loadErr := LoadClientTLSCredentials(ClientTLSConfig{
				CAFile: fixture.CAFile, CertificateFile: fixture.ClientCertFile,
				PrivateKeyFile: fixture.ClientKeyFile, ServerName: "other.example",
			})
			require.NoError(t, loadErr)
			result := performTLSHandshake(t, validServer, clientCredentials, "other.example")
			require.ErrorContains(t, result.Client, "not other.example")
		})
}

// tlsHandshakeResult contains both sides of one in-memory mutual-TLS handshake.
//
// Example: rejection tests inspect the server result for client authorization failures.
type tlsHandshakeResult struct {
	Server error
	Client error
}

// performTLSHandshake runs both sides of one bounded in-memory handshake.
//
// Example: tests use the returned pair to distinguish server and client verification failures.
func performTLSHandshake(
	t *testing.T,
	serverCredentials credentials.TransportCredentials,
	clientCredentials credentials.TransportCredentials,
	serverName string,
) tlsHandshakeResult {
	t.Helper()
	serverRaw, clientRaw := net.Pipe()
	deadline := time.Now().Add(time.Second)
	require.NoError(t, serverRaw.SetDeadline(deadline))
	require.NoError(t, clientRaw.SetDeadline(deadline))
	serverResult := make(chan error, 1)
	clientResult := make(chan error, 1)
	observability.Go(context.Background(), runServerHandshake{Credentials: serverCredentials, Connection: serverRaw, Result: serverResult}.Run)
	observability.Go(context.Background(), runClientHandshake{
		Credentials: clientCredentials,
		Connection:  clientRaw,
		ServerName:  serverName,
		Result:      clientResult,
	}.Run)
	result := tlsHandshakeResult{Server: <-serverResult, Client: <-clientResult}
	require.NoError(t, errors.Join(serverRaw.Close(), clientRaw.Close()))

	return result
}

// runServerHandshake owns one TLS server-side handshake result.
//
// Example: observability.Go executes Run while the client handshake progresses.
type runServerHandshake struct {
	Credentials credentials.TransportCredentials
	Connection  net.Conn
	Result      chan<- error
}

// Run completes the server handshake and publishes its result.
//
// Example: a disallowed client SAN is returned to the test channel.
func (runner runServerHandshake) Run(_ context.Context) {
	_, _, err := runner.Credentials.ServerHandshake(runner.Connection)
	runner.Result <- err
}

// runClientHandshake owns one TLS client-side handshake result.
//
// Example: observability.Go executes Run while the server validates its certificate.
type runClientHandshake struct {
	Credentials credentials.TransportCredentials
	Connection  net.Conn
	ServerName  string
	Result      chan<- error
}

// Run completes the client handshake and publishes its result.
//
// Example: a server-name mismatch is returned to the test channel.
func (runner runClientHandshake) Run(ctx context.Context) {
	_, _, err := runner.Credentials.ClientHandshake(ctx, runner.ServerName, runner.Connection)
	runner.Result <- err
}

// requireErrorType verifies that an error chain contains one concrete error type.
//
// Example: requireErrorType[*ErrInvalidRequest](t, err) checks a validation result.
func requireErrorType[T error](
	t *testing.T,
	err error,
) {
	t.Helper()
	var typed T
	require.ErrorAs(t, err, &typed)
}
