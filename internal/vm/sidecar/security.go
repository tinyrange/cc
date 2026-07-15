package sidecar

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const WorkerTLSScheme = "tls://"

type WorkerSecurityReason string

const (
	WorkerSecurityPlaintextTCPRejected WorkerSecurityReason = "plaintext_tcp_rejected"
	WorkerSecurityTLSConfigRequired    WorkerSecurityReason = "tls_config_required"
	WorkerSecurityInvalidTLSConfig     WorkerSecurityReason = "invalid_tls_config"
	WorkerSecurityPeerScopeMismatch    WorkerSecurityReason = "peer_scope_mismatch"
	WorkerSecurityHandshakeFailed      WorkerSecurityReason = "handshake_failed"
)

type WorkerSecurityError struct {
	Endpoint string
	Reason   WorkerSecurityReason
	Detail   string
}

func (e *WorkerSecurityError) Error() string {
	if e == nil {
		return "invalid worker transport security"
	}
	switch e.Reason {
	case WorkerSecurityPlaintextTCPRejected:
		return fmt.Sprintf("worker endpoint %q uses unsupported plaintext TCP", e.Endpoint)
	case WorkerSecurityTLSConfigRequired:
		return fmt.Sprintf("worker endpoint %q requires mutual TLS configuration", e.Endpoint)
	case WorkerSecurityInvalidTLSConfig:
		return fmt.Sprintf("invalid worker mutual TLS configuration for %q: %s", e.Endpoint, e.Detail)
	case WorkerSecurityPeerScopeMismatch:
		return fmt.Sprintf("worker peer scope mismatch for %q: %s", e.Endpoint, e.Detail)
	case WorkerSecurityHandshakeFailed:
		return fmt.Sprintf("worker mutual TLS handshake failed for %q: %s", e.Endpoint, e.Detail)
	default:
		return fmt.Sprintf("worker transport security failed for %q: %s", e.Endpoint, e.Detail)
	}
}

type workerTLSRole string

const (
	workerTLSRoleWorker      workerTLSRole = "worker"
	workerTLSRoleCoordinator workerTLSRole = "coordinator"
)

type workerTLSFileConfig struct {
	Role             workerTLSRole `json:"role"`
	CertificateFile  string        `json:"certificate_file"`
	PrivateKeyFile   string        `json:"private_key_file"`
	PeerCAFile       string        `json:"peer_ca_file"`
	ServerName       string        `json:"server_name,omitempty"`
	Scope            string        `json:"scope"`
	HandshakeTimeout string        `json:"handshake_timeout"`
}

type WorkerTransportSecurity struct {
	TLSConfig        *tls.Config
	Scope            string
	HandshakeTimeout time.Duration
	reload           func() (*WorkerTransportSecurity, error)
}

func LoadWorkerServerSecurity(path string) (*WorkerTransportSecurity, error) {
	initial, absolutePath, err := loadWorkerTLSFile(path, workerTLSRoleWorker)
	if err != nil {
		return nil, workerTLSConfigError(path, err)
	}
	initial.reload = func() (*WorkerTransportSecurity, error) {
		current, _, err := loadWorkerTLSFile(absolutePath, workerTLSRoleWorker)
		if err != nil {
			return nil, workerTLSConfigError(absolutePath, err)
		}
		if current.Scope != initial.Scope {
			return nil, &WorkerSecurityError{
				Endpoint: absolutePath,
				Reason:   WorkerSecurityInvalidTLSConfig,
				Detail:   fmt.Sprintf("worker scope changed from %q to %q; restart is required", initial.Scope, current.Scope),
			}
		}
		return current, nil
	}
	return initial, nil
}

func LoadWorkerClientSecurity(path string) (*WorkerTransportSecurity, error) {
	security, _, err := loadWorkerTLSFile(path, workerTLSRoleCoordinator)
	if err != nil {
		return nil, workerTLSConfigError(path, err)
	}
	return security, nil
}

func workerTLSConfigError(endpoint string, err error) error {
	var securityErr *WorkerSecurityError
	if errors.As(err, &securityErr) {
		securityErr.Endpoint = endpoint
		return securityErr
	}
	return &WorkerSecurityError{
		Endpoint: endpoint,
		Reason:   WorkerSecurityInvalidTLSConfig,
		Detail:   err.Error(),
	}
}

