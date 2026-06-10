//go:build darwin && arm64

package hvf_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const hvfCodesignedEnv = "CCX3_HVF_TEST_CODESIGNED"

func TestMain(m *testing.M) {
	if os.Getenv(hvfCodesignedEnv) == "1" {
		os.Exit(m.Run())
	}
	code, err := runCodesignedTestBinary()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func TestHVFBootsLinuxAndRunsOneShotCommand(t *testing.T) {
	req := hvfLinuxRunRequest(t)
	req.Command = []string{
		"sh",
		"-lc",
		"set -eu; printf 'hvf-one-shot\\n'; cat /proc/sys/kernel/ostype; test -r /proc/1/cmdline; cat /etc/alpine-release; printf 'machine=%s\\n' \"$(uname -m)\"",
	}

	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	result, err := hvf.RunContainer(ctx, req)
	if err != nil {
		t.Fatalf("boot Linux guest and run one-shot command: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("guest command exited with %d\noutput:\n%s\ntranscript:\n%s", result.ExitCode, result.Output, result.Transcript)
	}
	requireGuestOutput(t, result.Output, "hvf-one-shot", "Linux", "machine=")
}

func TestHVFBootsPersistentLinuxAndExecsCommands(t *testing.T) {
	req := hvfLinuxRunRequest(t)
	req.Persistent = true

	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	session, err := hvf.StartContainer(ctx, req)
	if err != nil {
		t.Fatalf("boot persistent Linux guest: %v", err)
	}
	defer session.Close()

	first := execInGuest(t, session, []string{
		"sh",
		"-lc",
		"set -eu; printf 'hvf-persistent\\n'; cat /proc/sys/kernel/ostype; test -d /sys/kernel; cat /etc/alpine-release",
	})
	requireGuestOutput(t, first.Output, "hvf-persistent", "Linux")

	second := execInGuest(t, session, []string{
		"sh",
		"-lc",
		"set -eu; printf '%s\\n' $((21 + 21)); printf persisted >/tmp/hvf-test; cat /tmp/hvf-test",
	})
	requireGuestOutput(t, second.Output, "42", "persisted")
}

func TestHVFPersistentLinuxExercisesRuntimeFeatures(t *testing.T) {
	readOnlyShare := t.TempDir()
	writableShare := t.TempDir()
	lateShare := t.TempDir()
	mustWriteFile(t, filepath.Join(readOnlyShare, "host.txt"), "read-only-share\n")
	mustWriteFile(t, filepath.Join(lateShare, "late.txt"), "late-share\n")

	req := hvfLinuxRunRequest(t)
	req.Persistent = true
	req.Env = []string{"HVF_BASE=base", "HVF_OVERRIDE=base"}
	req.WorkDir = "/tmp"
	req.Shares = []hvf.DirectoryShare{
		{Source: readOnlyShare, Mount: "/mnt/ro", Writable: false},
		{Source: writableShare, Mount: "/mnt/rw", Writable: true, Cache: "aggressive"},
	}

	bootEvents := &bootEventRecorder{}
	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	session, err := hvf.StartContainerStream(ctx, req, bootEvents.Record)
	if err != nil {
		t.Fatalf("boot persistent Linux guest with runtime features: %v", err)
	}
	defer session.Close()
	bootEvents.RequireKind(t, "status")

	shareEnv := execInGuestRequest(t, session, client.ExecRequest{
		Command: []string{
			"sh",
			"-lc",
			"set -eu; printf 'hvf-runtime-features\\n'; pwd; printf 'env=%s/%s/%s\\n' \"$HVF_BASE\" \"$HVF_OVERRIDE\" \"$HVF_EXTRA\"; cat /mnt/ro/host.txt; if printf denied >/mnt/ro/denied 2>/tmp/ro.err; then exit 41; fi; printf guest-write >/mnt/rw/guest.txt; cat /mnt/rw/guest.txt",
		},
		Env: []string{"HVF_OVERRIDE=exec", "HVF_EXTRA=extra"},
	})
	requireGuestOutput(t, shareEnv.Output, "hvf-runtime-features", "/tmp", "env=base/exec/extra", "read-only-share", "guest-write")
	if got := mustReadHostFile(t, filepath.Join(writableShare, "guest.txt")); got != "guest-write" {
		t.Fatalf("writable share did not receive guest write: %q", got)
	}

	addShareWithTimeout(t, session, client.ShareMount{Source: lateShare, Mount: "/mnt/late", Writable: true})
	late := execInGuest(t, session, []string{
		"sh",
		"-lc",
		"set -eu; cat /mnt/late/late.txt; printf late-write >/mnt/late/from-guest.txt",
	})
	requireGuestOutput(t, late.Output, "late-share")
	if got := mustReadHostFile(t, filepath.Join(lateShare, "from-guest.txt")); got != "late-write" {
		t.Fatalf("runtime share did not receive guest write: %q", got)
	}

	user := execInGuestRequest(t, session, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; printf 'uid=%s gid=%s\\n' \"$(id -u)\" \"$(id -g)\"; printf user-write >/tmp/user-owned; test -s /tmp/user-owned"},
		User:    "1234:1235",
	})
	requireGuestOutput(t, user.Output, "uid=1234 gid=1235")

	addImageWithTimeout(t, session, "/.ccx3/images/alternate", req.Image)
	imageRoot := execInGuestRequest(t, session, client.ExecRequest{
		Command:     []string{"/bin/sh", "-lc", "set -eu; test -r /etc/alpine-release; printf 'image-rootdir\\n'; pwd"},
		RootDir:     "/.ccx3/images/alternate",
		SkipResolve: true,
		WorkDir:     "/",
	})
	requireGuestOutput(t, imageRoot.Output, "image-rootdir", "/")

	streamOutput := execStreamInGuest(t, session, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; while IFS= read -r line; do printf 'line:%s\\n' \"$line\"; done"},
	}, execStreamInput("alpha\n", "beta\n"), 0)
	requireGuestOutput(t, streamOutput, "line:alpha", "line:beta")

	ttyOutput := execStreamInGuest(t, session, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; stty size; printf 'tty-ok\\n'"},
		TTY:     true,
		Cols:    77,
		Rows:    33,
	}, nil, 0)
	requireGuestOutput(t, ttyOutput, "33 77", "tty-ok")

	signalOutput := execStreamSignalOnOutput(t, session)
	requireGuestOutput(t, signalOutput, "signal-ready", "got-term")

	runGuestControl(t, session, client.ExecRequest{Kind: "fs_mkdir", Path: "/hvf-control"}, 0)
	runGuestControl(t, session, client.ExecRequest{Kind: "fs_write", Path: "/hvf-control/file.txt", Stdin: []byte("control-write")}, 0)
	archive := runGuestControl(t, session, client.ExecRequest{Kind: "fs_archive", Path: "/hvf-control"}, 0)
	requireTarFile(t, archive, "hvf-control/file.txt", "control-write")
	runGuestControl(t, session, client.ExecRequest{
		Kind:      "fs_extract",
		Path:      "/hvf-control",
		Directory: true,
		Stdin:     tarPayload(t, map[string]string{"extract.txt": "extracted"}),
	}, 0)
	extracted := execInGuest(t, session, []string{"sh", "-lc", "set -eu; cat /hvf-control/extract.txt"})
	requireGuestOutput(t, extracted.Output, "extracted")

	writtenRoot := execInGuest(t, session, []string{"sh", "-lc", "set -eu; printf snapshot-root >/hvf-snapshot.txt; sync"})
	if writtenRoot.ExitCode != 0 {
		t.Fatalf("write root snapshot fixture exited with %d", writtenRoot.ExitCode)
	}
	flushGuest(t, session)
	rootSnapshot, err := session.RootSnapshot()
	if err != nil {
		t.Fatalf("snapshot guest root filesystem: %v", err)
	}
	if got := readImageFile(t, rootSnapshot, "/hvf-snapshot.txt"); got != "snapshot-root" {
		t.Fatalf("root snapshot mismatch: %q", got)
	}
	shareSnapshot, err := session.RootSnapshotAt("/mnt/ro")
	if err != nil {
		t.Fatalf("snapshot read-only share: %v", err)
	}
	if got := readImageFile(t, shareSnapshot, "/host.txt"); got != "read-only-share\n" {
		t.Fatalf("share snapshot mismatch: %q", got)
	}

	stats := session.VirtioFSStats()
	if len(stats) == 0 {
		t.Fatalf("expected virtio-fs stats after filesystem activity")
	}
	var fuseRequests uint64
	for _, stat := range stats {
		fuseRequests += stat.FUSERequests
	}
	if fuseRequests == 0 {
		t.Fatalf("expected virtio-fs FUSE requests after filesystem activity: %+v", stats)
	}
}

