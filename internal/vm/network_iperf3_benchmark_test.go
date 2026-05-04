//go:build linux && amd64

package vm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func TestRuntimeBackendIperf3Benchmark(t *testing.T) {
	if os.Getenv("CCX3_IPERF3_BENCH") == "" {
		t.Skip("set CCX3_IPERF3_BENCH=1 to run the iperf3 network benchmark")
	}
	if _, err := exec.LookPath("iperf3"); err != nil {
		t.Skipf("host iperf3 unavailable: %v", err)
	}
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("local alpine fixture unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	backend, store := newIperfBenchmarkBackend(t, ctx)
	if _, err := store.Pull(ctx, "alpine", fixture); err != nil {
		t.Fatalf("store.Pull() error = %v", err)
	}

	hostServerPort := reserveLocalTCPPort(t)
	hostServerOutput, stopHostServer := startHostIperfServer(t, ctx, hostServerPort)
	guestToHost := runGuestIperf(t, ctx, backend, hostServerPort, false)
	stopHostServer()
	t.Logf("guest -> host via service.internal:%d\n%s", hostServerPort, guestToHost)
	t.Logf("guest -> host host-server output\n%s", hostServerOutput.String())
	if os.Getenv("CCX3_IPERF3_BENCH_MODE") == "guest-to-host" {
		return
	}

	reversePort := reserveLocalTCPPort(t)
	reverseServerOutput, stopReverseServer := startHostIperfServer(t, ctx, reversePort)
	hostToGuestReverse := runGuestIperf(t, ctx, backend, reversePort, true)
	stopReverseServer()
	t.Logf("host -> guest via iperf3 -R over service.internal:%d\n%s", reversePort, hostToGuestReverse)
	t.Logf("reverse host-server output\n%s", reverseServerOutput.String())

	forwardPort := reserveLocalTCPPort(t)
	inst, err := backend.Start(ctx, client.CreateInstanceRequest{
		Image: "alpine",
		Network: &client.NetworkConfig{
			Enabled:       true,
			AllowInternet: true,
			PortForwards: []client.PortForward{
				{Protocol: "tcp", HostAddr: "127.0.0.1", HostPort: forwardPort, GuestPort: 5201},
			},
		},
		MemoryMB: 512,
	})
	if err != nil {
		t.Fatalf("backend.Start() error = %v", err)
	}
	defer inst.Close()
	execInGuest(t, ctx, inst, installIperf3Command())
	execInGuest(t, ctx, inst, "iperf3 -s -1 -p 5201 >/tmp/cc-iperf3-server.log 2>&1 & echo guest-iperf-ready")
	hostToGuestForward := runHostIperfClient(t, ctx, fmt.Sprintf("127.0.0.1:%d", forwardPort), false)
	serverLog := execInGuest(t, ctx, inst, "cat /tmp/cc-iperf3-server.log || true")
	t.Logf("host -> guest via port forward 127.0.0.1:%d -> 10.42.0.2:5201\n%s", forwardPort, hostToGuestForward)
	t.Logf("guest port-forward server output\n%s", serverLog)
}

func newIperfBenchmarkBackend(t testing.TB, ctx context.Context) (Backend, *oci.Store) {
	t.Helper()
	root := t.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(ctx); err != nil {
		t.Fatalf("kernel.Ensure() error = %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	return NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit")), store
}

func startHostIperfServer(t testing.TB, ctx context.Context, port int) (*bytes.Buffer, func()) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "iperf3", "-s", "-p", fmt.Sprintf("%d", port))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start host iperf3 server: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	return &output, func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}
}

func runGuestIperf(t testing.TB, ctx context.Context, backend Backend, port int, reverse bool) string {
	t.Helper()
	args := ""
	if reverse {
		args = " -R"
	}
	resp, err := backend.Run(ctx, client.RunRequest{
		Image:    "alpine",
		Network:  &client.NetworkConfig{Enabled: true, AllowInternet: true},
		Command:  []string{"sh", "-c", fmt.Sprintf("%s && iperf3 -c service.internal -p %d -t 5%s", installIperf3Command(), port, args)},
		MemoryMB: 512,
		User:     "0:0",
	})
	if err != nil {
		t.Fatalf("guest iperf3 run error = %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("guest iperf3 exit=%d\n%s", resp.ExitCode, resp.Output)
	}
	return resp.Output
}

func installIperf3Command() string {
	return "apk add --no-cache iperf3 >/tmp/cc-apk-iperf3.log 2>&1 || { cat /tmp/cc-apk-iperf3.log; exit 1; }"
}

func execInGuest(t testing.TB, ctx context.Context, inst Instance, command string) string {
	t.Helper()
	resp, err := inst.Exec(ctx, client.ExecRequest{
		Command: []string{"sh", "-c", command},
		User:    "0:0",
	})
	if err != nil {
		t.Fatalf("guest exec %q error = %v\noutput:\n%s", command, err, resp.Output)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("guest exec %q exit=%d\n%s", command, resp.ExitCode, resp.Output)
	}
	return resp.Output
}

func runHostIperfClient(t testing.TB, ctx context.Context, address string, reverse bool) string {
	t.Helper()
	host, port, ok := strings.Cut(address, ":")
	if !ok {
		t.Fatalf("bad host iperf address %q", address)
	}
	args := []string{"-c", host, "-p", port, "-t", "5"}
	if reverse {
		args = append(args, "-R")
	}
	cmd := exec.CommandContext(ctx, "iperf3", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("host iperf3 client error = %v\n%s", err, string(out))
	}
	return string(out)
}