func loadWorkerTLSFile(path string, role workerTLSRole) (*WorkerTransportSecurity, string, error) {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return nil, "", fmt.Errorf("resolve configuration path: %w", err)
	}
	if err := validateWorkerPrivateFile(absolutePath, "worker TLS configuration"); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, "", fmt.Errorf("read configuration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fileConfig workerTLSFileConfig
	if err := decoder.Decode(&fileConfig); err != nil {
		return nil, "", fmt.Errorf("decode configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, "", fmt.Errorf("decode configuration: %w", err)
	}
	if fileConfig.Role != role {
		return nil, "", fmt.Errorf("configuration role is %q, want %q", fileConfig.Role, role)
	}
	if err := validateWorkerScope(fileConfig.Scope); err != nil {
		return nil, "", err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(fileConfig.HandshakeTimeout))
	if err != nil || timeout <= 0 {
		return nil, "", fmt.Errorf("handshake_timeout must be a positive duration")
	}
	base := filepath.Dir(absolutePath)
	certificateFile, err := requiredWorkerTLSPath(base, "certificate_file", fileConfig.CertificateFile)
	if err != nil {
		return nil, "", err
	}
	privateKeyFile, err := requiredWorkerTLSPath(base, "private_key_file", fileConfig.PrivateKeyFile)
	if err != nil {
		return nil, "", err
	}
	peerCAFile, err := requiredWorkerTLSPath(base, "peer_ca_file", fileConfig.PeerCAFile)
	if err != nil {
		return nil, "", err
	}
	if err := validateWorkerPrivateFile(privateKeyFile, "worker TLS private key"); err != nil {
		return nil, "", err
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, "", fmt.Errorf("load certificate and private key: %w", err)
	}
	caPEM, err := os.ReadFile(peerCAFile)
	if err != nil {
		return nil, "", fmt.Errorf("read peer CA: %w", err)
	}
	peerCAs := x509.NewCertPool()
	if !peerCAs.AppendCertsFromPEM(caPEM) {
		return nil, "", fmt.Errorf("peer CA file contains no certificates")
	}

	config := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		Certificates:           []tls.Certificate{certificate},
		SessionTicketsDisabled: true,
	}
	peerRole := workerTLSRoleCoordinator
	if role == workerTLSRoleCoordinator {
		peerRole = workerTLSRoleWorker
		serverName := strings.TrimSpace(fileConfig.ServerName)
		if serverName == "" {
			return nil, "", fmt.Errorf("server_name is required for coordinator configuration")
		}
		config.RootCAs = peerCAs
		config.ServerName = serverName
	} else {
		config.ClientAuth = tls.RequireAndVerifyClientCert
		config.ClientCAs = peerCAs
	}
	config.VerifyConnection = verifyWorkerPeerScope(fileConfig.Scope, peerRole)
	return &WorkerTransportSecurity{
		TLSConfig:        config,
		Scope:            fileConfig.Scope,
		HandshakeTimeout: timeout,
	}, absolutePath, nil
}

func requiredWorkerTLSPath(base, field, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(base, path), nil
}

func validateWorkerScope(scope string) error {
	if len(scope) < 16 || len(scope) > 128 {
		return fmt.Errorf("scope must contain 16 to 128 URL-safe characters")
	}
	for _, char := range scope {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return fmt.Errorf("scope must contain only URL-safe characters")
	}
	return nil
}

func workerScopeURI(scope string, role workerTLSRole) *url.URL {
	return &url.URL{Scheme: "urn", Opaque: "cc:worker:" + scope + ":" + string(role)}
}

func verifyWorkerPeerScope(scope string, role workerTLSRole) func(tls.ConnectionState) error {
	expected := workerScopeURI(scope, role).String()
	return func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
			return &WorkerSecurityError{Reason: WorkerSecurityPeerScopeMismatch, Detail: "peer certificate was not verified"}
		}
		for _, identity := range state.VerifiedChains[0][0].URIs {
			if identity.String() == expected {
				return nil
			}
		}
		return &WorkerSecurityError{Reason: WorkerSecurityPeerScopeMismatch, Detail: "peer certificate does not contain the required worker scope"}
	}
}

func HandshakeWorkerServer(ctx context.Context, conn net.Conn, endpoint string, security *WorkerTransportSecurity) (*tls.Conn, error) {
	if security == nil || security.TLSConfig == nil {
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityTLSConfigRequired}
	}
	if security.reload != nil {
		current, err := security.reload()
		if err != nil {
			var securityErr *WorkerSecurityError
			if errors.As(err, &securityErr) {
				securityErr.Endpoint = endpoint
				return nil, securityErr
			}
			return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityInvalidTLSConfig, Detail: err.Error()}
		}
		security = current
	}
	if security.HandshakeTimeout <= 0 {
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityInvalidTLSConfig, Detail: "handshake timeout must be positive"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, security.HandshakeTimeout)
	defer cancel()
	tlsConn := tls.Server(conn, security.TLSConfig.Clone())
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		_ = tlsConn.Close()
		var securityErr *WorkerSecurityError
		if errors.As(err, &securityErr) {
			securityErr.Endpoint = endpoint
			return nil, securityErr
		}
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityHandshakeFailed, Detail: err.Error()}
	}
	return tlsConn, nil
}