func TestHVFBootsLinuxWithVirtioDevicesNetworkAndSMP(t *testing.T) {
	req := hvfLinuxRunRequestWithModules(t, []string{"CONFIG_VIRTIO_NET"}, map[string]string{
		"CONFIG_VIRTIO_NET": "kernel/drivers/net/virtio_net.ko.gz",
	})
	req.CPUs = 2
	req.Persistent = true
	req.Network = &vmruntime.GuestNetworkConfig{
		Interface: "eth0",
		Address:   "10.42.0.2/24",
		Gateway:   "10.42.0.1",
		DNS:       "10.42.0.1",
	}
	req.NetDevice = virtio.NewNet(
		arm64vm.NetBase,
		arm64vm.NetSize,
		arm64vm.NetIRQ,
		net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02},
		acceptingNetBackend{},
	)
	command := []string{
		"sh",
		"-lc",
		"set -eu; printf 'hvf-devices\\n'; cpus=$(grep -c '^processor' /proc/cpuinfo); test \"$cpus\" -ge 2; printf 'cpus=%s\\n' \"$cpus\"; dd if=/dev/hwrng of=/tmp/hvf-rng bs=16 count=1 2>/dev/null; test \"$(wc -c </tmp/hvf-rng)\" = 16; printf 'rng=ok\\n'; test -d /sys/class/net/eth0; test \"$(cat /sys/class/net/eth0/address)\" = '02:42:0a:2a:00:02'; grep -q '^nameserver 10.42.0.1$' /etc/resolv.conf; printf 'net=%s\\n' \"$(cat /sys/class/net/eth0/address)\"",
	}

	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	session, err := hvf.StartContainer(ctx, req)
	if err != nil {
		t.Fatalf("boot Linux guest with virtio devices, network, and SMP: %v", err)
	}
	defer session.Close()

	result := execInGuest(t, session, command)
	requireGuestOutput(t, result.Output, "hvf-devices", "cpus=", "rng=ok", "net=02:42:0a:2a:00:02")
}

