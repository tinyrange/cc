//go:build linux && amd64

package vm

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vmruntime"
)

func TestRuntimeBackendInitramfsReady(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernel := alpine.NewManager(t.TempDir())
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	backend := NewRuntimeBackend(kernel, nil, t.TempDir())
	resp, err := backend.Run(ctx, client.RunRequest{MemoryMB: 256, Dmesg: true})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, vmruntime.InstanceReadyMarker) {
		t.Fatalf("output missing ready marker %q:\n%s", vmruntime.InstanceReadyMarker, resp.Output)
	}
}

func TestRuntimeBackendRunCommand(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Command:  []string{"sh", "-c", "echo linux-amd64-ok"},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-ok" {
		t.Fatalf("backend.Run().Output = %q, want linux-amd64-ok", resp.Output)
	}
}

func TestRuntimeBackendRunCommandWithNetworkDevice(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Network:  &client.NetworkConfig{Enabled: true},
		Command:  []string{"sh", "-c", "ls /sys/class/net && test -d /sys/class/net/eth0 && ip addr show lo && ip addr show eth0 && ping -c 1 -W 1 127.0.0.1 && ip route && cat /etc/resolv.conf && ping -c 1 -W 1 10.42.0.1 && nslookup host.containers.internal 10.42.0.1 && echo network-device-ok"},
		MemoryMB: 256,
		User:     "0:0",
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, "network-device-ok") {
		t.Fatalf("output missing network success marker:\n%s", resp.Output)
	}
}

func TestRuntimeBackendPortForwardToGuestWebServer(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	hostPort := reserveLocalTCPPort(t)
	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
	inst, err := backend.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Network: &client.NetworkConfig{
			Enabled: true,
			PortForwards: []client.PortForward{
				{Protocol: "tcp", HostAddr: "127.0.0.1", HostPort: hostPort, GuestPort: 8080},
			},
		},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Start() error = %v", err)
	}
	defer inst.Close()

	resp, err := inst.Exec(ctx, client.ExecRequest{
		Command: []string{"sh", "-c", "while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: 13\\r\\nConnection: close\\r\\n\\r\\nguest-web-ok\\n' | nc -l -p 8080; done >/tmp/cc-port-forward.log 2>&1 & echo server-ready"},
	})
	if err != nil {
		t.Fatalf("start guest web server error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 || !strings.Contains(resp.Output, "server-ready") {
		t.Fatalf("start guest web server exit=%d output:\n%s", resp.ExitCode, resp.Output)
	}

	body := fetchWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/", hostPort), 5*time.Second)
	if strings.TrimSpace(body) != "guest-web-ok" {
		t.Fatalf("unexpected forwarded response %q", body)
	}
}