func handshakeWorkerClient(ctx context.Context, conn net.Conn, endpoint string, security *WorkerTransportSecurity) (*tls.Conn, error) {
	if security == nil || security.TLSConfig == nil {
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityTLSConfigRequired}
	}
	if security.HandshakeTimeout <= 0 {
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityInvalidTLSConfig, Detail: "handshake timeout must be positive"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, security.HandshakeTimeout)
	defer cancel()
	tlsConn := tls.Client(conn, security.TLSConfig.Clone())
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		_ = tlsConn.Close()
		var securityErr *WorkerSecurityError
		if errors.As(err, &securityErr) {
			securityErr.Endpoint = endpoint
			return nil, securityErr
		}
		return nil, &WorkerSecurityError{Endpoint: endpoint, Reason: WorkerSecurityHandshakeFailed, Detail: err.Error()}
	}
	return tlsConn, nil
}

type EphemeralWorkerSecurity struct {
	ServerConfigPath string
	ClientConfigPath string
	Scope            string
	cleanup          func()
}

func (s *EphemeralWorkerSecurity) Close() {
	if s != nil && s.cleanup != nil {
		s.cleanup()
	}
}

func NewEphemeralWorkerSecurity(cacheDir string) (*EphemeralWorkerSecurity, error) {
	root := filepath.Join(cacheDir, "_worker-auth")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create worker authentication root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure worker authentication root: %w", err)
	}
	dir, err := os.MkdirTemp(root, "session-")
	if err != nil {
		return nil, fmt.Errorf("create worker authentication directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	fail := func(err error) (*EphemeralWorkerSecurity, error) {
		cleanup()
		return nil, err
	}

	scope, err := randomWorkerScope()
	if err != nil {
		return fail(err)
	}
	ca, caKey, caPEM, err := createWorkerCA()
	if err != nil {
		return fail(err)
	}
	workerCert, workerKey, err := createWorkerLeaf(ca, caKey, scope, workerTLSRoleWorker)
	if err != nil {
		return fail(err)
	}
	coordinatorCert, coordinatorKey, err := createWorkerLeaf(ca, caKey, scope, workerTLSRoleCoordinator)
	if err != nil {
		return fail(err)
	}
	files := map[string][]byte{
		"ca.pem":          caPEM,
		"worker.crt":      workerCert,
		"worker.key":      workerKey,
		"coordinator.crt": coordinatorCert,
		"coordinator.key": coordinatorKey,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return fail(fmt.Errorf("write ephemeral worker credential: %w", err))
		}
	}
	serverConfig := workerTLSFileConfig{
		Role:             workerTLSRoleWorker,
		CertificateFile:  "worker.crt",
		PrivateKeyFile:   "worker.key",
		PeerCAFile:       "ca.pem",
		Scope:            scope,
		HandshakeTimeout: "5s",
	}
	clientConfig := workerTLSFileConfig{
		Role:             workerTLSRoleCoordinator,
		CertificateFile:  "coordinator.crt",
		PrivateKeyFile:   "coordinator.key",
		PeerCAFile:       "ca.pem",
		ServerName:       "127.0.0.1",
		Scope:            scope,
		HandshakeTimeout: "5s",
	}
	serverConfigPath := filepath.Join(dir, "worker.json")
	clientConfigPath := filepath.Join(dir, "coordinator.json")
	if err := writeWorkerTLSFile(serverConfigPath, serverConfig); err != nil {
		return fail(err)
	}
	if err := writeWorkerTLSFile(clientConfigPath, clientConfig); err != nil {
		return fail(err)
	}
	return &EphemeralWorkerSecurity{
		ServerConfigPath: serverConfigPath,
		ClientConfigPath: clientConfigPath,
		Scope:            scope,
		cleanup:          cleanup,
	}, nil
}

func writeWorkerTLSFile(path string, config workerTLSFileConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write worker TLS configuration: %w", err)
	}
	return nil
}

func randomWorkerScope() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate worker scope: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func createWorkerCA() (*x509.Certificate, ed25519.PrivateKey, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now()
	serial, err := randomWorkerSerial()
	if err != nil {
		return nil, nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "cc ephemeral worker CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return nil, nil, nil, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	return certificate, privateKey, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func createWorkerLeaf(ca *x509.Certificate, caKey ed25519.PrivateKey, scope string, role workerTLSRole) ([]byte, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	serial, err := randomWorkerSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "cc worker " + string(role)},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		URIs:         []*url.URL{workerScopeURI(scope, role)},
	}
	if role == workerTLSRoleWorker {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, publicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func randomWorkerSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 120)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return serial, nil
}