func TestHVFPersistentLinuxConsoleHistoryWithDmesg(t *testing.T) {
	req := hvfLinuxRunRequest(t)
	req.Persistent = true
	req.Dmesg = true

	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	session, err := hvf.StartContainer(ctx, req)
	if err != nil {
		t.Fatalf("boot persistent Linux guest with dmesg enabled: %v", err)
	}
	defer session.Close()

	execInGuest(t, session, []string{"sh", "-lc", "true"})
	history, err := session.ConsoleHistory(context.Background())
	if err != nil {
		t.Fatalf("read console history: %v", err)
	}
	requireGuestOutput(t, history, "ccx3-init")
}

func TestHVFBootsLinuxWithNestedVirtualizationWhenSupported(t *testing.T) {
	supported, err := hvf.NestedVirtualizationSupported()
	if err != nil {
		t.Fatalf("check nested virtualization support: %v", err)
	}
	if !supported {
		t.Skip("nested virtualization is not supported on this Mac")
	}

	req := hvfLinuxRunRequest(t)
	req.NestedVirt = true
	req.Persistent = true
	command := []string{
		"sh",
		"-lc",
		"set -eu; printf 'hvf-nested\\n'; cat /proc/sys/kernel/ostype; test -r /proc/cpuinfo",
	}

	ctx, cancel := context.WithTimeout(context.Background(), hvfBootTimeout())
	defer cancel()

	session, err := hvf.StartContainer(ctx, req)
	if err != nil {
		t.Fatalf("boot Linux guest with nested virtualization enabled: %v", err)
	}
	defer session.Close()

	result := execInGuest(t, session, command)
	requireGuestOutput(t, result.Output, "hvf-nested", "Linux")
}