func TestRuntimeBackendRunsTwoNamedVMsConcurrently(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	hostPortOne := reserveLocalTCPPort(t)
	hostPortTwo := reserveLocalTCPPort(t)
	root := t.TempDir()
	t.Setenv("CCX3_OCI_SHARED_CACHE_DIR", filepath.Join(root, "shared-oci-cache"))
	shareDir := filepath.Join(root, "share-one")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("create share dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "secret.txt"), []byte("one-share\n"), 0o644); err != nil {
		t.Fatalf("write share file: %v", err)
	}

	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	for _, image := range []string{"alpine-one", "alpine-two"} {
		if _, err := store.Pull(ctx, image, fixture); err != nil {
			t.Fatalf("store.Pull(%s) error = %v", image, err)
		}
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit")))
	defer mgr.ShutdownAll(context.Background())

	for _, tc := range []struct {
		id       string
		image    string
		hostPort int
	}{
		{id: "one", image: "alpine-one", hostPort: hostPortOne},
		{id: "two", image: "alpine-two", hostPort: hostPortTwo},
	} {
		state, err := mgr.Start(ctx, client.CreateInstanceRequest{
			ID:    tc.id,
			Image: tc.image,
			Network: &client.NetworkConfig{
				Enabled: true,
				PortForwards: []client.PortForward{{
					Protocol:  "tcp",
					HostAddr:  "127.0.0.1",
					HostPort:  tc.hostPort,
					GuestPort: 8080,
				}},
			},
			MemoryMB: 256,
		})
		if err != nil {
			t.Fatalf("Start(%s) error = %v", tc.id, err)
		}
		if state.ID != tc.id || state.Image != tc.image || state.Status != "running" {
			t.Fatalf("Start(%s) state = %#v, want running with matching id and image", tc.id, state)
		}
	}

	respOne, err := mgr.RunIn(ctx, "one", client.RunRequest{
		Shares:  []client.ShareMount{{Source: shareDir, Mount: "/.share/one"}},
		Command: []string{"sh", "-c", "printf one: && cat /.share/one/secret.txt"},
	})
	if err != nil {
		t.Fatalf("RunIn(one share) error = %v\noutput:\n%s", err, respOne.Output)
	}
	if strings.TrimSpace(respOne.Output) != "one:one-share" {
		t.Fatalf("RunIn(one share) output = %q, want one:one-share", respOne.Output)
	}

	respTwo, err := mgr.RunIn(ctx, "two", client.RunRequest{
		Command: []string{"sh", "-c", "test ! -e /.share/one/secret.txt && echo two-isolated"},
	})
	if err != nil {
		t.Fatalf("RunIn(two isolation) error = %v\noutput:\n%s", err, respTwo.Output)
	}
	if strings.TrimSpace(respTwo.Output) != "two-isolated" {
		t.Fatalf("RunIn(two isolation) output = %q, want two-isolated", respTwo.Output)
	}

	for _, tc := range []struct {
		id          string
		hostPort    int
		bodyLiteral string
	}{
		{id: "one", hostPort: hostPortOne, bodyLiteral: "one-ok\\n"},
		{id: "two", hostPort: hostPortTwo, bodyLiteral: "two-ok\\n"},
	} {
		command := fmt.Sprintf("while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: 7\\r\\nConnection: close\\r\\n\\r\\n%s' | nc -l -p 8080; done >/tmp/cc-port-forward.log 2>&1 & echo server-ready", tc.bodyLiteral)
		resp, err := mgr.RunIn(ctx, tc.id, client.RunRequest{Command: []string{"sh", "-c", command}})
		if err != nil {
			t.Fatalf("RunIn(%s web server) error = %v\noutput:\n%s", tc.id, err, resp.Output)
		}
		if resp.ExitCode != 0 || !strings.Contains(resp.Output, "server-ready") {
			t.Fatalf("RunIn(%s web server) exit=%d output:\n%s", tc.id, resp.ExitCode, resp.Output)
		}
	}

	if body := fetchWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/", hostPortOne), 5*time.Second); body != "one-ok\n" {
		t.Fatalf("forward one response = %q, want one-ok", body)
	}
	if body := fetchWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/", hostPortTwo), 5*time.Second); body != "two-ok\n" {
		t.Fatalf("forward two response = %q, want two-ok", body)
	}

	err = mgr.AddPortForwardTo(ctx, "two", client.PortForward{
		Protocol:  "tcp",
		HostAddr:  "127.0.0.1",
		HostPort:  hostPortOne,
		GuestPort: 8081,
	})
	if err == nil || !strings.Contains(err.Error(), "listen port forward") {
		t.Fatalf("duplicate host forward error = %v, want listen port forward error", err)
	}

	if err := mgr.ShutdownInstance(ctx, "one"); err != nil {
		t.Fatalf("ShutdownInstance(one) error = %v", err)
	}
	respTwo, err = mgr.RunIn(ctx, "two", client.RunRequest{Command: []string{"sh", "-c", "echo two-still-running"}})
	if err != nil {
		t.Fatalf("RunIn(two after one shutdown) error = %v\noutput:\n%s", err, respTwo.Output)
	}
	if strings.TrimSpace(respTwo.Output) != "two-still-running" {
		t.Fatalf("RunIn(two after one shutdown) output = %q, want two-still-running", respTwo.Output)
	}
}

