package ccvmd

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type ListenerSecurityReason string

const (
	ListenerSecurityInvalidAddress               ListenerSecurityReason = "invalid_address"
	ListenerSecurityRemoteAuthenticationRequired ListenerSecurityReason = "remote_authentication_required"
	ListenerSecurityInvalidMutualTLS             ListenerSecurityReason = "invalid_mutual_tls"
	ListenerSecurityConflictingAuthentication    ListenerSecurityReason = "conflicting_authentication"
)

type ListenerSecurityError struct {
	Address string
	Reason  ListenerSecurityReason
	Detail  string
}

func (e *ListenerSecurityError) Error() string {
	if e == nil {
		return "invalid listener security configuration"
	}
	switch e.Reason {
	case ListenerSecurityInvalidAddress:
		return fmt.Sprintf("invalid listener address %q: %s", e.Address, e.Detail)
	case ListenerSecurityRemoteAuthenticationRequired:
		return fmt.Sprintf("listener %q is remotely reachable and requires mutual TLS authentication", e.Address)
	case ListenerSecurityInvalidMutualTLS:
		return fmt.Sprintf("invalid mutual TLS configuration for listener %q: %s", e.Address, e.Detail)
	case ListenerSecurityConflictingAuthentication:
		return fmt.Sprintf("listener %q has both programmatic and file-based authentication", e.Address)
	default:
		return fmt.Sprintf("invalid listener security configuration for %q: %s", e.Address, e.Detail)
	}
}

type ServerAuthentication struct {
	tlsConfig *tls.Config
}

func NewMutualTLSAuthentication(config *tls.Config) (*ServerAuthentication, error) {
	wrapped, err := validatedMutualTLSConfig(config)
	if err != nil {
		return nil, &ListenerSecurityError{
			Reason: ListenerSecurityInvalidMutualTLS,
			Detail: err.Error(),
		}
	}
	return &ServerAuthentication{tlsConfig: wrapped}, nil
}

func (a *ServerAuthentication) listener(listener net.Listener) net.Listener {
	return tls.NewListener(listener, a.tlsConfig.Clone())
}

func validatedMutualTLSConfig(config *tls.Config) (*tls.Config, error) {
	if config == nil {
		return nil, fmt.Errorf("TLS configuration is required")
	}
	source := config.Clone()
	// A resumed TLS session can skip fresh client-certificate verification.
	// Disable tickets so every new connection observes certificate and CA
	// rotation through the validated configuration callback.
	source.SessionTicketsDisabled = true
	if err := validateMutualTLSPolicy(source, source.GetConfigForClient != nil); err != nil {
		return nil, err
	}
	getConfigForClient := source.GetConfigForClient
	if getConfigForClient == nil {
		return source, nil
	}

	wrapped := source.Clone()
	wrapped.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		selected, err := getConfigForClient(hello)
		if err != nil {
			return nil, err
		}
		if selected == nil {
			return nil, fmt.Errorf("mutual TLS configuration callback returned nil")
		}
		selected = selected.Clone()
		selected.SessionTicketsDisabled = true
		if err := validateMutualTLSPolicy(selected, false); err != nil {
			return nil, err
		}
		selected.GetConfigForClient = nil
		return selected, nil
	}
	return wrapped, nil
}

func validateMutualTLSPolicy(config *tls.Config, callbackProvidesMaterial bool) error {
	if config.MinVersion < tls.VersionTLS13 {
		return fmt.Errorf("minimum TLS version must be TLS 1.3")
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		return fmt.Errorf("client authentication must require and verify a certificate")
	}
	if !callbackProvidesMaterial && len(config.Certificates) == 0 && config.GetCertificate == nil {
		return fmt.Errorf("server certificate is required")
	}
	if !callbackProvidesMaterial && config.ClientCAs == nil {
		return fmt.Errorf("client CA pool is required")
	}
	return nil
}