func runCodesignedTestBinary() (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("locate test binary: %w", err)
	}
	tmpDir, err := os.MkdirTemp("", "cc-hvf-test-*")
	if err != nil {
		return 1, fmt.Errorf("create codesigned test temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	signedExe := filepath.Join(tmpDir, filepath.Base(exe))
	if err := copyFile(exe, signedExe); err != nil {
		return 1, fmt.Errorf("copy test binary for codesigning: %w", err)
	}
	if err := os.Chmod(signedExe, 0o755); err != nil {
		return 1, fmt.Errorf("chmod copied test binary: %w", err)
	}

	entitlements := filepath.Join(repoRootFromSource(), "tools", "entitlements.xml")
	cmd := exec.Command("codesign", "-f", "-s", "-", "--entitlements", entitlements, signedExe)
	if output, err := cmd.CombinedOutput(); err != nil {
		return 1, fmt.Errorf("codesign HVF test binary: %w\n%s", err, output)
	}

	child := exec.Command(signedExe, os.Args[1:]...)
	child.Env = append(os.Environ(), hvfCodesignedEnv+"=1")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Stdin = os.Stdin
	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("run codesigned HVF test binary: %w", err)
	}
	return 0, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

type acceptingNetBackend struct{}

func (acceptingNetBackend) HandleTxPacket([]byte) error {
	return nil
}

type bootEventRecorder struct {
	mu     sync.Mutex
	events []client.BootEvent
}

func (r *bootEventRecorder) Record(event client.BootEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *bootEventRecorder) RequireKind(t *testing.T, kind string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		for _, event := range r.events {
			if event.Kind == kind {
				r.mu.Unlock()
				return
			}
		}
		events := append([]client.BootEvent(nil), r.events...)
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("boot events did not include kind %q: %+v", kind, events)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func execInGuest(t *testing.T, session *hvf.ContainerSession, command []string) client.ExecResponse {
	t.Helper()
	return execInGuestRequest(t, session, client.ExecRequest{Command: command})
}

func execInGuestRequest(t *testing.T, session *hvf.ContainerSession, req client.ExecRequest) client.ExecResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	resp, err := session.Exec(ctx, req)
	if err != nil {
		history, _ := session.ConsoleHistory(context.Background())
		t.Fatalf("run guest command %q: %v\nconsole:\n%s", req.Command, err, history)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("guest command %q exited with %d\noutput:\n%s", req.Command, resp.ExitCode, resp.Output)
	}
	return resp
}

func execStreamInput(chunks ...string) <-chan client.ExecInput {
	inputs := make(chan client.ExecInput, len(chunks))
	for _, chunk := range chunks {
		inputs <- client.ExecInput{Kind: "stdin", Input: chunk}
	}
	close(inputs)
	return inputs
}

func execStreamInGuest(t *testing.T, session *hvf.ContainerSession, req client.ExecRequest, inputs <-chan client.ExecInput, wantExit int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	var output bytes.Buffer
	var exitCode *int
	err := session.ExecStream(ctx, req, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			if len(event.Data) > 0 {
				output.Write(event.Data)
			} else {
				output.WriteString(event.Output)
			}
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		history, _ := session.ConsoleHistory(context.Background())
		t.Fatalf("stream guest exec %q: %v\noutput:\n%s\nconsole:\n%s", req.Command, err, output.String(), history)
	}
	if exitCode == nil {
		t.Fatalf("stream guest exec %q did not report an exit\noutput:\n%s", req.Command, output.String())
	}
	if *exitCode != wantExit {
		t.Fatalf("stream guest exec %q exited with %d, want %d\noutput:\n%s", req.Command, *exitCode, wantExit, output.String())
	}
	return output.String()
}

func execStreamSignalOnOutput(t *testing.T, session *hvf.ContainerSession) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	inputs := make(chan client.ExecInput, 1)
	var output bytes.Buffer
	var exitCode *int
	signaled := false
	err := session.ExecStream(ctx, client.ExecRequest{
		Command: []string{"sh", "-lc", "trap 'echo got-term; exit 7' TERM; echo signal-ready; while :; do sleep 1; done"},
	}, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			if len(event.Data) > 0 {
				output.Write(event.Data)
			} else {
				output.WriteString(event.Output)
			}
			if !signaled && strings.Contains(output.String(), "signal-ready") {
				signaled = true
				inputs <- client.ExecInput{Kind: "signal", Signal: "TERM"}
				close(inputs)
			}
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		history, _ := session.ConsoleHistory(context.Background())
		t.Fatalf("stream guest signal exec: %v\noutput:\n%s\nconsole:\n%s", err, output.String(), history)
	}
	if exitCode == nil {
		t.Fatalf("stream guest signal exec did not report an exit\noutput:\n%s", output.String())
	}
	if *exitCode != 7 {
		t.Fatalf("stream guest signal exec exited with %d, want 7\noutput:\n%s", *exitCode, output.String())
	}
	return output.String()
}

