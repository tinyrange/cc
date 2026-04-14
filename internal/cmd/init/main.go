//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const configPath = "/etc/ccx3-init.json"

var consoleFD = 2
var kmsgFD = -1

type config struct {
	Command          []string `json:"command"`
	Env              []string `json:"env"`
	WorkDir          string   `json:"workdir"`
	Modules          []string `json:"modules"`
	RootFSTag        string   `json:"rootfs_tag"`
	BeginMarker      string   `json:"begin_marker"`
	ExitMarkerPrefix string   `json:"exit_marker_prefix"`
}

func main() {
	if err := run(); err != nil {
		writeConsole("ccx3-init: " + err.Error() + "\n")
		for {
			syscall.Pause()
		}
	}
}

func run() error {
	fd, err := syscall.Open("/dev/console", syscall.O_RDWR, 0)
	if err == nil {
		consoleFD = fd
		for _, target := range []int{0, 1, 2} {
			_ = syscall.Dup3(fd, target, 0)
		}
	}
	if fd, err := syscall.Open("/dev/kmsg", syscall.O_WRONLY, 0); err == nil {
		kmsgFD = fd
	}

	var cfg config
	buf, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(buf, &cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if len(cfg.Command) == 0 {
		return fmt.Errorf("config command is empty")
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/"
	}

	_ = syscall.Mount("proc", "/proc", "proc", 0, "")
	_ = syscall.Mount("sysfs", "/sys", "sysfs", 0, "")

	writeKernel("ccx3-init: loading modules")
	if err := loadModules(cfg.Modules); err != nil {
		return err
	}
	writeKernel("ccx3-init: modules loaded")

	if cfg.RootFSTag != "" {
		writeKernel("ccx3-init: mounting rootfs")
		if err := mountRootFS(cfg.RootFSTag); err != nil {
			return err
		}
		writeKernel("ccx3-init: rootfs mounted")
	}
	if err := os.Chdir(cfg.WorkDir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.WorkDir, err)
	}

	if cfg.BeginMarker != "" {
		writeKernel(cfg.BeginMarker)
	}

	writeKernel("ccx3-init: exec " + strings.Join(cfg.Command, " "))
	if err := execCommand(cfg); err != nil {
		return err
	}
	return fmt.Errorf("exec command returned unexpectedly")
}

func mountRootFS(tag string) error {
	if err := os.MkdirAll("/mnt", 0o755); err != nil {
		return fmt.Errorf("mkdir /mnt: %w", err)
	}
	if err := syscall.Mount(tag, "/mnt", "virtiofs", 0, ""); err != nil {
		return fmt.Errorf("mount virtiofs %s: %w", tag, err)
	}
	if err := syscall.Chroot("/mnt"); err != nil {
		return fmt.Errorf("chroot /mnt: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / after chroot: %w", err)
	}

	for _, dir := range []string{"/proc", "/sys", "/dev", "/tmp", "/dev/pts", "/dev/shm"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	_ = syscall.Mount("proc", "/proc", "proc", 0, "")
	_ = syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	_ = syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "")
	_ = syscall.Mount("tmpfs", "/tmp", "tmpfs", 0, "mode=1777")
	_ = syscall.Mount("devpts", "/dev/pts", "devpts", 0, "")
	_ = syscall.Mount("tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777")
	_ = os.Symlink("/proc/self/fd", "/dev/fd")
	return nil
}

func writeString(fd int, value string) {
	for len(value) > 0 {
		n, err := syscall.Write(fd, []byte(value))
		if err != nil || n <= 0 {
			return
		}
		value = value[n:]
	}
}

func writeConsole(value string) {
	writeString(consoleFD, value)
}

func writeKernel(value string) {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return
	}
	if kmsgFD >= 0 {
		writeString(kmsgFD, "<6>"+value+"\n")
		return
	}
	writeConsole(value + "\n")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [32]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func loadModules(modules []string) error {
	for _, path := range modules {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read module %s: %w", path, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("module %s is empty", path)
		}
		params, err := syscall.BytePtrFromString("")
		if err != nil {
			return fmt.Errorf("init module params: %w", err)
		}
		_, _, errno := syscall.RawSyscall(syscall.SYS_INIT_MODULE, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(params)))
		if errno != 0 {
			return fmt.Errorf("load module %s: errno=%d", path, errno)
		}
	}
	return nil
}

func execCommand(cfg config) error {
	if info, err := os.Stat(cfg.Command[0]); err != nil {
		writeKernel("ccx3-init: stat failed for " + cfg.Command[0] + ": " + err.Error())
	} else {
		writeKernel("ccx3-init: stat mode for " + cfg.Command[0] + " is " + fmt.Sprintf("%#o", info.Mode()&0o777))
	}

	exitCode, err := execCommandGo(cfg.Command, cfg.Env, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("run %s: %w", cfg.Command[0], err)
	}
	if cfg.ExitMarkerPrefix != "" {
		writeKernel(cfg.ExitMarkerPrefix + itoa(exitCode))
	}
	syscall.Sync()
	_ = syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF)
	for {
		syscall.Pause()
	}
}

func execCommandGo(argv []string, env []string, workDir string) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	if workDir != "" {
		cmd.Dir = workDir
	}
	console := os.NewFile(uintptr(consoleFD), "/dev/console")
	cmd.Stdin = console
	cmd.Stdout = console
	cmd.Stderr = console

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}
