//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os"
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

	var pipeFDs [2]int
	if err := syscall.Pipe(pipeFDs[:]); err != nil {
		return fmt.Errorf("create pipe: %w", err)
	}
	attr := &syscall.ProcAttr{
		Dir:   cfg.WorkDir,
		Env:   cfg.Env,
		Files: []uintptr{uintptr(consoleFD), uintptr(pipeFDs[1]), uintptr(pipeFDs[1])},
	}
	pid, err := syscall.ForkExec(cfg.Command[0], cfg.Command, attr)
	if err != nil {
		return fmt.Errorf("exec %s: %w", cfg.Command[0], err)
	}
	_ = syscall.Close(pipeFDs[1])

	output := readAll(pipeFDs[0])
	_ = syscall.Close(pipeFDs[0])

	var status syscall.WaitStatus
	_, err = syscall.Wait4(pid, &status, 0, nil)
	if err != nil {
		return fmt.Errorf("wait for child: %w", err)
	}

	exitCode := 0
	if status.Exited() {
		exitCode = status.ExitStatus()
	} else if status.Signaled() {
		exitCode = 128 + int(status.Signal())
	}

	if len(output) > 0 {
		for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
			writeKernel(line)
		}
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

func mountRootFS(tag string) error {
	for _, dir := range []string{"/mnt", "/mnt/proc", "/mnt/sys", "/mnt/dev", "/mnt/tmp"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := syscall.Mount(tag, "/mnt", "virtiofs", 0, ""); err != nil {
		return fmt.Errorf("mount virtiofs %s: %w", tag, err)
	}
	_ = syscall.Mount("proc", "/mnt/proc", "proc", 0, "")
	_ = syscall.Mount("sysfs", "/mnt/sys", "sysfs", 0, "")
	_ = syscall.Mount("devtmpfs", "/mnt/dev", "devtmpfs", 0, "")
	_ = syscall.Mount("tmpfs", "/mnt/tmp", "tmpfs", 0, "mode=1777")
	if err := os.Chdir("/mnt"); err != nil {
		return fmt.Errorf("chdir /mnt: %w", err)
	}
	_ = os.MkdirAll("oldroot", 0o755)
	if err := syscall.PivotRoot(".", "oldroot"); err != nil {
		if err := syscall.Chroot("."); err != nil {
			return fmt.Errorf("chroot /mnt: %w", err)
		}
	} else {
		if err := os.Chdir("/"); err != nil {
			return fmt.Errorf("chdir / after pivot_root: %w", err)
		}
		_ = syscall.Unmount("/oldroot", syscall.MNT_DETACH)
		_ = os.Remove("/oldroot")
		return nil
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / after chroot: %w", err)
	}
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

func readAll(fd int) string {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil || n == 0 {
			break
		}
	}
	return b.String()
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
