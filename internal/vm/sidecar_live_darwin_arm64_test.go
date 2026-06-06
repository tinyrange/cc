//go:build darwin && arm64

package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestDarwinSidecarLiveL2AndSave(t *testing.T) {
	if os.Getenv("CCX3_DARWIN_SIDECAR_LIVE") == "" {
		t.Skip("set CCX3_DARWIN_SIDECAR_LIVE=1 to run the macOS sidecar live test")
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	root, err := os.MkdirTemp("/tmp", "ccx3-sidecar-live.")
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})
	ccvmPath := filepath.Join(root, "ccvm")
	build := exec.CommandContext(ctx, "go", "build", "-tags", "embed_guestinit", "-o", ccvmPath, "./cmd/ccvm")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ccvm: %v\n%s", err, out)
	}
	signAdHocForHVF(t, ctx, repoRoot, ccvmPath)

	api, stop := startTestCCVMD(t, ctx, ccvmPath, filepath.Join(root, "cache"))
	defer stop()

	if err := api.PullImage("alpine", client.PullImageRequest{Source: fixture}); err != nil {
		t.Fatalf("PullImage(alpine) error = %v", err)
	}
	t.Log("pulled alpine fixture")

	network := &client.NetworkConfig{Enabled: true, AllowInternet: true}
	t.Log("starting sidecar VM one")
	one, err := api.CreateInstanceStreamWithID("one", client.CreateInstanceRequest{
		Image:          "alpine",
		Network:        network,
		TimeoutSeconds: 30,
	}, nil)
	if err != nil {
		t.Fatalf("CreateInstance(one) error = %v", err)
	}
	t.Logf("started one at %s", one.NetworkIPv4)
	t.Log("writing save marker in VM one")
	runExecInVM(t, api, "one", "sh", "-c", "printf sidecar-save-ok >/etc/vmsh-sidecar-marker")
	t.Log("starting sidecar VM two")
	two, err := api.CreateInstanceStreamWithID("two", client.CreateInstanceRequest{
		Image:          "alpine",
		Network:        network,
		TimeoutSeconds: 30,
	}, nil)
	if err != nil {
		t.Fatalf("CreateInstance(two) error = %v", err)
	}
	if one.NetworkIPv4 == "" || two.NetworkIPv4 == "" || one.NetworkIPv4 == two.NetworkIPv4 {
		t.Fatalf("network addresses one=%q two=%q", one.NetworkIPv4, two.NetworkIPv4)
	}
	t.Logf("started two at %s", two.NetworkIPv4)

	t.Log("pinging VM one from VM two over coordinator L2")
	ping := fmt.Sprintf("for i in $(seq 1 30); do ping -c 1 -W 1 %s && exit 0; sleep 0.1; done; exit 1", one.NetworkIPv4)
	if out := runExecInVM(t, api, "two", "sh", "-c", ping); !strings.Contains(out, "1 packets received") && !strings.Contains(out, "1 received") {
		t.Fatalf("L2 ping output = %q, want successful ping", out)
	}

	t.Log("saving VM one")
	if _, err := api.SaveInstanceImage("one", client.SaveImageRequest{Name: "alpine-saved"}); err != nil {
		t.Fatalf("SaveInstanceImage(one) error = %v", err)
	}
	t.Log("stopping VM two")
	if err := api.ShutdownInstanceWithID("two"); err != nil {
		t.Fatalf("Shutdown(two) error = %v", err)
	}
	t.Log("starting saved VM")
	restored, err := api.CreateInstanceStreamWithID("restored", client.CreateInstanceRequest{
		Image:          "alpine-saved",
		TimeoutSeconds: 30,
	}, nil)
	if err != nil {
		t.Fatalf("CreateInstance(restored) error = %v", err)
	}
	if restored.Status != "running" {
		t.Fatalf("restored status = %q, want running", restored.Status)
	}
	t.Log("checking restored marker")
	if out := strings.TrimSpace(runExecInVM(t, api, "restored", "cat", "/etc/vmsh-sidecar-marker")); out != "sidecar-save-ok" {
		t.Fatalf("restored marker = %q, want sidecar-save-ok", out)
	}
}

func signAdHocForHVF(t *testing.T, ctx context.Context, repoRoot, binary string) {
	t.Helper()
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		t.Skipf("codesign unavailable: %v", err)
	}
	cmd := exec.CommandContext(ctx, codesign, "--force", "--sign", "-", "--entitlements", filepath.Join(repoRoot, "tools", "entitlements.xml"), binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ad-hoc codesign failed: %v\n%s", err, out)
	}
}

func startTestCCVMD(t *testing.T, ctx context.Context, ccvmPath, cacheDir string) (*client.Client, func()) {
	t.Helper()
	cmd := exec.CommandContext(ctx, ccvmPath, "-cache-dir", cacheDir)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	cmd.Env = append(os.Environ(), "CCX3_SIDECAR_MAX_VMS=4", "CCX3_VM_BOOT_TIMEOUT=30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ccvm: %v", err)
	}
	var hello client.ServerHello
	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("read ccvm hello: %v", err)
	}
	if hello.Kind == "error" || hello.Error != "" || strings.TrimSpace(hello.Addr) == "" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("ccvm hello = %#v", hello)
	}
	api := client.NewClient("http://"+hello.Addr, func() (net.Conn, error) {
		return net.Dial("tcp", hello.Addr)
	})
	if err := api.HealthCheck(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("HealthCheck() error = %v", err)
	}
	stop := func() {
		_ = api.Shutdown()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	return api, stop
}

func runExecInVM(t *testing.T, api *client.Client, id string, command ...string) string {
	t.Helper()
	type result struct {
		resp client.ExecResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := api.RunIn(id, client.RunRequest{
			Command:        command,
			TimeoutSeconds: 15,
		})
		done <- result{resp: resp, err: err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("RunIn(%s, %v) error = %v\noutput:\n%s", id, command, got.err, got.resp.Output)
		}
		if got.resp.ExitCode != 0 {
			t.Fatalf("RunIn(%s, %v) exit = %d\noutput:\n%s", id, command, got.resp.ExitCode, got.resp.Output)
		}
		return got.resp.Output
	case <-time.After(25 * time.Second):
		status, err := api.InstanceStatusOf(id)
		if err != nil {
			t.Fatalf("RunIn(%s, %v) timed out; status error: %v", id, command, err)
		}
		t.Fatalf("RunIn(%s, %v) timed out; status: %#v", id, command, status)
	}
	return ""
}