func TestRuntimeBackendVMToVMNginxCurl(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root := t.TempDir()
	t.Setenv("CCX3_OCI_SHARED_CACHE_DIR", filepath.Join(root, "shared-oci-cache"))
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	for _, image := range []string{"alpine-nginx", "alpine-curl"} {
		if _, err := store.Pull(ctx, image, fixture); err != nil {
			t.Fatalf("store.Pull(%s) error = %v", image, err)
		}
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit")))
	defer mgr.ShutdownAll(context.Background())

	network := &client.NetworkConfig{Enabled: true, AllowInternet: true}
	serverState, err := mgr.Start(ctx, client.CreateInstanceRequest{
		ID:       "nginx",
		Image:    "alpine-nginx",
		Network:  network,
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("Start(nginx) error = %v", err)
	}
	if serverState.NetworkIPv4 == "" {
		t.Fatalf("Start(nginx) state missing network IPv4: %#v", serverState)
	}

	clientState, err := mgr.Start(ctx, client.CreateInstanceRequest{
		ID:       "curl",
		Image:    "alpine-curl",
		Network:  network,
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("Start(curl) error = %v", err)
	}
	if clientState.NetworkIPv4 == "" || clientState.NetworkIPv4 == serverState.NetworkIPv4 {
		t.Fatalf("Start(curl) network IPv4 = %q, server IPv4 = %q", clientState.NetworkIPv4, serverState.NetworkIPv4)
	}

	resp, err := mgr.RunIn(ctx, "nginx", client.RunRequest{
		Command: []string{"sh", "-c", "set -e; if ! command -v nginx >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1; then apk add --no-cache nginx curl >/tmp/cc-apk-nginx.log; fi; mkdir -p /run/nginx /tmp/cc-nginx; printf 'vm-nginx-ok\\n' >/tmp/cc-nginx/index.html; printf 'server { listen 80 default_server; root /tmp/cc-nginx; location / { try_files $uri /index.html; } }\\n' >/etc/nginx/http.d/default.conf; nginx -g 'daemon off;' >/tmp/cc-nginx.log 2>&1 & for i in $(seq 1 100); do if curl -fsS http://127.0.0.1/ | grep -q vm-nginx-ok; then echo nginx-ready; exit 0; fi; sleep 0.1; done; cat /tmp/cc-nginx.log; exit 1"},
		User:    "0:0",
	})
	if err != nil {
		t.Fatalf("RunIn(nginx) error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 || !strings.Contains(resp.Output, "nginx-ready") {
		t.Fatalf("RunIn(nginx) exit=%d output:\n%s", resp.ExitCode, resp.Output)
	}

	resp, err = mgr.RunIn(ctx, "curl", client.RunRequest{
		Command: []string{"sh", "-c", fmt.Sprintf("set -e; if ! command -v curl >/dev/null 2>&1; then apk add --no-cache curl >/tmp/cc-apk-curl.log; fi; curl -fsS --connect-timeout 2 http://%s/", serverState.NetworkIPv4)},
		User:    "0:0",
	})
	if err != nil {
		t.Fatalf("RunIn(curl) error = %v\noutput:\n%s", err, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "vm-nginx-ok" {
		t.Fatalf("curl output = %q, want vm-nginx-ok", resp.Output)
	}
}

func reserveLocalTCPPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local tcp port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func fetchWithRetry(t testing.TB, url string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := http.Client{Timeout: time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return string(body)
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("status %s", resp.Status)
			}
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("fetch %s failed: %v", url, lastErr)
	return ""
}

func TestRuntimeBackendRunCommandDefaultsToHostUserAndResolvableHostname(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Command:  []string{"sh", "-c", "uid=$(id -u); gid=$(id -g); passwd=$(awk -F: -v uid=\"$uid\" '$3==uid { found=1 } END { print found+0 }' /etc/passwd); group=$(awk -F: -v gid=\"$gid\" '$3==gid { found=1 } END { print found+0 }' /etc/group); printf 'uid=%s gid=%s passwd=%s group=%s hostname=%s hosts=%s\\n' \"$uid\" \"$gid\" \"$passwd\" \"$group\" \"$(cat /etc/hostname)\" \"$(grep ccx3 /etc/hosts | wc -l)\""},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("backend.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	want := fmt.Sprintf("uid=%d gid=%d passwd=1 group=1 hostname=ccx3 hosts=2", os.Getuid(), os.Getgid())
	if strings.TrimSpace(resp.Output) != want {
		t.Fatalf("backend.Run().Output = %q, want %q", resp.Output, want)
	}
}

func TestRuntimeBackendStartThenExec(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image:    "alpine",
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "echo linux-amd64-start-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-start-ok" {
		t.Fatalf("mgr.Run().Output = %q, want linux-amd64-start-ok", resp.Output)
	}
}

func TestRuntimeBackendStartWithWritableShare(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	shareDir := filepath.Join(root, "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(share) error = %v", err)
	}

	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Shares: []client.ShareMount{{
			Source:   shareDir,
			Mount:    "/work",
			Writable: true,
		}},
		MemoryMB: 256,
	})
	if err != nil {
		t.Fatalf("mgr.Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.Start().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"/bin/sh", "-lc", "echo hello-amd64-share > /work/hello.txt && cat /work/hello.txt"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "hello-amd64-share" {
		t.Fatalf("mgr.Run().Output = %q, want hello-amd64-share", resp.Output)
	}
	buf, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if strings.TrimSpace(string(buf)) != "hello-amd64-share" {
		t.Fatalf("host share contents = %q, want hello-amd64-share", string(buf))
	}
}

func TestRuntimeBackendStartBlankThenRunImage(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.StartBlank(ctx, client.StartInstanceRequest{MemoryMB: 256})
	if err != nil {
		t.Fatalf("mgr.StartBlank() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.StartBlank().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-c", "echo linux-amd64-blank-image-ok"},
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if strings.TrimSpace(resp.Output) != "linux-amd64-blank-image-ok" {
		t.Fatalf("mgr.Run().Output = %q, want linux-amd64-blank-image-ok", resp.Output)
	}
}

func TestRuntimeBackendStartBlankThenRunImageWithShareWorkdir(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	shareDir := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	mgr := NewManagerWithBackend(NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit")))
	state, err := mgr.StartBlank(ctx, client.StartInstanceRequest{MemoryMB: 256})
	if err != nil {
		t.Fatalf("mgr.StartBlank() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("mgr.StartBlank().Status = %q, want running", state.Status)
	}
	defer mgr.Shutdown(context.Background())

	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   "alpine",
		Command: []string{"sh", "-lc", "pwd && echo shell-share-ok > hello.txt && cat hello.txt"},
		Shares: []client.ShareMount{
			{Source: shareDir, Mount: "/work", Writable: true},
		},
		WorkDir: "/work",
	})
	if err != nil {
		t.Fatalf("mgr.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("mgr.Run().ExitCode = %d, want 0\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	lines := strings.Split(strings.TrimSpace(resp.Output), "\n")
	if len(lines) < 2 {
		t.Fatalf("mgr.Run().Output = %q, want pwd and command output", resp.Output)
	}
	if strings.TrimSpace(lines[0]) != "/work" {
		t.Fatalf("pwd = %q, want /work\noutput:\n%s", lines[0], resp.Output)
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "shell-share-ok" {
		t.Fatalf("final output = %q, want shell-share-ok\noutput:\n%s", lines[len(lines)-1], resp.Output)
	}
	buf, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if strings.TrimSpace(string(buf)) != "shell-share-ok" {
		t.Fatalf("host share contents = %q, want shell-share-ok", string(buf))
	}
}

func TestRuntimeBackendRunNiimathFromLocalSIMGPath(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	fixture := filepath.Join("..", "..", "local", "niimath_1.0.20250804_20251016.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local niimath fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(ctx, "niimath", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "niimath",
		Command:  []string{"niimath", "-help"},
		MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("backend.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(strings.ToLower(resp.Output), "usage: niimath") {
		t.Fatalf("backend output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}

func TestRuntimeBackendRunNiimathFromCVMFSPath(t *testing.T) {
	if os.Getenv("CCX3_KVM_BOOT") == "" {
		t.Skip("set CCX3_KVM_BOOT=1 to run the linux amd64 KVM boot probe")
	}
	if os.Getenv("CCX3_CVMFS_LIVE") == "" {
		t.Skip("set CCX3_CVMFS_LIVE=1 to run the live Neurodesk CVMFS test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	source := "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016"
	if _, err := store.Pull(ctx, "niimath-cvmfs", source); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	backend := NewRuntimeBackend(kernel, store, filepath.Join(store.Root(), "_guestinit"))
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "niimath-cvmfs",
		Command:  []string{"niimath", "-help"},
		MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("backend.Run() error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode == 126 || resp.ExitCode == 127 {
		t.Fatalf("backend.Run().ExitCode = %d, want niimath to be found in PATH\noutput:\n%s", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(strings.ToLower(resp.Output), "usage: niimath") {
		t.Fatalf("backend output did not contain niimath help\noutput:\n%s", resp.Output)
	}
}
