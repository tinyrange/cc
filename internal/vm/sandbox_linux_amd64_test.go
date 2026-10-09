//go:build linux && amd64

package vm

import (
	"context"
	"io"
	"j5.nz/cc/client"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxGuestHTTPWithoutInternet(t *testing.T) {
	env := newRuntimeBootEnv(t)
	helperDir := t.TempDir()
	source := `package main
import("net";"net/http";"os";"time")
func main(){
 if os.Args[1]=="probe" {c,e:=net.DialTimeout("tcp","1.1.1.1:80",300*time.Millisecond);if e==nil{c.Close();os.Exit(1)};return}
 if e:=http.ListenAndServe(os.Args[1],http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Write([]byte("guest-netstack"))}));e!=nil{panic(e)}
}`
	sourcePath := filepath.Join(helperDir, "server.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(helperDir, "server"), sourcePath)
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build guest HTTP helper: %v %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inst, err := StartSandbox(ctx, env.backend, client.CreateInstanceRequest{ID: "sandbox-http", Image: env.imageName, Shares: []client.ShareMount{{Source: helperDir, Mount: "/test-helper"}}, DefaultUser: "root", MemoryMB: 768, CPUs: 1, Network: &client.NetworkConfig{Enabled: true, AllowInternet: false, BlockHostAccess: true}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	dial := inst.(interface {
		DialGuestContext(context.Context, string, string) (net.Conn, error)
	})
	guestIP := inst.(interface{ NetworkIPv4() string }).NetworkIPv4()
	for _, addr := range []string{"127.0.0.1:80", "localhost:80", "169.254.169.254:80", "1.1.1.1:80"} {
		if c, err := dial.DialGuestContext(ctx, "tcp", addr); err == nil {
			c.Close()
			t.Fatalf("accepted non-guest target %s", addr)
		}
	}
	root := execInRuntimeRequest(t, inst, client.ExecRequest{Command: []string{"id", "-u"}, User: "root"})
	user := execInRuntimeRequest(t, inst, client.ExecRequest{Command: []string{"id", "-u"}, User: "nobody"})
	if strings.TrimSpace(root.Output) != "0" || strings.TrimSpace(user.Output) == "0" {
		t.Fatalf("guest user selection root=%q nobody=%q", root.Output, user.Output)
	}
	execInRuntimeRequest(t, inst, client.ExecRequest{SkipResolve: true, Command: []string{"/test-helper/server", "probe"}})
	serveCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- inst.ExecStream(serveCtx, client.ExecRequest{SkipResolve: true, Command: []string{"/test-helper/server", "0.0.0.0:8000"}}, nil, func(event client.ExecEvent) error { t.Logf("guest service event: %+v", event); return nil })
	}()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: dial.DialGuestContext}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	var body []byte
	for time.Now().Before(deadline) {
		response, e := httpClient.Get("http://" + net.JoinHostPort(guestIP, "8000") + "/")
		if e == nil {
			body, e = io.ReadAll(response.Body)
			response.Body.Close()
			if e == nil {
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("guest service exited: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if string(body) != "guest-netstack" {
		t.Fatalf("guest HTTP body %q", body)
	}
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("guest service did not cancel")
	}
	// A Linux listener bound only to guest loopback is NOT assumed reachable
	// through the guest NIC. Verify that limitation rather than host-fallback.
	loopCtx, loopStop := context.WithCancel(ctx)
	defer loopStop()
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- inst.ExecStream(loopCtx, clientpkgExecLoopback(), nil, func(event client.ExecEvent) error { t.Logf("guest service event: %+v", event); return nil })
	}()
	ready := false
	for attempt := 0; attempt < 50; attempt++ {
		response, err := inst.Exec(ctx, client.ExecRequest{Command: []string{"cat", "/proc/net/tcp"}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(response.Output, "0100007F:1F41") {
			ready = true
			break
		}
		select {
		case err := <-loopDone:
			t.Fatalf("loopback service exited: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("loopback listener was not established")
	}

	probeCtx, probeCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer probeCancel()
	if c, err := dial.DialGuestContext(probeCtx, "tcp", net.JoinHostPort(guestIP, "8001")); err == nil {
		c.Close()
		t.Fatal("unexpected loopback-only service reachability; inspect routing contract")
	}
	loopStop()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("loopback service did not cancel")
	}
}
func clientpkgExecLoopback() client.ExecRequest {
	return client.ExecRequest{SkipResolve: true, Command: []string{"/test-helper/server", "127.0.0.1:8001"}}
}