func resolveServerAuthentication(address, configPath string, configured *ServerAuthentication) (*ServerAuthentication, error) {
	configPath = strings.TrimSpace(configPath)
	if configured != nil && configPath != "" {
		return nil, &ListenerSecurityError{
			Address: address,
			Reason:  ListenerSecurityConflictingAuthentication,
		}
	}
	authentication := configured
	if configPath != "" {
		loaded, err := newFileMutualTLSAuthentication(configPath)
		if err != nil {
			var securityErr *ListenerSecurityError
			if errors.As(err, &securityErr) {
				securityErr.Address = address
				return nil, securityErr
			}
			return nil, &ListenerSecurityError{
				Address: address,
				Reason:  ListenerSecurityInvalidMutualTLS,
				Detail:  err.Error(),
			}
		}
		authentication = loaded
	}

	required, err := listenerAddressRequiresAuthentication(address)
	if err != nil {
		return nil, err
	}
	if required && authentication == nil {
		return nil, &ListenerSecurityError{
			Address: address,
			Reason:  ListenerSecurityRemoteAuthenticationRequired,
		}
	}
	return authentication, nil
}

func listenerAddressRequiresAuthentication(address string) (bool, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false, &ListenerSecurityError{
			Address: address,
			Reason:  ListenerSecurityInvalidAddress,
			Detail:  err.Error(),
		}
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	if host == "" {
		return true, nil
	}
	if strings.EqualFold(host, "localhost") {
		return false, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname can resolve to different or mixed addresses over time. Only
		// the reserved localhost name is classified as local without mTLS.
		return true, nil
	}
	return !ip.IsLoopback(), nil
}

func listenerAddrRequiresAuthentication(address net.Addr) bool {
	if address == nil {
		return true
	}
	tcpAddress, ok := address.(*net.TCPAddr)
	if !ok || tcpAddress.IP == nil {
		return true
	}
	return !tcpAddress.IP.IsLoopback()
}

type mutualTLSFileConfig struct {
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
	ClientCAFile    string `json:"client_ca_file"`
}

func newFileMutualTLSAuthentication(configPath string) (*ServerAuthentication, error) {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve TLS configuration path: %w", err)
	}
	if err := validatePrivateConfigFile(configPath); err != nil {
		return nil, err
	}
	files, err := readMutualTLSFileConfig(configPath)
	if err != nil {
		return nil, err
	}
	resolveRelativeTLSPaths(filepath.Dir(configPath), &files)
	initial, err := loadMutualTLSFiles(files)
	if err != nil {
		return nil, err
	}

	config := initial.Clone()
	config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return loadMutualTLSFiles(files)
	}
	return NewMutualTLSAuthentication(config)
}

func readMutualTLSFileConfig(path string) (mutualTLSFileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mutualTLSFileConfig{}, fmt.Errorf("read TLS configuration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config mutualTLSFileConfig
	if err := decoder.Decode(&config); err != nil {
		return mutualTLSFileConfig{}, fmt.Errorf("decode TLS configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return mutualTLSFileConfig{}, fmt.Errorf("decode TLS configuration: %w", err)
	}
	if strings.TrimSpace(config.CertificateFile) == "" {
		return mutualTLSFileConfig{}, fmt.Errorf("certificate_file is required")
	}
	if strings.TrimSpace(config.PrivateKeyFile) == "" {
		return mutualTLSFileConfig{}, fmt.Errorf("private_key_file is required")
	}
	if strings.TrimSpace(config.ClientCAFile) == "" {
		return mutualTLSFileConfig{}, fmt.Errorf("client_ca_file is required")
	}
	return config, nil
}

func resolveRelativeTLSPaths(base string, config *mutualTLSFileConfig) {
	config.CertificateFile = resolveRelativeTLSPath(base, config.CertificateFile)
	config.PrivateKeyFile = resolveRelativeTLSPath(base, config.PrivateKeyFile)
	config.ClientCAFile = resolveRelativeTLSPath(base, config.ClientCAFile)
}

func resolveRelativeTLSPath(base, path string) string {
	path = strings.TrimSpace(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func loadMutualTLSFiles(config mutualTLSFileConfig) (*tls.Config, error) {
	if err := validatePrivateKeyFile(config.PrivateKeyFile); err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server certificate and private key: %w", err)
	}
	caPEM, err := os.ReadFile(config.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA bundle: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("client CA bundle contains no certificates")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}