func runGuestControl(t *testing.T, session *hvf.ContainerSession, req client.ExecRequest, wantExit int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	var output bytes.Buffer
	var exitCode *int
	err := session.ExecStream(ctx, req, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			if len(event.Data) > 0 {
				output.Write(event.Data)
			} else {
				output.WriteString(event.Output)
			}
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		history, _ := session.ConsoleHistory(context.Background())
		t.Fatalf("run guest control request %q path %q: %v\noutput:\n%s\nconsole:\n%s", req.Kind, req.Path, err, output.String(), history)
	}
	if exitCode == nil {
		t.Fatalf("guest control request %q path %q did not report an exit\noutput:\n%s", req.Kind, req.Path, output.String())
	}
	if *exitCode != wantExit {
		t.Fatalf("guest control request %q path %q exited with %d, want %d\noutput:\n%s", req.Kind, req.Path, *exitCode, wantExit, output.String())
	}
	return output.Bytes()
}

func addShareWithTimeout(t *testing.T, session *hvf.ContainerSession, share client.ShareMount) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	if err := session.AddShare(ctx, share); err != nil {
		t.Fatalf("add runtime share %q: %v", share.Mount, err)
	}
}

func addImageWithTimeout(t *testing.T, session *hvf.ContainerSession, mount string, image *oci.Image) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	if err := session.AddImage(ctx, mount, image); err != nil {
		t.Fatalf("add runtime image at %q: %v", mount, err)
	}
}

func flushGuest(t *testing.T, session *hvf.ContainerSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	if err := session.Flush(ctx); err != nil {
		t.Fatalf("flush guest filesystem state: %v", err)
	}
}

func hvfLinuxRunRequest(t *testing.T) hvf.ContainerRunRequest {
	return hvfLinuxRunRequestWithModules(t, nil, nil)
}

func hvfLinuxRunRequestWithModules(t *testing.T, extraConfigVars []string, extraModuleMap map[string]string) hvf.ContainerRunRequest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfPrepareTimeout())
	defer cancel()

	root := repoRoot(t)
	cacheRoot := hvfLinuxCacheRoot(t)
	t.Setenv("CCX3_OCI_SHARED_CACHE_DIR", filepath.Join(cacheRoot, "oci-shared"))

	init := buildGuestInit(t, ctx, root)
	kernelManager := alpine.NewManager(filepath.Join(cacheRoot, "kernel"))
	if err := kernelManager.EnsureWithProgress(ctx, nil); err != nil {
		t.Fatalf("prepare Alpine arm64 Linux kernel: %v", err)
	}
	kernel, err := kernelManager.ReadKernel()
	if err != nil {
		t.Fatalf("read Alpine arm64 Linux kernel: %v", err)
	}

	image := importAlpineSIMG(t, ctx, root)
	configVars := []string{
		"CONFIG_VIRTIO_MMIO",
		"CONFIG_FUSE_FS",
		"CONFIG_VIRTIO_FS",
		"CONFIG_VSOCKETS",
		"CONFIG_VIRTIO_VSOCKETS",
		"CONFIG_HW_RANDOM",
		"CONFIG_HW_RANDOM_VIRTIO",
	}
	moduleMap := map[string]string{
		"CONFIG_VIRTIO_MMIO":      "kernel/drivers/virtio/virtio_mmio.ko.gz",
		"CONFIG_FUSE_FS":          "kernel/fs/fuse/fuse.ko.gz",
		"CONFIG_VIRTIO_FS":        "kernel/fs/fuse/virtiofs.ko.gz",
		"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.gz",
		"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
		"CONFIG_HW_RANDOM":        "kernel/drivers/char/hw_random/rng-core.ko.gz",
		"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
	}
	configVars = append(configVars, extraConfigVars...)
	for key, value := range extraModuleMap {
		moduleMap[key] = value
	}

	amd64EmulatorPath := ""
	if strings.TrimSpace(image.Architecture) == "amd64" {
		configVars = append(configVars, "CONFIG_BINFMT_MISC")
		moduleMap["CONFIG_BINFMT_MISC"] = "kernel/fs/binfmt_misc.ko.gz"
		amd64EmulatorPath, err = kernelManager.ExtractPackageFile(ctx, "community", "qemu-x86_64", "usr/bin/qemu-x86_64")
		if err != nil {
			t.Fatalf("prepare guest qemu-x86_64 emulator: %v", err)
		}
	}

	modules, err := kernelManager.PlanModuleLoad(configVars, moduleMap)
	if err != nil {
		t.Fatalf("prepare Linux kernel modules: %v", err)
	}

	return hvf.ContainerRunRequest{
		Kernel:            kernel,
		Init:              init,
		AMD64EmulatorPath: amd64EmulatorPath,
		Modules:           modules,
		Image:             image,
		MemoryMB:          768,
		CPUs:              1,
		UnixTime:          time.Now().Unix(),
	}
}

