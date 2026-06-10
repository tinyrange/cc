//go:build darwin && arm64

package hvf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
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

func execInGuest(t *testing.T, session *hvf.ContainerSession, command []string) client.ExecResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hvfExecTimeout())
	defer cancel()
	resp, err := session.Exec(ctx, client.ExecRequest{Command: command})
	if err != nil {
		history, _ := session.ConsoleHistory(context.Background())
		t.Fatalf("run guest command %q: %v\nconsole:\n%s", command, err, history)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("guest command %q exited with %d\noutput:\n%s", command, resp.ExitCode, resp.Output)
	}
	return resp
}

func hvfLinuxRunRequest(t *testing.T) hvf.ContainerRunRequest {
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
