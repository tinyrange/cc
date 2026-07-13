package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerMutualTLSRejectsPlaintextBeforeHello(t *testing.T) {
	credentials, err := NewEphemeralWorkerSecurity(t.TempDir())
	if err != nil {
		t.Fatalf("create worker credentials: %v", err)
	}
	defer credentials.Close()
	serverSecurity, err := LoadWorkerServerSecurity(credentials.ServerConfigPath)
	if err != nil {
		t.Fatalf("load server security: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	endpoint := WorkerTLSScheme + listener.Addr().String()

	handshakes := make(chan error, 2)
	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			conn, err := listener.Accept()
			if err != nil {
				handshakes <- err
				continue
			}
			authenticated, err := HandshakeWorkerServer(context.Background(), conn, endpoint, serverSecurity)
			if err != nil {
				_ = conn.Close()
				handshakes <- err
				continue
			}
			codec := NewWorkerCodec(authenticated)
			err = codec.Send(mustWorkerFrame(0, WorkerFrameHello, WorkerHello{
				Version:  WorkerProtocolVersion,
				WorkerID: serverSecurity.Scope,
			}))
			_ = codec.Close()
			handshakes <- err
		}
	}()

	plaintext, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial plaintext: %v", err)
	}
	if _, err := plaintext.Write([]byte(`{"service":"control","type":"hello"}` + "\n")); err != nil {
		t.Fatalf("write plaintext frame: %v", err)
	}
	if err := plaintext.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set plaintext deadline: %v", err)
	}
	buf := make([]byte, 1)
	if n, err := plaintext.Read(buf); err == nil || n != 0 {
		t.Fatalf("plaintext peer read %d bytes before authentication, err=%v", n, err)
	}
	_ = plaintext.Close()
	var securityErr *WorkerSecurityError
	if err := <-handshakes; !errors.As(err, &securityErr) || securityErr.Reason != WorkerSecurityHandshakeFailed {
		t.Fatalf("plaintext handshake error = %T %v", err, err)
	}

	worker, err := DialWorkerTLS(context.Background(), endpoint, credentials.ClientConfigPath)
	if err != nil {
		t.Fatalf("dial authenticated worker: %v", err)
	}
	_ = worker.Close()
	if err := <-handshakes; err != nil {
		t.Fatalf("authenticated handshake: %v", err)
	}
}

func TestWorkerMutualTLSEnforcesCertificateScope(t *testing.T) {
	credentials, err := NewEphemeralWorkerSecurity(t.TempDir())
	if err != nil {
		t.Fatalf("create worker credentials: %v", err)
	}
	defer credentials.Close()
	serverSecurity, err := LoadWorkerServerSecurity(credentials.ServerConfigPath)
	if err != nil {
		t.Fatalf("load server security: %v", err)
	}
	clientConfig := readWorkerTLSConfigForTest(t, credentials.ClientConfigPath)
	clientConfig.Scope = "different_worker_scope"
	writeWorkerTLSConfigForTest(t, credentials.ClientConfigPath, clientConfig)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	endpoint := WorkerTLSScheme + listener.Addr().String()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		_, err = HandshakeWorkerServer(context.Background(), conn, endpoint, serverSecurity)
		_ = conn.Close()
		serverDone <- err
	}()

	_, err = DialWorkerTLS(context.Background(), endpoint, credentials.ClientConfigPath)
	var securityErr *WorkerSecurityError
	if !errors.As(err, &securityErr) {
		t.Fatalf("scope error type = %T, want WorkerSecurityError", err)
	}
	if securityErr.Reason != WorkerSecurityPeerScopeMismatch {
		t.Fatalf("scope error reason = %q", securityErr.Reason)
	}
	<-serverDone
}

func TestWorkerServerReloadsRotatedCredentialsWithinScope(t *testing.T) {
	credentials, err := NewEphemeralWorkerSecurity(t.TempDir())
	if err != nil {
		t.Fatalf("create worker credentials: %v", err)
	}
	defer credentials.Close()
	serverSecurity, err := LoadWorkerServerSecurity(credentials.ServerConfigPath)
	if err != nil {
		t.Fatalf("load server security: %v", err)
	}
	dir := filepath.Dir(credentials.ServerConfigPath)
	rotateWorkerCredentialsForTest(t, dir, credentials.Scope)
	clientSecurity, err := LoadWorkerClientSecurity(credentials.ClientConfigPath)
	if err != nil {
		t.Fatalf("load rotated client security: %v", err)
	}

	left, right := net.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := HandshakeWorkerServer(context.Background(), left, "pipe", serverSecurity)
		if err == nil {
			_ = conn.Close()
		}
		serverDone <- err
	}()
	clientConn, err := handshakeWorkerClient(context.Background(), right, "pipe", clientSecurity)
	if err != nil {
		t.Fatalf("handshake with rotated credentials: %v", err)
	}
	_ = clientConn.Close()
	if err := <-serverDone; err != nil {
		t.Fatalf("server handshake with rotated credentials: %v", err)
	}
}

func rotateWorkerCredentialsForTest(t *testing.T, dir, scope string) {
	t.Helper()
	ca, caKey, caPEM, err := createWorkerCA()
	if err != nil {
		t.Fatalf("create rotated CA: %v", err)
	}
	workerCert, workerKey, err := createWorkerLeaf(ca, caKey, scope, workerTLSRoleWorker)
	if err != nil {
		t.Fatalf("create rotated worker certificate: %v", err)
	}
	coordinatorCert, coordinatorKey, err := createWorkerLeaf(ca, caKey, scope, workerTLSRoleCoordinator)
	if err != nil {
		t.Fatalf("create rotated coordinator certificate: %v", err)
	}
	for name, data := range map[string][]byte{
		"ca.pem":          caPEM,
		"worker.crt":      workerCert,
		"worker.key":      workerKey,
		"coordinator.crt": coordinatorCert,
		"coordinator.key": coordinatorKey,
	} {
		atomicReplaceWorkerFileForTest(t, filepath.Join(dir, name), data)
	}
}

func atomicReplaceWorkerFileForTest(t *testing.T, path string, data []byte) {
	t.Helper()
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		t.Fatalf("write rotated credential: %v", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatalf("replace rotated credential: %v", err)
	}
}

func readWorkerTLSConfigForTest(t *testing.T, path string) workerTLSFileConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read worker TLS config: %v", err)
	}
	var config workerTLSFileConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode worker TLS config: %v", err)
	}
	return config
}

func writeWorkerTLSConfigForTest(t *testing.T, path string, config workerTLSFileConfig) {
	t.Helper()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("encode worker TLS config: %v", err)
	}
	atomicReplaceWorkerFileForTest(t, path, append(data, '\n'))
}
