package ccvmd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListenerAddressAuthenticationClassification(t *testing.T) {
	tests := []struct {
		address string
		remote  bool
	}{
		{address: "localhost:0"},
		{address: "127.0.0.1:8080"},
		{address: "[::1]:8080"},
		{address: "0.0.0.0:8080", remote: true},
		{address: "[::]:8080", remote: true},
		{address: "192.0.2.10:8080", remote: true},
		{address: "node.tailnet.ts.net:8080", remote: true},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			remote, err := listenerAddressRequiresAuthentication(test.address)
			if err != nil {
				t.Fatalf("classify listener: %v", err)
			}
			if remote != test.remote {
				t.Fatalf("remote = %t, want %t", remote, test.remote)
			}
		})
	}
}

func TestRemoteListenerRequiresTypedAuthentication(t *testing.T) {
	_, err := resolveServerAuthentication("0.0.0.0:0", "", nil)
	var securityErr *ListenerSecurityError
	if !errors.As(err, &securityErr) {
		t.Fatalf("error type = %T, want ListenerSecurityError", err)
	}
	if securityErr.Reason != ListenerSecurityRemoteAuthenticationRequired {
		t.Fatalf("reason = %q, want %q", securityErr.Reason, ListenerSecurityRemoteAuthenticationRequired)
	}

	if _, err := resolveServerAuthentication("localhost:0", "", nil); err != nil {
		t.Fatalf("local listener rejected: %v", err)
	}
}

func TestMutualTLSAuthenticationRejectsWeakPolicy(t *testing.T) {
	_, err := NewMutualTLSAuthentication(&tls.Config{MinVersion: tls.VersionTLS13})
	var securityErr *ListenerSecurityError
	if !errors.As(err, &securityErr) {
		t.Fatalf("error type = %T, want ListenerSecurityError", err)
	}
	if securityErr.Reason != ListenerSecurityInvalidMutualTLS {
		t.Fatalf("reason = %q, want %q", securityErr.Reason, ListenerSecurityInvalidMutualTLS)
	}
}

func TestFileMutualTLSProtectsAllRoutesAndReloadsClientCA(t *testing.T) {
	dir := t.TempDir()
	serverCA := newTestCertificateAuthority(t, "server-ca")
	clientCA1 := newTestCertificateAuthority(t, "client-ca-1")
	clientCA2 := newTestCertificateAuthority(t, "client-ca-2")
	serverCert, serverKey := serverCA.issue(t, "ccvmd", true)
	clientCert1, clientKey1 := clientCA1.issue(t, "client-1", false)
	clientCert2, clientKey2 := clientCA2.issue(t, "client-2", false)

	writeTestFile(t, filepath.Join(dir, "server.crt"), serverCert, 0o644)
	writeTestFile(t, filepath.Join(dir, "server.key"), serverKey, 0o600)
	writeTestFile(t, filepath.Join(dir, "clients.pem"), clientCA1.certPEM, 0o644)
	configData, err := json.Marshal(mutualTLSFileConfig{
		CertificateFile: "server.crt",
		PrivateKeyFile:  "server.key",
		ClientCAFile:    "clients.pem",
	})
	if err != nil {
		t.Fatalf("marshal TLS config: %v", err)
	}
	configPath := filepath.Join(dir, "tls.json")
	writeTestFile(t, configPath, configData, 0o600)

	authentication, err := newFileMutualTLSAuthentication(configPath)
	if err != nil {
		t.Fatalf("load mutual TLS authentication: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(authentication.listener(listener))
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		<-serveDone
	})

	serverRoots := x509.NewCertPool()
	if !serverRoots.AppendCertsFromPEM(serverCA.certPEM) {
		t.Fatal("append server CA")
	}
	client1 := newMutualTLSTestClient(t, serverRoots, clientCert1, clientKey1)
	client2 := newMutualTLSTestClient(t, serverRoots, clientCert2, clientKey2)
	unauthenticated := newMutualTLSTestClient(t, serverRoots, nil, nil)
	baseURL := "https://" + listener.Addr().String()

	for _, path := range []string{"/healthz", "/debug/virtiofs", "/vm/run", "/stream"} {
		if status, err := requestTestRoute(client1, baseURL+path); err != nil || status != http.StatusNoContent {
			t.Fatalf("authenticated route %s: status=%d err=%v", path, status, err)
		}
		if _, err := requestTestRoute(unauthenticated, baseURL+path); err == nil {
			t.Fatalf("unauthenticated route %s completed TLS", path)
		}
	}

	atomicReplaceTestFile(t, filepath.Join(dir, "clients.pem"), clientCA2.certPEM, 0o644)
	if status, err := requestTestRoute(client2, baseURL+"/healthz"); err != nil || status != http.StatusNoContent {
		t.Fatalf("rotated client CA was not used: status=%d err=%v", status, err)
	}
	if _, err := requestTestRoute(client1, baseURL+"/healthz"); err == nil {
		t.Fatal("client signed by removed CA completed a new TLS connection")
	}
}

type testCertificateAuthority struct {
	certificate *x509.Certificate
	privateKey  ed25519.PrivateKey
	certPEM     []byte
}

func newTestCertificateAuthority(t *testing.T, name string) testCertificateAuthority {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          randomTestSerial(t),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return testCertificateAuthority{
		certificate: certificate,
		privateKey:  privateKey,
		certPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (ca testCertificateAuthority) issue(t *testing.T, name string, server bool) ([]byte, []byte) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: randomTestSerial(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, publicKey, ca.privateKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func randomTestSerial(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatalf("generate certificate serial: %v", err)
	}
	return serial
}

func newMutualTLSTestClient(t *testing.T, roots *x509.CertPool, certPEM, keyPEM []byte) *http.Client {
	t.Helper()
	config := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}
	if len(certPEM) != 0 {
		certificate, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			t.Fatalf("load client key pair: %v", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig:   config,
		DisableKeepAlives: true,
	}}
}

func requestTestRoute(client *http.Client, target string) (int, error) {
	response, err := client.Get(target)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, nil
}

func writeTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func atomicReplaceTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	temporary := path + ".new"
	writeTestFile(t, temporary, data, mode)
	if err := os.Rename(temporary, path); err != nil {
		t.Fatalf("replace %s: %v", path, err)
	}
}