func buildGuestInit(t *testing.T, ctx context.Context, root string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "init-linux-arm64")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./internal/cmd/init")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build Linux arm64 guest init: %v\n%s", err, combined)
	}
	init, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read Linux arm64 guest init: %v", err)
	}
	if !bytes.HasPrefix(init, []byte("\x7fELF")) {
		t.Fatalf("built guest init is not an ELF binary")
	}
	return init
}

func importAlpineSIMG(t *testing.T, ctx context.Context, root string) *oci.Image {
	t.Helper()
	const imageName = "alpine-hvf-linux-test"
	store := oci.NewStore(filepath.Join(t.TempDir(), "images"))
	fixture := filepath.Join(root, "fixtures", "alpine.simg")
	if _, err := store.Pull(ctx, imageName, fixture, oci.PullOptions{Architecture: "amd64"}); err != nil {
		t.Fatalf("import Alpine SIMG fixture: %v", err)
	}
	image, err := store.Open(imageName)
	if err != nil {
		t.Fatalf("open imported Alpine image: %v", err)
	}
	image = withRuntimeMountDirs(image)
	if image == nil || image.RootFS == nil {
		t.Fatalf("imported Alpine image has no root filesystem")
	}
	switch strings.TrimSpace(image.Architecture) {
	case "amd64", "arm64", "":
	default:
		t.Fatalf("unsupported Alpine fixture architecture %q", image.Architecture)
	}
	return image
}

func withRuntimeMountDirs(image *oci.Image) *oci.Image {
	if image == nil || image.RootFS == nil {
		return image
	}
	overlay := imagefs.NewOverlay(image.RootFS)
	for _, dir := range []string{"/dev", "/proc", "/sys", "/run", "/tmp"} {
		_ = overlay.AddDir(dir, fs.ModeDir|0o755)
	}
	cloned := *image
	cloned.RootFS = overlay.Root()
	return &cloned
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadHostFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readImageFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup snapshot path %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("snapshot path %s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read snapshot path %s: %v", guestPath, err)
	}
	return string(data)
}

func tarPayload(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, contents := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(contents)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := io.WriteString(tw, contents); err != nil {
			t.Fatalf("write tar contents %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar payload: %v", err)
	}
	return buf.Bytes()
}

func requireTarFile(t *testing.T, archive []byte, name, want string) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar archive: %v", err)
		}
		names = append(names, header.Name)
		if header.Name != name {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar file %s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("tar file %s = %q, want %q", name, string(data), want)
		}
		return
	}
	t.Fatalf("tar archive did not contain %s; entries: %s", name, strings.Join(names, ", "))
}

func requireGuestOutput(t *testing.T, output string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("guest output does not contain %q\noutput:\n%s", fragment, output)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return repoRootFromSource()
}

func repoRootFromSource() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func hvfLinuxCacheRoot(t *testing.T) string {
	t.Helper()
	if dir := strings.TrimSpace(os.Getenv("CCX3_HVF_TEST_CACHE_DIR")); dir != "" {
		return dir
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		return filepath.Join(os.TempDir(), "ccx3-hvf-linux-test")
	}
	return filepath.Join(cacheRoot, "ccx3", "hvf-linux-test")
}

func hvfPrepareTimeout() time.Duration {
	return envDuration("CCX3_HVF_TEST_PREPARE_TIMEOUT", 10*time.Minute)
}

func hvfBootTimeout() time.Duration {
	return envDuration("CCX3_HVF_TEST_BOOT_TIMEOUT", 3*time.Minute)
}

func hvfExecTimeout() time.Duration {
	return envDuration("CCX3_HVF_TEST_EXEC_TIMEOUT", time.Minute)
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	return fallback
}
