//go:build linux

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const configPath = "/etc/ccx3-init.json"
const guestQEMUPath = "/run/ccx3/qemu-x86_64"
const guestQEMUBinfmtPath = "/run/ccx3-qemu-x86_64"
const initDurationMarker = "__CCX3_INIT_MS__:"
const execTimingMarker = "__CCX3_TIMING__:"
const defaultControlMarkerPref = "__CCX3_CTL__:"
const fatalBootMarker = "ccx3-init-fatal: "
const execPivotMode = "--ccx3-exec-pivot"
const stage2Mode = "--ccx3-stage2"
const stage2Path = "/run/ccx3/stage2"
const stage2ConfigPath = "/run/ccx3/config.json"
const initSystemSystemd = "systemd"
const systemdReadyTimeout = 30 * time.Second

var consoleFD = 2
var kmsgFD = -1
var protocolMu sync.Mutex
var setTimeOfDay = unix.Settimeofday

type config struct {
	Command            []string `json:"command"`
	Env                []string `json:"env"`
	WorkDir            string   `json:"workdir"`
	User               string   `json:"user,omitempty"`
	InitSystem         string   `json:"init,omitempty"`
	Hostname           string   `json:"hostname,omitempty"`
	Modules            []string `json:"modules"`
	EmulatorTag        string   `json:"emulator_tag,omitempty"`
	RootFSTag          string   `json:"rootfs_tag"`
	Shares             []share  `json:"shares,omitempty"`
	VsockPort          uint32   `json:"vsock_port,omitempty"`
	ReadyMarker        string   `json:"ready_marker"`
	BeginMarker        string   `json:"begin_marker"`
	OutputMarkerPref   string   `json:"output_marker_prefix"`
	ErrorMarkerPref    string   `json:"error_marker_prefix"`
	ControlMarkerPref  string   `json:"control_marker_prefix"`
	UsageMarkerPref    string   `json:"usage_marker_prefix"`
	ExitMarkerPrefix   string   `json:"exit_marker_prefix"`
	PrecopyAMD64Root   bool     `json:"precopy_amd64_root,omitempty"`
	DisableCgroupMount bool     `json:"disable_cgroup_mount,omitempty"`
	Network            *network `json:"network,omitempty"`
	UnixTime           int64    `json:"unix_time,omitempty"`
}

type network struct {
	Interface string `json:"interface,omitempty"`
	Address   string `json:"address,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	DNS       string `json:"dns,omitempty"`
}

type share struct {
	Tag      string `json:"tag"`
	Mount    string `json:"mount"`
	Writable bool   `json:"writable,omitempty"`
}

type execRequest struct {
	Kind      string   `json:"kind,omitempty"`
	ID        string   `json:"id"`
	Command   []string `json:"command,omitempty"`
	Env       []string `json:"env,omitempty"`
	RootDir   string   `json:"root_dir,omitempty"`
	Path      string   `json:"path,omitempty"`
	Dir       bool     `json:"directory,omitempty"`
	WorkDir   string   `json:"workdir,omitempty"`
	User      string   `json:"user,omitempty"`
	Stdin     []byte   `json:"stdin,omitempty"`
	TTY       bool     `json:"tty,omitempty"`
	ControlFD bool     `json:"control_fd,omitempty"`
	Signal    string   `json:"signal,omitempty"`
	Cols      int      `json:"cols,omitempty"`
	Rows      int      `json:"rows,omitempty"`
}

type managedExec struct {
	stdinMu sync.Mutex
	stdin   io.WriteCloser
	pty     *os.File
	process *os.Process
	group   bool
	start   time.Time
}

func (m *managedExec) writeStdin(data []byte) error {
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	if m.stdin == nil {
		return fmt.Errorf("stdin is closed")
	}
	_, err := m.stdin.Write(data)
	return err
}

func (m *managedExec) closeStdin() error {
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	if m.stdin == nil {
		return nil
	}
	err := m.stdin.Close()
	m.stdin = nil
	return err
}

func (m *managedExec) setProcess(proc *os.Process, group bool) {
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	m.process = proc
	m.group = group
}

func (m *managedExec) setPTY(pty *os.File) {
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	m.pty = pty
}

func (m *managedExec) signal(name string) error {
	sig, err := parseSignal(name)
	if err != nil {
		return err
	}
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	if m.process == nil {
		return fmt.Errorf("process is not started")
	}
	if m.group && m.process.Pid > 0 {
		err := syscall.Kill(-m.process.Pid, sig)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
	}
	return m.process.Signal(sig)
}

func (m *managedExec) resize(cols, rows int) error {
	m.stdinMu.Lock()
	defer m.stdinMu.Unlock()
	if m.pty == nil {
		return fmt.Errorf("exec has no tty")
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid tty size %dx%d", cols, rows)
	}
	return unix.IoctlSetWinsize(int(m.pty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Col: uint16(cols),
		Row: uint16(rows),
	})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == stage2Mode {
		if err := runStage2(); err != nil {
			fmt.Fprintf(os.Stderr, "ccx3-init: stage2: %v\n", err)
			os.Exit(126)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == execPivotMode {
		if err := runExecPivot(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "ccx3-init: exec pivot: %v\n", err)
			os.Exit(126)
		}
		return
	}
	if err := run(); err != nil {
		writeKernel(fatalBootMarker + err.Error())
		writeConsole(fatalBootMarker + err.Error() + "\n")
		for {
			syscall.Pause()
		}
	}
}

func run() error {
	fd, err := syscall.Open("/dev/console", syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = syscall.SetNonblock(fd, false)
		consoleFD = fd
		for _, target := range []int{0, 1, 2} {
			_ = syscall.Dup3(fd, target, 0)
		}
	}
	if fd, err := syscall.Open("/dev/kmsg", syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
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
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/"
	}
	if cfg.Hostname == "" || cfg.Hostname == "(none)" {
		cfg.Hostname = "ccx3"
	}
	if err := configureClock(cfg.UnixTime); err != nil {
		return err
	}
	bootStart := time.Now()

	_ = syscall.Mount("proc", "/proc", "proc", 0, "")
	_ = syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	if !cfg.DisableCgroupMount {
		mountCgroupFS("/sys/fs/cgroup")
	}
	configureMemoryOvercommit("/proc")

	writeStage(bootStart, "loading modules")
	if err := loadModules(cfg.Modules); err != nil {
		return err
	}
	writeStage(bootStart, "modules loaded")
	if cfg.RootFSTag != "" {
		writeStage(bootStart, "mounting rootfs")
		if err := mountRootFS(cfg.RootFSTag, cfg.EmulatorTag, cfg.DisableCgroupMount); err != nil {
			return err
		}
		writeStage(bootStart, "rootfs mounted")
		if err := configureHostname(cfg.Hostname); err != nil {
			return fmt.Errorf("configure hostname: %w", err)
		}
		if err := configureRuntimeFilesystem(); err != nil {
			return fmt.Errorf("configure runtime filesystem: %w", err)
		}
		if err := configurePackageManagers(""); err != nil {
			return fmt.Errorf("configure package managers: %w", err)
		}
		if cfg.PrecopyAMD64Root {
			writeStage(bootStart, "precopying amd64 root")
			if err := precopyAMD64Root(); err != nil {
				return fmt.Errorf("precopy amd64 root: %w", err)
			}
			writeStage(bootStart, "amd64 root precopied")
		}
		writeStage(bootStart, "configuring binfmt")
		if configured, err := configureBinfmt(); err != nil {
			return fmt.Errorf("configure binfmt: %w", err)
		} else if configured {
			writeStage(bootStart, "binfmt configured")
		} else {
			writeStage(bootStart, "binfmt unavailable")
		}
	}
	if cfg.Network != nil {
		writeStage(bootStart, "configuring network")
		if err := configureNetwork(cfg.Network); err != nil {
			return fmt.Errorf("configure network: %w", err)
		}
		writeStage(bootStart, "network configured")
	}
	writeStage(bootStart, "changing workdir")
	if err := os.Chdir(cfg.WorkDir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.WorkDir, err)
	}

	if len(cfg.Command) == 0 {
		if cfg.VsockPort != 0 {
			if strings.TrimSpace(cfg.InitSystem) != "" {
				return bootInitSystem(cfg, bootStart)
			}
			writeStage(bootStart, "connecting vsock control")
			control, err := connectVsock(cfg.VsockPort)
			if err != nil {
				return fmt.Errorf("connect vsock control: %w", err)
			}
			defer control.Close()
			if cfg.ReadyMarker != "" {
				writeProtocolLineTo(control, initDurationMarker+itoa(int(time.Since(bootStart).Milliseconds())))
				writeStage(bootStart, "sending ready marker")
				writeProtocolLineTo(control, cfg.ReadyMarker)
			}
			writeStage(bootStart, "entering command loop")
			return commandLoop(cfg, control)
		}
		if cfg.ReadyMarker != "" {
			writeKernel(cfg.ReadyMarker)
			if kmsgFD >= 0 {
				writeConsole(cfg.ReadyMarker + "\n")
			}
		}
		return commandLoop(cfg, os.Stdin)
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

func runStage2() error {
	writeKernel("ccx3-init: stage2 starting")
	var cfg config
	buf, err := readStage2Config()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(buf, &cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/"
	}
	if cfg.VsockPort == 0 {
		return fmt.Errorf("vsock control port is not configured")
	}
	writeKernel("ccx3-init: stage2 config loaded")
	if err := os.Chdir(cfg.WorkDir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.WorkDir, err)
	}
	control, err := connectStage2Control(cfg.VsockPort)
	if err != nil {
		return fmt.Errorf("connect vsock control: %w", err)
	}
	defer control.Close()
	if cfg.ReadyMarker != "" {
		writeProtocolLineTo(control, initDurationMarker+"0")
		writeProtocolLineTo(control, cfg.ReadyMarker)
	}
	return commandLoop(cfg, control)
}

func waitForSystemdControlSocket(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := socketExists("/run/systemd/private")
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("systemd control socket did not appear within %s", timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func socketExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return false, fmt.Errorf("%s exists but is not a socket", path)
		}
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", path, err)
}

func systemdSystemBusExpected() bool {
	for _, candidate := range []string{
		"/usr/bin/dbus-daemon",
		"/bin/dbus-daemon",
		"/usr/bin/dbus-broker-launch",
		"/lib/systemd/system/dbus.service",
		"/usr/lib/systemd/system/dbus.service",
	} {
		if pathExists(candidate) {
			return true
		}
	}
	return false
}

func readStage2Config() ([]byte, error) {
	if path := strings.TrimSpace(os.Getenv("CCX3_STAGE2_CONFIG")); path != "" {
		return os.ReadFile(path)
	}
	buf, err := os.ReadFile(stage2ConfigPath)
	if err == nil {
		return buf, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.ReadFile(configPath)
}

func connectStage2Control(port uint32) (*os.File, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		control, err := connectVsock(port)
		if err == nil {
			if attempt > 0 {
				writeKernel("ccx3-init: stage2 vsock connected")
			}
			return control, nil
		}
		lastErr = err
		if attempt == 0 {
			writeKernel("ccx3-init: stage2 waiting for vsock control: " + err.Error())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, lastErr
}

func bootInitSystem(cfg config, bootStart time.Time) error {
	initSystem := strings.TrimSpace(cfg.InitSystem)
	switch initSystem {
	case "", "ccx3":
		return fmt.Errorf("init system is not configured")
	case initSystemSystemd:
		return bootSystemd(cfg, bootStart)
	default:
		return fmt.Errorf("unsupported init system %q", initSystem)
	}
}

func bootSystemd(cfg config, bootStart time.Time) error {
	systemdPath, err := findSystemd()
	if err != nil {
		return err
	}
	writeStage(bootStart, "installing ccx3 stage2")
	if err := installSystemdStage2(cfg); err != nil {
		return err
	}
	writeStage(bootStart, "exec systemd")
	return syscall.Exec(systemdPath, []string{systemdPath}, os.Environ())
}

func findSystemd() (string, error) {
	for _, candidate := range []string{"/lib/systemd/systemd", "/usr/lib/systemd/systemd"} {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		return candidate, nil
	}
	if target, err := filepath.EvalSymlinks("/sbin/init"); err == nil && strings.Contains(target, "systemd") {
		return "/sbin/init", nil
	}
	return "", fmt.Errorf("systemd init requested but no systemd binary was found")
}

func installSystemdStage2(cfg config) error {
	if err := copyCurrentExecutable(stage2Path); err != nil {
		return err
	}
	configData, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode stage2 config: %w", err)
	}
	if err := os.WriteFile(stage2ConfigPath, append(configData, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", stage2ConfigPath, err)
	}
	if err := os.MkdirAll("/run/systemd/system/basic.target.wants", 0o755); err != nil {
		return fmt.Errorf("mkdir systemd runtime units: %w", err)
	}
	unitPath := "/run/systemd/system/ccx3-stage2.service"
	unit := `[Unit]
Description=ccx3 guest control stage2
DefaultDependencies=no
After=local-fs.target
Before=basic.target
StartLimitIntervalSec=0

[Service]
Type=simple
Environment=CCX3_STAGE2_CONFIG=/run/ccx3/config.json
ExecStart=/run/ccx3/stage2 --ccx3-stage2
Restart=on-failure
RestartSec=250ms
StandardInput=null
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=basic.target
`
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", unitPath, err)
	}
	if err := ensureSymlink(unitPath, "/run/systemd/system/basic.target.wants/ccx3-stage2.service"); err != nil {
		return fmt.Errorf("enable ccx3 stage2: %w", err)
	}
	for _, unit := range []string{
		"systemd-networkd-wait-online.service",
		"NetworkManager-wait-online.service",
		"systemd-remount-fs.service",
	} {
		if err := ensureSymlink("/dev/null", filepath.Join("/run/systemd/system", unit)); err != nil {
			return fmt.Errorf("mask %s: %w", unit, err)
		}
	}
	return nil
}

func copyCurrentExecutable(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	src, err := os.Open("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy current executable to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	return nil
}

func runExecPivot(args []string) error {
	if len(args) < 7 {
		return fmt.Errorf("invalid exec pivot argument count")
	}
	rootDir := args[0]
	workDir := args[1]
	uid := args[2]
	gid := args[3]
	groups := args[4]
	if args[5] != "--" {
		return fmt.Errorf("invalid exec pivot separator")
	}
	argv := args[6:]
	if len(argv) == 0 {
		return fmt.Errorf("missing command")
	}
	if rootDir != "" {
		if err := pivotExecRoot(rootDir); err != nil {
			return err
		}
	}
	if workDir != "" {
		if err := os.Chdir(workDir); err != nil {
			return fmt.Errorf("chdir %s: %w", workDir, err)
		}
	}
	if uid != "" || gid != "" || groups != "" {
		if err := applyExecCredential(uid, gid, groups); err != nil {
			return err
		}
	}
	argv0, err := lookPathForExec(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(argv0, argv, os.Environ())
}

func pivotExecRoot(rootDir string) error {
	if rootDir == "" {
		return fmt.Errorf("missing root_dir")
	}
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mount namespace private: %w", err)
	}
	putOld := filepath.Join(rootDir, ".ccx3-old-root")
	if err := os.MkdirAll(putOld, 0o700); err != nil {
		return fmt.Errorf("mkdir put_old: %w", err)
	}
	if err := syscall.PivotRoot(rootDir, putOld); err != nil {
		return fmt.Errorf("pivot_root %s: %w", rootDir, err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / after pivot_root: %w", err)
	}
	if err := syscall.Unmount("/.ccx3-old-root", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}
	if err := os.Remove("/.ccx3-old-root"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old root: %w", err)
	}
	return nil
}

func applyExecCredential(uidText, gidText, groupsText string) error {
	uid, err := parseUint32(uidText)
	if err != nil {
		return fmt.Errorf("invalid uid %q: %w", uidText, err)
	}
	gid, err := parseUint32(gidText)
	if err != nil {
		return fmt.Errorf("invalid gid %q: %w", gidText, err)
	}
	var groups []int
	if groupsText != "" {
		for _, part := range strings.Split(groupsText, ",") {
			group, err := parseUint32(part)
			if err != nil {
				return fmt.Errorf("invalid group %q: %w", part, err)
			}
			groups = append(groups, int(group))
		}
	}
	if err := syscall.Setgroups(groups); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setgid(int(gid)); err != nil {
		return fmt.Errorf("setgid: %w", err)
	}
	if err := syscall.Setuid(int(uid)); err != nil {
		return fmt.Errorf("setuid: %w", err)
	}
	return nil
}

func lookPathForExec(file string) (string, error) {
	if strings.Contains(file, "/") {
		return file, nil
	}
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("executable file not found in PATH: %s", file)
}

func writeStage(start time.Time, stage string) {
	line := fmt.Sprintf("ccx3-init: +%dms %s", time.Since(start).Milliseconds(), stage)
	writeKernel(line)
	writeConsole(line + "\n")
}

func configureClock(unixTime int64) error {
	if unixTime <= 0 {
		return nil
	}
	tv := unix.NsecToTimeval(unixTime * int64(time.Second))
	if err := setTimeOfDay(&tv); err != nil {
		return fmt.Errorf("set guest clock: %w", err)
	}
	return nil
}

func mountRootFS(tag, emulatorTag string, disableCgroup bool) error {
	if err := os.MkdirAll("/mnt", 0o755); err != nil {
		return fmt.Errorf("mkdir /mnt: %w", err)
	}
	if err := syscall.Mount(tag, "/mnt", "virtiofs", 0, ""); err != nil {
		return fmt.Errorf("mount virtiofs %s: %w", tag, err)
	}
	if err := switchRootFS("/mnt"); err != nil {
		return err
	}

	for _, dir := range []string{"/proc", "/sys", "/dev", "/tmp", "/run"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	_ = syscall.Mount("proc", "/proc", "proc", 0, "")
	_ = syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	if !disableCgroup {
		mountCgroupFS("/sys/fs/cgroup")
	}
	configureMemoryOvercommit("/proc")
	_ = syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "")
	_ = syscall.Mount("tmpfs", "/tmp", "tmpfs", 0, "mode=1777")
	_ = syscall.Mount("tmpfs", "/run", "tmpfs", 0, "mode=755")
	for _, dir := range []string{"/dev/pts", "/dev/shm"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	_ = syscall.Mount("devpts", "/dev/pts", "devpts", 0, "gid=5,mode=620,ptmxmode=666")
	_ = syscall.Mount("tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777")
	if emulatorTag != "" {
		if err := os.MkdirAll("/run/ccx3", 0o755); err != nil {
			return fmt.Errorf("mkdir /run/ccx3: %w", err)
		}
		if err := syscall.Mount(emulatorTag, "/run/ccx3", "virtiofs", 0, ""); err != nil {
			return fmt.Errorf("mount emulator virtiofs %s: %w", emulatorTag, err)
		}
	}
	configureDeviceLinks()
	return nil
}

func switchRootFS(newRoot string) error {
	if err := os.Chdir(newRoot); err != nil {
		return fmt.Errorf("chdir %s before switch_root: %w", newRoot, err)
	}
	// pivot_root cannot move away from the initramfs rootfs. Use the
	// switch_root pattern so later exec roots are not nested under a chroot,
	// which would prevent user-namespace sandboxes such as bubblewrap.
	for _, mount := range []string{"/sys/fs/cgroup", "/sys", "/proc"} {
		_ = syscall.Unmount(mount, syscall.MNT_DETACH)
	}
	if err := emptyRootFS(newRoot); err != nil {
		return err
	}
	if err := syscall.Mount(".", "/", "", syscall.MS_MOVE, ""); err != nil {
		return fmt.Errorf("move root %s to /: %w", newRoot, err)
	}
	if err := syscall.Chroot("."); err != nil {
		return fmt.Errorf("chroot after switch_root: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / after switch_root: %w", err)
	}
	return nil
}

func emptyRootFS(keep string) error {
	keep = "/" + strings.Trim(strings.TrimSpace(keep), "/")
	entries, err := os.ReadDir("/")
	if err != nil {
		return fmt.Errorf("read old root: %w", err)
	}
	for _, entry := range entries {
		name := "/" + entry.Name()
		if name == keep {
			continue
		}
		if err := removeRootFSEntry(name); err != nil {
			return err
		}
	}
	return nil
}

func removeRootFSEntry(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat old root entry %s: %w", name, err)
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		entries, err := os.ReadDir(name)
		if err != nil {
			return fmt.Errorf("read old root dir %s: %w", name, err)
		}
		for _, entry := range entries {
			if err := removeRootFSEntry(filepath.Join(name, entry.Name())); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old root entry %s: %w", name, err)
	}
	return nil
}

func mountCgroupFS(path string) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		writeKernel("ccx3-init: mkdir cgroup mountpoint: " + err.Error())
		return
	}
	if err := syscall.Mount("none", path, "cgroup2", 0, ""); err == nil || errors.Is(err, syscall.EBUSY) {
		return
	}
	// Older or custom kernels may only expose cgroup v1. Mounting cgroup2 is
	// best-effort so ordinary containers continue to boot on those kernels.
	if err := syscall.Mount("cgroup", path, "cgroup", 0, ""); err != nil && !errors.Is(err, syscall.EBUSY) {
		writeKernel("ccx3-init: mount cgroup filesystem: " + err.Error())
	}
}

func configureRuntimeFilesystem() error {
	if err := mountRuntimeTmpFS("/var/tmp", "mode=1777"); err != nil {
		return err
	}
	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{"/run/lock", 0o1777},
		{"/run/user", 0o755},
		{"/run/sshd", 0o755},
		{"/var/log", 0o755},
		{"/var/cache", 0o755},
		{"/var/lib/dbus", 0o755},
	} {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir.path, err)
		}
		if err := os.Chmod(dir.path, dir.mode); err != nil {
			return fmt.Errorf("chmod %s: %w", dir.path, err)
		}
	}
	if err := ensureMachineID(); err != nil {
		return err
	}
	if err := ensureLocaltime(); err != nil {
		return err
	}
	return nil
}

func mountRuntimeTmpFS(path, options string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := syscall.Mount("tmpfs", path, "tmpfs", 0, options); err != nil && !errors.Is(err, syscall.EBUSY) {
		return fmt.Errorf("mount tmpfs on %s: %w", path, err)
	}
	return nil
}

func configureDeviceLinks() {
	for target, link := range map[string]string{
		"/proc/self/fd":   "/dev/fd",
		"/proc/self/fd/0": "/dev/stdin",
		"/proc/self/fd/1": "/dev/stdout",
		"/proc/self/fd/2": "/dev/stderr",
	} {
		if err := ensureSymlink(target, link); err != nil {
			writeKernel("ccx3-init: link " + link + ": " + err.Error())
		}
	}
}

func ensureLocaltime() error {
	if pathExists("/etc/localtime") {
		return nil
	}
	if !pathExists("/usr/share/zoneinfo/Etc/UTC") {
		return nil
	}
	return ensureSymlink("/usr/share/zoneinfo/Etc/UTC", "/etc/localtime")
}

func ensureSymlink(target, link string) error {
	if existing, err := os.Readlink(link); err == nil {
		if existing == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		// Create it below.
	} else if info, statErr := os.Lstat(link); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}

func ensureMachineID() error {
	id, err := readMachineID("/etc/machine-id")
	if err != nil {
		return err
	}
	if id == "" {
		id, err = newMachineID()
		if err != nil {
			return err
		}
		if err := os.WriteFile("/etc/machine-id", []byte(id+"\n"), 0o444); err != nil {
			return fmt.Errorf("write /etc/machine-id: %w", err)
		}
	}
	dbusPath := "/var/lib/dbus/machine-id"
	if existing, err := readMachineID(dbusPath); err != nil {
		return err
	} else if existing == id {
		return nil
	}
	_ = os.Remove(dbusPath)
	if err := os.Symlink("/etc/machine-id", dbusPath); err == nil {
		return nil
	}
	if err := os.WriteFile(dbusPath, []byte(id+"\n"), 0o444); err != nil {
		return fmt.Errorf("write %s: %w", dbusPath, err)
	}
	return nil
}

func readMachineID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" || id == "uninitialized" {
		return "", nil
	}
	return id, nil
}

func newMachineID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate machine-id: %w", err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return hex.EncodeToString(buf[:]), nil
}

func precopyAMD64Root() error {
	for _, path := range []string{
		"/run/ccx3/qemu-x86_64",
		"/bin/busybox",
		"/lib/ld-musl-x86_64.so.1",
		"/lib/libc.musl-x86_64.so.1",
	} {
		if err := copyPath(path, filepath.Join("/run/ccx3-precopy", path)); err != nil {
			return err
		}
	}
	return nil
}

func configureNetwork(cfg *network) error {
	if cfg == nil {
		return nil
	}
	iface := strings.TrimSpace(cfg.Interface)
	if iface == "" {
		iface = "eth0"
	}
	address := strings.TrimSpace(cfg.Address)
	if address == "" {
		address = "10.42.0.2/24"
	}
	gateway := strings.TrimSpace(cfg.Gateway)
	if gateway == "" {
		gateway = "10.42.0.1"
	}
	dns := strings.TrimSpace(cfg.DNS)
	if dns == "" {
		dns = gateway
	}
	if err := configureNetworkLink("lo"); err != nil {
		writeKernel("ccx3-init: configure lo: " + err.Error())
	}
	if err := configureNetworkLink(iface); err != nil {
		return err
	}
	if err := configureNetworkAddress(iface, address); err != nil {
		return err
	}
	if err := configureNetworkDefaultRoute(iface, gateway); err != nil {
		return err
	}
	if err := writeResolverConfig(dns); err != nil {
		return err
	}
	return nil
}

func writeResolverConfig(dns string) error {
	data := []byte("nameserver " + dns + "\n")
	if err := writeFileReplacingMissingSymlink("/etc/resolv.conf", data, 0o644); err != nil {
		return fmt.Errorf("write /etc/resolv.conf: %w", err)
	}
	return nil
}

func writeFileReplacingMissingSymlink(name string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(name, data, perm); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info, err := os.Lstat(name); err == nil && info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(name)
		return os.WriteFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

func configureNetworkLink(name string) error {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return fmt.Errorf("find interface %s: %w", name, err)
	}
	msg := unix.IfInfomsg{
		Family: unix.AF_UNSPEC,
		Index:  int32(iface.Index),
		Flags:  unix.IFF_UP,
		Change: unix.IFF_UP,
	}
	if err := netlinkRequest(unix.RTM_NEWLINK, 0, structBytes(&msg), false); err != nil {
		return fmt.Errorf("set link %s up: %w", name, err)
	}
	return nil
}

func configureNetworkAddress(ifaceName, address string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("find interface %s: %w", ifaceName, err)
	}
	ip, ipNet, err := net.ParseCIDR(address)
	if err != nil {
		return fmt.Errorf("parse address %s: %w", address, err)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("address %s is not IPv4", address)
	}
	ones, _ := ipNet.Mask.Size()
	msg := unix.IfAddrmsg{
		Family:    unix.AF_INET,
		Prefixlen: uint8(ones),
		Scope:     unix.RT_SCOPE_UNIVERSE,
		Index:     uint32(iface.Index),
	}
	payload := structBytes(&msg)
	payload = appendRtAttr(payload, unix.IFA_LOCAL, ip4)
	payload = appendRtAttr(payload, unix.IFA_ADDRESS, ip4)
	if err := netlinkRequest(unix.RTM_NEWADDR, unix.NLM_F_CREATE|unix.NLM_F_REPLACE, payload, true); err != nil {
		return fmt.Errorf("add address %s to %s: %w", address, ifaceName, err)
	}
	return nil
}

func configureNetworkDefaultRoute(ifaceName, gateway string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("find interface %s: %w", ifaceName, err)
	}
	gw := net.ParseIP(gateway).To4()
	if gw == nil {
		return fmt.Errorf("gateway %s is not IPv4", gateway)
	}
	msg := unix.RtMsg{
		Family:   unix.AF_INET,
		Table:    unix.RT_TABLE_MAIN,
		Protocol: unix.RTPROT_STATIC,
		Scope:    unix.RT_SCOPE_UNIVERSE,
		Type:     unix.RTN_UNICAST,
	}
	payload := structBytes(&msg)
	payload = appendRtAttr(payload, unix.RTA_GATEWAY, gw)
	oif := uint32(iface.Index)
	payload = appendRtAttr(payload, unix.RTA_OIF, structBytes(&oif))
	if err := netlinkRequest(unix.RTM_NEWROUTE, unix.NLM_F_CREATE|unix.NLM_F_REPLACE, payload, true); err != nil {
		return fmt.Errorf("add default route via %s dev %s: %w", gateway, ifaceName, err)
	}
	return nil
}

func netlinkRequest(msgType uint16, flags uint16, payload []byte, ignoreExists bool) error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	seq := uint32(time.Now().UnixNano())
	hdr := unix.NlMsghdr{
		Len:   uint32(unix.NLMSG_HDRLEN + len(payload)),
		Type:  msgType,
		Flags: unix.NLM_F_REQUEST | unix.NLM_F_ACK | flags,
		Seq:   seq,
	}
	msg := append(structBytes(&hdr), payload...)
	if err := unix.Sendto(fd, msg, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	return readNetlinkAck(fd, seq, ignoreExists)
}

func readNetlinkAck(fd int, seq uint32, ignoreExists bool) error {
	buf := make([]byte, 8192)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return err
		}
		for off := 0; off+unix.NLMSG_HDRLEN <= n; {
			hdr := (*unix.NlMsghdr)(unsafe.Pointer(&buf[off]))
			if hdr.Len < unix.NLMSG_HDRLEN || off+int(hdr.Len) > n {
				return fmt.Errorf("short netlink response")
			}
			next := off + nlmsgAlign(int(hdr.Len))
			if hdr.Seq != seq {
				off = next
				continue
			}
			switch hdr.Type {
			case unix.NLMSG_ERROR:
				if int(hdr.Len) < unix.NLMSG_HDRLEN+int(unsafe.Sizeof(unix.NlMsgerr{})) {
					return fmt.Errorf("short netlink error response")
				}
				msgErr := (*unix.NlMsgerr)(unsafe.Pointer(&buf[off+unix.NLMSG_HDRLEN]))
				if msgErr.Error == 0 {
					return nil
				}
				errno := syscall.Errno(-msgErr.Error)
				if ignoreExists && errno == syscall.EEXIST {
					return nil
				}
				return errno
			case unix.NLMSG_DONE:
				return nil
			}
			off = next
		}
	}
}

func appendRtAttr(buf []byte, attrType uint16, data []byte) []byte {
	attr := unix.RtAttr{
		Len:  uint16(unsafe.Sizeof(unix.RtAttr{}) + uintptr(len(data))),
		Type: attrType,
	}
	buf = append(buf, structBytes(&attr)...)
	buf = append(buf, data...)
	for len(buf)%unix.NLMSG_ALIGNTO != 0 {
		buf = append(buf, 0)
	}
	return buf
}

func nlmsgAlign(n int) int {
	return (n + unix.NLMSG_ALIGNTO - 1) & ^(unix.NLMSG_ALIGNTO - 1)
}

func structBytes[T any](value *T) []byte {
	size := int(unsafe.Sizeof(*value))
	data := unsafe.Slice((*byte)(unsafe.Pointer(value)), size)
	out := make([]byte, size)
	copy(out, data)
	return out
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return nil
}

func copyPath(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", src, err)
		}
		_ = os.Remove(dst)
		if err := os.Symlink(target, dst); err != nil {
			return fmt.Errorf("symlink %s: %w", dst, err)
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

func configureMemoryOvercommit(procRoot string) {
	path := filepath.Join(procRoot, "sys/vm/overcommit_memory")
	_ = os.WriteFile(path, []byte("1\n"), 0o644)
}

func configureBinfmt() (bool, error) {
	if _, err := os.Stat(guestQEMUPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := copyPath(guestQEMUPath, guestQEMUBinfmtPath); err != nil {
		return false, fmt.Errorf("copy qemu-x86_64 for binfmt: %w", err)
	}
	if err := os.Chmod(guestQEMUBinfmtPath, 0o755); err != nil {
		return false, fmt.Errorf("chmod %s: %w", guestQEMUBinfmtPath, err)
	}
	if err := os.MkdirAll("/proc/sys/fs/binfmt_misc", 0o755); err != nil {
		return false, fmt.Errorf("mkdir binfmt_misc: %w", err)
	}
	if err := syscall.Mount("binfmt_misc", "/proc/sys/fs/binfmt_misc", "binfmt_misc", 0, ""); err != nil && !errors.Is(err, syscall.EBUSY) {
		if errors.Is(err, syscall.ENODEV) {
			writeKernel("ccx3-init: binfmt_misc unavailable: " + err.Error())
			return false, nil
		}
		return false, fmt.Errorf("mount binfmt_misc: %w", err)
	}
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/qemu-x86_64"); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat qemu-x86_64 registration: %w", err)
	}
	const qemuX8664Registration = ":qemu-x86_64:M::\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x3e\\x00:\\xff\\xff\\xff\\xff\\xff\\xfe\\xfe\\x00\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xfe\\xff\\xff\\xff:" + guestQEMUBinfmtPath + ":F"
	if err := os.WriteFile("/proc/sys/fs/binfmt_misc/register", []byte(qemuX8664Registration), 0o644); err != nil {
		return false, fmt.Errorf("register qemu-x86_64: %w", err)
	}
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/qemu-x86_64"); err != nil {
		return false, fmt.Errorf("verify qemu-x86_64 registration: %w", err)
	}
	return true, nil
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

func writeProtocolLine(value string) {
	protocolMu.Lock()
	defer protocolMu.Unlock()
	writeConsole(strings.TrimRight(value, "\n") + "\n")
}

func writeProtocolLineTo(w io.Writer, value string) {
	protocolMu.Lock()
	defer protocolMu.Unlock()
	_, _ = io.WriteString(w, strings.TrimRight(value, "\n")+"\n")
}

func writeExecStderr(cfg config, control io.Writer, id, value string) {
	if cfg.ErrorMarkerPref == "" || value == "" {
		return
	}
	writeProtocolLineTo(control, cfg.ErrorMarkerPref+id+":"+base64.StdEncoding.EncodeToString([]byte(value)))
}

func writeExecStdoutBytes(cfg config, control io.Writer, id string, data []byte) {
	if cfg.OutputMarkerPref == "" || len(data) == 0 {
		return
	}
	writeProtocolLineTo(control, cfg.OutputMarkerPref+id+":"+base64.StdEncoding.EncodeToString(data))
}

func writeExecControlBytes(cfg config, control io.Writer, id string, data []byte) {
	if cfg.ControlMarkerPref == "" || len(data) == 0 {
		return
	}
	writeProtocolLineTo(control, cfg.ControlMarkerPref+id+":"+base64.StdEncoding.EncodeToString(data))
}

func writeExecTiming(control io.Writer, id, phase string, start time.Time) {
	if id == "" || phase == "" {
		return
	}
	writeProtocolLineTo(control, execTimingMarker+id+":"+phase+":"+itoa(int(time.Since(start).Milliseconds())))
}

func appendFileContents(buf *strings.Builder, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		buf.WriteString(path)
		buf.WriteString(": ")
		buf.WriteString(err.Error())
		buf.WriteString("\n")
		return
	}
	buf.WriteString(path)
	buf.WriteString(":\n")
	buf.Write(data)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		buf.WriteString("\n")
	}
}

func appendStat(buf *strings.Builder, label, path string) {
	info, err := os.Stat(path)
	if err != nil {
		buf.WriteString(label)
		buf.WriteString(": ")
		buf.WriteString(path)
		buf.WriteString(": ")
		buf.WriteString(err.Error())
		buf.WriteString("\n")
		return
	}
	buf.WriteString(label)
	buf.WriteString(": ")
	buf.WriteString(path)
	buf.WriteString(" mode=")
	buf.WriteString(fmt.Sprintf("%#o", info.Mode()&0o777))
	if info.IsDir() {
		buf.WriteString(" dir=true")
	}
	buf.WriteString("\n")
}

func collectExecDiagnostics(rootDir string, argv []string, workDir string) string {
	var buf strings.Builder
	if rootDir != "" {
		appendStat(&buf, "root_dir", rootDir)
	}
	if len(argv) != 0 {
		appendStat(&buf, "argv0", argv[0])
		if rootDir != "" && strings.HasPrefix(argv[0], "/") {
			appendStat(&buf, "argv0@root", rootDir+argv[0])
		}
	}
	if workDir != "" {
		appendStat(&buf, "workdir", workDir)
		if rootDir != "" && strings.HasPrefix(workDir, "/") {
			appendStat(&buf, "workdir@root", rootDir+workDir)
		}
	}
	appendStat(&buf, "/run", "/run")
	appendStat(&buf, "/run/ccx3", "/run/ccx3")
	appendStat(&buf, guestQEMUBinfmtPath, guestQEMUBinfmtPath)
	if info, err := os.Stat(guestQEMUPath); err != nil {
		buf.WriteString(guestQEMUPath)
		buf.WriteString(": ")
		buf.WriteString(err.Error())
		buf.WriteString("\n")
	} else {
		buf.WriteString(guestQEMUPath)
		buf.WriteString(" mode: ")
		buf.WriteString(fmt.Sprintf("%#o", info.Mode()&0o777))
		buf.WriteString("\n")
	}
	appendFileContents(&buf, "/proc/sys/fs/binfmt_misc/status")
	appendFileContents(&buf, "/proc/sys/fs/binfmt_misc/qemu-x86_64")
	return buf.String()
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

func parseSignal(name string) (syscall.Signal, error) {
	switch strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(name), "SIG")) {
	case "HUP":
		return syscall.SIGHUP, nil
	case "INT":
		return syscall.SIGINT, nil
	case "QUIT":
		return syscall.SIGQUIT, nil
	case "TERM":
		return syscall.SIGTERM, nil
	case "KILL":
		return syscall.SIGKILL, nil
	case "USR1":
		return syscall.SIGUSR1, nil
	case "USR2":
		return syscall.SIGUSR2, nil
	case "WINCH":
		return syscall.SIGWINCH, nil
	default:
		return 0, fmt.Errorf("unsupported signal %q", name)
	}
}

func openPTY(cols, rows int) (*os.File, *os.File, error) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	master := os.NewFile(uintptr(masterFD), "ptmx")
	if master == nil {
		_ = unix.Close(masterFD)
		return nil, nil, fmt.Errorf("open /dev/ptmx: no file handle")
	}

	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("unlock ptmx: %w", err)
	}
	ptn, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("query pty number: %w", err)
	}
	if cols > 0 && rows > 0 {
		if err := unix.IoctlSetWinsize(masterFD, unix.TIOCSWINSZ, &unix.Winsize{
			Col: uint16(cols),
			Row: uint16(rows),
		}); err != nil {
			_ = master.Close()
			return nil, nil, fmt.Errorf("set initial winsize: %w", err)
		}
	}
	slave, err := os.OpenFile("/dev/pts/"+itoa(ptn), os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open slave pty: %w", err)
	}
	return master, slave, nil
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

	exitCode, usage, err := execCommandGo(cfg.Command, cfg.Env, cfg.WorkDir, cfg.User)
	if err != nil {
		return fmt.Errorf("run %s: %w", cfg.Command[0], err)
	}
	if cfg.UsageMarkerPref != "" && usage != nil {
		usageMarker := cfg.UsageMarkerPref + encodeExecUsage(usage)
		writeKernel(usageMarker)
		writeProtocolLine(usageMarker)
	}
	if cfg.ExitMarkerPrefix != "" {
		exitMarker := cfg.ExitMarkerPrefix + itoa(exitCode)
		writeKernel(exitMarker)
		writeProtocolLine(exitMarker)
	}
	syscall.Sync()
	_ = syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF)
	for {
		syscall.Pause()
	}
}

func execCommandGo(argv []string, env []string, workDir string, user string) (int, *execUsage, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	start := time.Now()
	cmd.Env = env
	if workDir != "" {
		cmd.Dir = workDir
	}
	console := os.NewFile(uintptr(consoleFD), "/dev/console")
	cmd.Stdin = console
	cmd.Stdout = console
	cmd.Stderr = console
	if cred, err := credentialForUser(user); err != nil {
		return 0, nil, err
	} else if cred != nil {
		if err := ensureCredentialUser("", cred); err != nil {
			return 0, nil, err
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}

	err := cmd.Run()
	usage := usageFromProcessState(cmd.ProcessState, time.Since(start))
	if err == nil {
		return 0, usage, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return processExitCode(cmd.ProcessState, exitErr.ExitCode()), usage, nil
	}
	return 0, usage, err
}

func commandLoop(cfg config, control io.ReadWriter) error {
	if cfg.ControlMarkerPref == "" {
		cfg.ControlMarkerPref = defaultControlMarkerPref
	}
	reader := bufio.NewReader(control)
	active := map[string]*managedExec{}
	pendingStdinClose := map[string]bool{}
	var activeMu sync.Mutex
	var systemdCommandMu sync.Mutex
	systemdCommandReady := strings.TrimSpace(cfg.InitSystem) != initSystemSystemd
	waitForCommandReady := func(argv []string) func() error {
		if strings.TrimSpace(cfg.InitSystem) != initSystemSystemd {
			return nil
		}
		if !commandNeedsSystemdReady(argv) {
			return nil
		}
		return func() error {
			systemdCommandMu.Lock()
			if systemdCommandReady {
				systemdCommandMu.Unlock()
				return nil
			}
			systemdCommandMu.Unlock()
			if err := waitForSystemdCommandReady(systemdReadyTimeout); err != nil {
				return err
			}
			systemdCommandMu.Lock()
			systemdCommandReady = true
			systemdCommandMu.Unlock()
			return nil
		}
	}
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("read exec request: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req execRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeKernel("ccx3-init: decode exec request: " + err.Error())
			if cfg.ExitMarkerPrefix != "" {
				writeKernel(cfg.ExitMarkerPrefix + "125")
			}
			continue
		}
		if req.Kind == "" {
			req.Kind = "exec"
		}
		switch req.Kind {
		case "exec":
		case "sync":
			if req.ID == "" {
				writeKernel("ccx3-init: sync request missing id")
				continue
			}
			go runSyncRequest(cfg, control, req.ID)
			continue
		case "fs_archive":
			if req.ID == "" {
				writeKernel("ccx3-init: fs_archive request missing id")
				continue
			}
			go runFSArchiveRequest(cfg, control, req.ID, req.RootDir, req.Path)
			continue
		case "fs_mkdir":
			if req.ID == "" {
				writeKernel("ccx3-init: fs_mkdir request missing id")
				continue
			}
			go runFSMkdirRequest(cfg, control, req.ID, req.RootDir, req.Path, firstNonEmpty(req.User, cfg.User))
			continue
		case "fs_write":
			if req.ID == "" {
				writeKernel("ccx3-init: fs_write request missing id")
				continue
			}
			go runFSWriteRequest(cfg, control, req.ID, req.RootDir, req.Path, firstNonEmpty(req.User, cfg.User), io.NopCloser(bytes.NewReader(req.Stdin)))
			continue
		case "fs_extract":
			if req.ID == "" {
				writeKernel("ccx3-init: fs_extract request missing id")
				continue
			}
			if len(req.Stdin) > 0 {
				go runFSExtractRequest(cfg, control, req.ID, req.RootDir, req.Path, req.Dir, req.User, io.NopCloser(bytes.NewReader(req.Stdin)), func() {})
				continue
			}
			stdinR, stdinW := io.Pipe()
			managed := &managedExec{stdin: stdinW, start: time.Now()}
			activeMu.Lock()
			active[req.ID] = managed
			activeMu.Unlock()
			go runFSExtractRequest(cfg, control, req.ID, req.RootDir, req.Path, req.Dir, req.User, stdinR, func() {
				_ = managed.closeStdin()
				activeMu.Lock()
				delete(active, req.ID)
				activeMu.Unlock()
			})
			continue
		case "stdin":
			activeMu.Lock()
			managed := active[req.ID]
			activeMu.Unlock()
			if managed == nil {
				writeKernel("ccx3-init: stdin for unknown exec id " + req.ID)
				continue
			}
			if err := managed.writeStdin(req.Stdin); err != nil {
				writeKernel("ccx3-init: write stdin: " + err.Error())
			}
			continue
		case "stdin_close":
			activeMu.Lock()
			managed := active[req.ID]
			if managed == nil {
				pendingStdinClose[req.ID] = true
			}
			activeMu.Unlock()
			if managed == nil {
				continue
			}
			writeExecTiming(control, req.ID, "stdin_close_recv", managed.start)
			if err := managed.closeStdin(); err != nil {
				writeKernel("ccx3-init: close stdin: " + err.Error())
			}
			continue
		case "signal":
			activeMu.Lock()
			managed := active[req.ID]
			activeMu.Unlock()
			if managed == nil {
				writeKernel("ccx3-init: signal for unknown exec id " + req.ID)
				continue
			}
			if err := managed.signal(req.Signal); err != nil {
				writeKernel("ccx3-init: signal " + req.Signal + ": " + err.Error())
			}
			continue
		case "resize":
			activeMu.Lock()
			managed := active[req.ID]
			activeMu.Unlock()
			if managed == nil {
				continue
			}
			if err := managed.resize(req.Cols, req.Rows); err != nil {
				writeKernel("ccx3-init: resize " + itoa(req.Cols) + "x" + itoa(req.Rows) + ": " + err.Error())
			}
			continue
		default:
			writeKernel("ccx3-init: unsupported control kind " + req.Kind)
			continue
		}
		if len(req.Command) == 0 {
			writeKernel("ccx3-init: exec request missing command")
			if cfg.ExitMarkerPrefix != "" {
				writeKernel(cfg.ExitMarkerPrefix + "125")
			}
			continue
		}
		if req.ID == "" {
			writeKernel("ccx3-init: exec request missing id")
			continue
		}

		workDir := req.WorkDir
		if workDir == "" {
			workDir = cfg.WorkDir
		}
		env := req.Env
		if len(env) == 0 {
			env = cfg.Env
		}
		user := req.User
		if user == "" {
			user = cfg.User
		}

		stdinR, stdinW := io.Pipe()
		managed := &managedExec{stdin: stdinW, start: time.Now()}
		activeMu.Lock()
		active[req.ID] = managed
		closeStdin := pendingStdinClose[req.ID]
		delete(pendingStdinClose, req.ID)
		activeMu.Unlock()
		go runManagedExec(cfg, control, req.ID, req.Command, env, req.RootDir, workDir, user, stdinR, managed, req.TTY, req.ControlFD, req.Cols, req.Rows, waitForCommandReady(req.Command), func() {
			_ = managed.closeStdin()
			activeMu.Lock()
			delete(active, req.ID)
			activeMu.Unlock()
		})
		if closeStdin {
			if err := managed.closeStdin(); err != nil {
				writeKernel("ccx3-init: close pending stdin: " + err.Error())
			}
		}
		if len(req.Stdin) > 0 {
			initialStdin := append([]byte(nil), req.Stdin...)
			go func(id string, managed *managedExec) {
				if err := managed.writeStdin(initialStdin); err != nil {
					writeKernel("ccx3-init: write initial stdin: " + err.Error())
				}
				if err := managed.closeStdin(); err != nil {
					writeKernel("ccx3-init: close initial stdin: " + err.Error())
				}
			}(req.ID, managed)
		}
	}
}

func commandNeedsSystemdReady(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return commandTextNeedsSystemdReady(argv[0], argv[1:])
}

func commandTextNeedsSystemdReady(cmd string, args []string) bool {
	switch filepath.Base(cmd) {
	case "systemctl", "journalctl", "loginctl", "busctl", "hostnamectl", "timedatectl", "localectl", "networkctl", "resolvectl":
		return true
	case "service":
		return len(args) == 0 || args[0] != "--help"
	case "sudo", "doas", "env", "command":
		next, rest, ok := unwrapCommandPrefix(args)
		if !ok {
			return false
		}
		return commandTextNeedsSystemdReady(next, rest)
	case "sh", "bash", "dash", "zsh":
		for i, arg := range args {
			if arg == "-c" && i+1 < len(args) {
				return shellScriptNeedsSystemdReady(args[i+1])
			}
		}
	}
	return false
}

func unwrapCommandPrefix(args []string) (string, []string, bool) {
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			args = args[1:]
			continue
		}
		if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
			args = args[1:]
			continue
		}
		if strings.HasPrefix(arg, "-") {
			args = args[1:]
			if arg == "-u" || arg == "-g" || arg == "-C" || arg == "-S" || arg == "--user" || arg == "--group" || arg == "--chdir" || arg == "--shell" {
				if len(args) > 0 {
					args = args[1:]
				}
			}
			continue
		}
		return arg, args[1:], true
	}
	return "", nil, false
}

func shellScriptNeedsSystemdReady(script string) bool {
	fields := strings.FieldsFunc(script, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ';', '&', '|', '(', ')', '<', '>', '`', '$', '"', '\'', '\\':
			return true
		default:
			return false
		}
	})
	for _, field := range fields {
		if commandTextNeedsSystemdReady(field, nil) {
			return true
		}
	}
	return false
}

func waitForSystemdCommandReady(timeout time.Duration) error {
	if ok, err := socketExists("/run/systemd/private"); err != nil {
		return err
	} else if !ok {
		if err := waitForSystemdControlSocket(timeout); err != nil {
			return err
		}
	}
	if !systemdSystemBusExpected() {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := socketExists("/run/dbus/system_bus_socket")
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("systemd system bus socket did not appear within %s", timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func runSyncRequest(cfg config, control io.Writer, id string) {
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	syscall.Sync()
	if cfg.ExitMarkerPrefix != "" {
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":0")
	}
}

func runFSArchiveRequest(cfg config, control io.Writer, id, rootDir, src string) {
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	exitCode := 0
	if err := archivePathToControl(cfg, control, id, rootDir, src); err != nil {
		exitCode = 1
		writeExecStderr(cfg, control, id, "ccx3-init: fs archive: "+err.Error()+"\n")
	}
	if cfg.ExitMarkerPrefix != "" {
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
	}
}

func archivePathToControl(cfg config, control io.Writer, id, rootDir, src string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("source path is required")
	}
	src = rootPath(rootDir, filepath.Clean(src))
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() {
		err := writePathTar(pw, src, filepath.Base(src), info)
		_ = pw.CloseWithError(err)
	}()
	var buf [32 * 1024]byte
	for {
		n, err := pr.Read(buf[:])
		if n > 0 {
			writeExecStdoutBytes(cfg, control, id, buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func runFSExtractRequest(cfg config, control io.Writer, id, rootDir, dst string, dstDir bool, user string, stdin io.ReadCloser, cleanup func()) {
	defer cleanup()
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	exitCode := 0
	if err := extractTarToPath(stdin, rootDir, dst, dstDir); err != nil {
		exitCode = 1
		writeExecStderr(cfg, control, id, "ccx3-init: fs extract: "+err.Error()+"\n")
	}
	if cfg.ExitMarkerPrefix != "" {
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
	}
}

func runFSMkdirRequest(cfg config, control io.Writer, id, rootDir, dir, user string) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	exitCode := 0
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	target := rootPath(rootDir, filepath.Clean(dir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		exitCode = 1
		writeExecStderr(cfg, control, id, "ccx3-init: fs mkdir: "+err.Error()+"\n")
	} else if err := chownPathForUser(target, user); err != nil {
		exitCode = 1
		writeExecStderr(cfg, control, id, "ccx3-init: fs mkdir: "+err.Error()+"\n")
	}
	if cfg.ExitMarkerPrefix != "" {
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
	}
}

func runFSWriteRequest(cfg config, control io.Writer, id, rootDir, dst, user string, stdin io.ReadCloser) {
	defer stdin.Close()
	exitCode := 0
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	if strings.TrimSpace(dst) == "" {
		writeExecStderr(cfg, control, id, "ccx3-init: fs write: destination path is required\n")
		exitCode = 1
		if cfg.ExitMarkerPrefix != "" {
			writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
		}
		return
	}
	target := rootPath(rootDir, filepath.Clean(dst))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		writeExecStderr(cfg, control, id, "ccx3-init: fs write: "+err.Error()+"\n")
		exitCode = 1
	} else if err := writeStreamToPath(stdin, target, 0o644); err != nil {
		writeExecStderr(cfg, control, id, "ccx3-init: fs write: "+err.Error()+"\n")
		exitCode = 1
	} else if err := chownPathForUser(target, user); err != nil {
		writeExecStderr(cfg, control, id, "ccx3-init: fs write: "+err.Error()+"\n")
		exitCode = 1
	}
	if cfg.ExitMarkerPrefix != "" {
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
	}
}

func writeStreamToPath(r io.Reader, target string, mode os.FileMode) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, r)
	closeErr := file.Close()
	return errors.Join(copyErr, closeErr)
}

func chownPathForUser(target, user string) error {
	cred, err := credentialForUser(user)
	if err != nil || cred == nil {
		return err
	}
	return os.Chown(target, int(cred.Uid), int(cred.Gid))
}

func writePathTar(w io.Writer, src, rootName string, rootInfo os.FileInfo) error {
	tw := tar.NewWriter(w)
	defer tw.Close()
	return filepath.WalkDir(src, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(src), filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			rel = rootName
			info = rootInfo
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractTarToPath(r io.Reader, rootDir, dst string, dstDir bool) error {
	if strings.TrimSpace(dst) == "" {
		return fmt.Errorf("destination path is required")
	}
	dst = rootPath(rootDir, filepath.Clean(dst))
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dstDir = true
	}
	tr := tar.NewReader(r)
	sawEntry := false
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if !sawEntry {
				return fmt.Errorf("archive is empty")
			}
			return nil
		}
		if err != nil {
			return err
		}
		sawEntry = true
		target, err := guestTarTarget(dst, dstDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			continue
		}
	}
}

func guestTarTarget(dst string, dstDir bool, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, "/")))
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe tar path %q", name)
	}
	if dstDir {
		return filepath.Join(dst, cleanName), nil
	}
	parts := strings.SplitN(cleanName, string(filepath.Separator), 2)
	if len(parts) == 1 {
		return dst, nil
	}
	return filepath.Join(dst, parts[1]), nil
}

func execPivotArgs(rootDir, workDir string, cred *syscall.Credential, argv []string) []string {
	uid := ""
	gid := ""
	groups := ""
	if cred != nil {
		uid = fmt.Sprint(cred.Uid)
		gid = fmt.Sprint(cred.Gid)
		if len(cred.Groups) > 0 {
			parts := make([]string, 0, len(cred.Groups))
			for _, group := range cred.Groups {
				parts = append(parts, fmt.Sprint(group))
			}
			groups = strings.Join(parts, ",")
		}
	}
	args := []string{rootDir, workDir, uid, gid, groups, "--"}
	return append(args, argv...)
}

func runManagedExec(cfg config, control io.Writer, id string, argv []string, env []string, rootDir string, workDir string, user string, stdin io.ReadCloser, managed *managedExec, tty bool, controlFD bool, cols int, rows int, waitReady func() error, cleanup func()) {
	defer cleanup()
	execStart := time.Now()
	writeExecTiming(control, id, "recv", execStart)
	if cfg.BeginMarker != "" {
		writeProtocolLineTo(control, cfg.BeginMarker+id)
	}
	writeKernel("ccx3-init: exec " + strings.Join(argv, " "))
	if waitReady != nil {
		writeExecTiming(control, id, "guest_ready_begin", execStart)
		if err := waitReady(); err != nil {
			writeKernel("ccx3-init: guest not ready for exec: " + err.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: guest not ready for exec: "+err.Error()+"\n")
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
		writeExecTiming(control, id, "guest_ready_done", execStart)
	}
	writeExecTiming(control, id, "start_begin", execStart)

	signalGroup := true
	var rootMounts []string
	if rootDir != "" {
		preparedRoot, mounts, err := prepareExecRoot(rootDir)
		if err != nil {
			writeKernel("ccx3-init: prepare exec root: " + err.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: prepare exec root: "+err.Error()+"\n")
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
		rootDir = preparedRoot
		rootMounts = mounts
		defer teardownExecRoot(rootMounts)
	}
	execCred, err := credentialForUser(user)
	if err != nil {
		writeKernel("ccx3-init: resolve user: " + err.Error())
		writeExecStderr(cfg, control, id, "ccx3-init: resolve user: "+err.Error()+"\n")
		if cfg.ExitMarkerPrefix != "" {
			writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
		}
		return
	}
	if execCred != nil {
		if err := ensureCredentialUser(rootDir, execCred); err != nil {
			writeKernel("ccx3-init: ensure user: " + err.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: ensure user: "+err.Error()+"\n")
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
		if err := ensureCredentialWorkDir(rootDir, workDir, execCred); err != nil {
			writeKernel("ccx3-init: ensure workdir: " + err.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: ensure workdir: "+err.Error()+"\n")
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
	}
	var cmd *exec.Cmd
	useExecPivot := rootDir != "" || (tty && execCred != nil)
	if useExecPivot {
		cmd = exec.Command("/proc/self/exe", append([]string{execPivotMode}, execPivotArgs(rootDir, workDir, execCred, argv)...)...)
	} else {
		cmd = exec.Command(argv[0], argv[1:]...)
		if workDir != "" {
			cmd.Dir = workDir
		}
	}
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	var (
		done       chan struct{}
		stdoutW    *io.PipeWriter
		stderrW    *io.PipeWriter
		controlR   *os.File
		controlW   *os.File
		ptyMaster  *os.File
		ptySlave   *os.File
		startError error
	)
	if controlFD {
		controlR, controlW, startError = os.Pipe()
		if startError != nil {
			writeKernel("ccx3-init: open control fd: " + startError.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: open control fd: "+startError.Error()+"\n")
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
		defer func() {
			if controlR != nil {
				_ = controlR.Close()
			}
		}()
		defer func() {
			if controlW != nil {
				_ = controlW.Close()
			}
		}()
		cmd.ExtraFiles = append(cmd.ExtraFiles, controlW)
	}

	if tty {
		ptyMaster, ptySlave, startError = openPTY(cols, rows)
		if startError != nil {
			writeKernel("ccx3-init: open pty: " + startError.Error())
			if cfg.ExitMarkerPrefix != "" {
				writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
			}
			return
		}
		defer func() {
			if ptyMaster != nil {
				_ = ptyMaster.Close()
			}
		}()
		defer func() {
			if ptySlave != nil {
				_ = ptySlave.Close()
			}
		}()
		managed.setPTY(ptyMaster)
		cmd.Stdin = ptySlave
		cmd.Stdout = ptySlave
		cmd.Stderr = ptySlave
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid:  true,
			Setctty: true,
			Ctty:    0,
		}
		streams := 1
		if stdin != nil {
			streams++
		}
		if controlR != nil {
			streams++
		}
		done = make(chan struct{}, streams)
		go func() {
			defer func() { done <- struct{}{} }()
			var buf [256]byte
			first := true
			for {
				n, err := ptyMaster.Read(buf[:])
				if n > 0 && cfg.OutputMarkerPref != "" {
					if first {
						writeExecTiming(control, id, "first_stdout", execStart)
						first = false
					}
					writeProtocolLineTo(control, cfg.OutputMarkerPref+id+":"+base64.StdEncoding.EncodeToString(buf[:n]))
				}
				if err != nil {
					return
				}
			}
		}()
		if stdin != nil {
			go func() {
				defer func() { done <- struct{}{} }()
				defer stdin.Close()
				var buf [256]byte
				for {
					n, err := stdin.Read(buf[:])
					if n > 0 {
						if _, writeErr := ptyMaster.Write(buf[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						if err == io.EOF {
							_, _ = ptyMaster.Write([]byte{4})
						}
						return
					}
				}
			}()
		}
	} else {
		if stdin != nil {
			defer stdin.Close()
			cmd.Stdin = stdin
		} else {
			devNull, err := os.Open("/dev/null")
			if err == nil {
				defer devNull.Close()
				cmd.Stdin = devNull
			}
		}

		stdoutR, stdoutPipeW := io.Pipe()
		stderrR, stderrPipeW := io.Pipe()
		stdoutW = stdoutPipeW
		stderrW = stderrPipeW
		cmd.Stdout = stdoutW
		cmd.Stderr = stderrW

		streams := 2
		if controlR != nil {
			streams++
		}
		done = make(chan struct{}, streams)
		go func() {
			defer func() { done <- struct{}{} }()
			defer stdoutR.Close()
			var buf [256]byte
			first := true
			for {
				n, err := stdoutR.Read(buf[:])
				if n > 0 && cfg.OutputMarkerPref != "" {
					if first {
						writeExecTiming(control, id, "first_stdout", execStart)
						first = false
					}
					writeProtocolLineTo(control, cfg.OutputMarkerPref+id+":"+base64.StdEncoding.EncodeToString(buf[:n]))
				}
				if err != nil {
					return
				}
			}
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			defer stderrR.Close()
			var buf [256]byte
			first := true
			for {
				n, err := stderrR.Read(buf[:])
				if n > 0 && cfg.ErrorMarkerPref != "" {
					if first {
						writeExecTiming(control, id, "first_stderr", execStart)
						first = false
					}
					writeProtocolLineTo(control, cfg.ErrorMarkerPref+id+":"+base64.StdEncoding.EncodeToString(buf[:n]))
				}
				if err != nil {
					return
				}
			}
		}()
	}
	if controlR != nil {
		go func() {
			defer func() { done <- struct{}{} }()
			var buf [256]byte
			for {
				n, err := controlR.Read(buf[:])
				if n > 0 {
					writeExecControlBytes(cfg, control, id, buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if rootDir != "" {
		cmd.SysProcAttr.Cloneflags |= syscall.CLONE_NEWNS
	} else if execCred != nil && !useExecPivot {
		cmd.SysProcAttr.Credential = execCred
	}

	writeExecTiming(control, id, "start_call", execStart)
	startErr := cmd.Start()
	if controlW != nil {
		_ = controlW.Close()
		controlW = nil
	}
	if startErr != nil {
		_ = managed.closeStdin()
		if ptySlave != nil {
			_ = ptySlave.Close()
			ptySlave = nil
		}
		if ptyMaster != nil {
			_ = ptyMaster.Close()
			ptyMaster = nil
		}
		if stdoutW != nil {
			_ = stdoutW.Close()
		}
		if stderrW != nil {
			_ = stderrW.Close()
		}
		for i := 0; i < cap(done); i++ {
			<-done
		}
		writeKernel("ccx3-init: exec error: " + startErr.Error())
		writeExecStderr(cfg, control, id, "ccx3-init: exec error: "+startErr.Error()+"\n"+collectExecDiagnostics(rootDir, argv, workDir))
		if cfg.ExitMarkerPrefix != "" {
			writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":126")
		}
		return
	}
	writeExecTiming(control, id, "started", execStart)
	managed.setProcess(cmd.Process, signalGroup)
	if ptySlave != nil {
		_ = ptySlave.Close()
		ptySlave = nil
	}

	writeExecTiming(control, id, "wait_begin", execStart)
	waitErr := cmd.Wait()
	usage := usageFromProcessState(cmd.ProcessState, time.Since(execStart))
	writeExecTiming(control, id, "wait_done", execStart)
	if tty {
		_ = managed.closeStdin()
	}
	if stdoutW != nil {
		_ = stdoutW.Close()
	}
	if stderrW != nil {
		_ = stderrW.Close()
	}
	for i := 0; i < cap(done); i++ {
		<-done
	}
	writeExecTiming(control, id, "streams_done", execStart)

	exitCode := 0
	if waitErr != nil {
		if errors.Is(waitErr, exec.ErrWaitDelay) {
			waitErr = nil
		}
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = processExitCode(cmd.ProcessState, exitErr.ExitCode())
		} else {
			writeKernel("ccx3-init: exec error: " + waitErr.Error())
			writeExecStderr(cfg, control, id, "ccx3-init: exec error: "+waitErr.Error()+"\n")
			exitCode = 126
		}
	}
	if cfg.UsageMarkerPref != "" && usage != nil {
		writeProtocolLineTo(control, cfg.UsageMarkerPref+id+":"+encodeExecUsage(usage))
	}
	if cfg.ExitMarkerPrefix != "" {
		writeExecTiming(control, id, "exit_sent", execStart)
		writeProtocolLineTo(control, cfg.ExitMarkerPrefix+id+":"+itoa(exitCode))
	}
}

func processExitCode(state *os.ProcessState, fallback int) int {
	if state == nil {
		return fallback
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return fallback
	}
	return 128 + int(status.Signal())
}

type execUsage struct {
	WallSeconds   float64 `json:"wall_seconds,omitempty"`
	UserSeconds   float64 `json:"user_seconds,omitempty"`
	SystemSeconds float64 `json:"system_seconds,omitempty"`
	CPUSeconds    float64 `json:"cpu_seconds,omitempty"`
	MaxRSSBytes   uint64  `json:"max_rss_bytes,omitempty"`
}

func usageFromProcessState(state *os.ProcessState, wall time.Duration) *execUsage {
	usage := &execUsage{WallSeconds: wall.Seconds()}
	if state == nil {
		return usage
	}
	if state.UserTime() > 0 {
		usage.UserSeconds = state.UserTime().Seconds()
	}
	if state.SystemTime() > 0 {
		usage.SystemSeconds = state.SystemTime().Seconds()
	}
	usage.CPUSeconds = usage.UserSeconds + usage.SystemSeconds
	if raw, ok := state.SysUsage().(*syscall.Rusage); ok && raw != nil && raw.Maxrss > 0 {
		usage.MaxRSSBytes = uint64(raw.Maxrss) * 1024
	}
	return usage
}

func encodeExecUsage(usage *execUsage) string {
	buf, err := json.Marshal(usage)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func configureHostname(hostname string) error {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || hostname == "(none)" {
		hostname = "ccx3"
	}
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		return fmt.Errorf("set hostname %q: %w", hostname, err)
	}
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return fmt.Errorf("mkdir /etc: %w", err)
	}
	_ = os.WriteFile("/etc/hostname", []byte(hostname+"\n"), 0o644)
	hosts := "127.0.0.1\tlocalhost " + hostname + "\n::1\tlocalhost ip6-localhost ip6-loopback " + hostname + "\n"
	_ = os.WriteFile("/etc/hosts", []byte(hosts), 0o644)
	return nil
}

func configurePackageManagers(rootDir string) error {
	aptDir := rootPath(rootDir, "/etc/apt/apt.conf.d")
	if !pathExists(rootPath(rootDir, "/usr/bin/apt")) && !pathExists(rootPath(rootDir, "/bin/apt")) {
		return nil
	}
	if err := os.MkdirAll(aptDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", aptDir, err)
	}
	conf := strings.Join([]string{
		`Acquire::Queue-Mode "access";`,
		`Acquire::http::Pipeline-Depth "0";`,
		`Acquire::https::Pipeline-Depth "0";`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(aptDir, "99ccvm-netstack"), []byte(conf), 0o644); err != nil {
		return fmt.Errorf("write apt netstack config: %w", err)
	}
	return nil
}

func pathExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func ensureCredentialUser(rootDir string, cred *syscall.Credential) error {
	if cred == nil || cred.Uid == 0 {
		return nil
	}
	rootDir = strings.TrimRight(rootDir, "/")
	uid := fmt.Sprintf("%d", cred.Uid)
	gid := fmt.Sprintf("%d", cred.Gid)
	name := usernameForUID(rootDir, uid)
	if name == "" {
		name = availableUserName(rootDir, "cc")
	}
	homeDir := homeDirForUID(rootDir, uid)
	if homeDir == "" {
		homeDir = "/home/" + name
	}
	group := groupNameForGID(rootDir, gid)
	if group == "" {
		group = availableGroupName(rootDir, name)
		if err := appendGroupEntry(rootDir, group, gid); err != nil {
			return err
		}
	}
	if name != "" && passwdHasUID(rootDir, uid) {
		return ensureCredentialHome(rootDir, homeDir, cred)
	}
	if err := appendPasswdEntry(rootDir, name, uid, gid, homeDir, "/bin/sh"); err != nil {
		return err
	}
	return ensureCredentialHome(rootDir, homeDir, cred)
}

func ensureCredentialHome(rootDir, homeDir string, cred *syscall.Credential) error {
	if strings.TrimSpace(homeDir) == "" || homeDir == "/" {
		return nil
	}
	return ensureCredentialDirectory(rootDir, homeDir, cred)
}

func ensureCredentialWorkDir(rootDir, workDir string, cred *syscall.Credential) error {
	workDir = filepath.Clean(strings.TrimSpace(workDir))
	if workDir == "" || workDir == "." || workDir == "/" {
		return nil
	}
	if !strings.HasPrefix(workDir, "/home/") {
		return nil
	}
	return ensureCredentialDirectory(rootDir, workDir, cred)
}

func ensureCredentialDirectory(rootDir, dir string, cred *syscall.Credential) error {
	if strings.TrimSpace(dir) == "" || dir == "/" {
		return nil
	}
	path := rootPath(rootDir, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, int(cred.Uid), int(cred.Gid)); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func rootPath(rootDir, name string) string {
	if rootDir == "" {
		return name
	}
	return filepath.Join(rootDir, strings.TrimPrefix(name, "/"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func usernameForUID(rootDir, uid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/passwd")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == uid {
			return fields[0]
		}
	}
	return ""
}

func homeDirForUID(rootDir, uid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/passwd")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 6 && fields[2] == uid {
			return fields[5]
		}
	}
	return ""
}

func groupNameForGID(rootDir, gid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/group")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == gid {
			return fields[0]
		}
	}
	return ""
}

func passwdHasUID(rootDir, uid string) bool {
	return usernameForUID(rootDir, uid) != ""
}

func colonFileLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func availableUserName(rootDir, base string) string {
	if !nameExists(rootPath(rootDir, "/etc/passwd"), base) {
		return base
	}
	for i := 1000; ; i++ {
		name := base + itoa(i)
		if !nameExists(rootPath(rootDir, "/etc/passwd"), name) {
			return name
		}
	}
}

func availableGroupName(rootDir, base string) string {
	if !nameExists(rootPath(rootDir, "/etc/group"), base) {
		return base
	}
	for i := 1000; ; i++ {
		name := base + itoa(i)
		if !nameExists(rootPath(rootDir, "/etc/group"), name) {
			return name
		}
	}
}

func nameExists(path, name string) bool {
	for _, line := range colonFileLines(path) {
		fields := strings.Split(line, ":")
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func appendGroupEntry(rootDir, name, gid string) error {
	path := rootPath(rootDir, "/etc/group")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return appendLine(path, name+":x:"+gid+":")
}

func appendPasswdEntry(rootDir, name, uid, gid, home, shell string) error {
	path := rootPath(rootDir, "/etc/passwd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return appendLine(path, strings.Join([]string{name, "x", uid, gid, "ccvm user", home, shell}, ":"))
}

func appendLine(path, line string) error {
	existing, _ := os.ReadFile(path)
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, prefix+line+"\n"); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func credentialForUser(user string) (*syscall.Credential, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, nil
	}
	if user == "root" || user == "0" || user == "0:0" {
		return nil, nil
	}
	uidPart, gidPart, hasGID := strings.Cut(user, ":")
	if uidPart == "" {
		return nil, fmt.Errorf("invalid user %q", user)
	}
	uid, err := parseUint32(uidPart)
	if err != nil {
		return nil, fmt.Errorf("invalid uid %q", uidPart)
	}
	gid := uid
	if hasGID {
		if gidPart == "" {
			return nil, fmt.Errorf("invalid gid in user %q", user)
		}
		gid, err = parseUint32(gidPart)
		if err != nil {
			return nil, fmt.Errorf("invalid gid %q", gidPart)
		}
	}
	return &syscall.Credential{Uid: uid, Gid: gid}, nil
}

func parseUint32(value string) (uint32, error) {
	n := uint64(0)
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not numeric")
		}
		n = n*10 + uint64(ch-'0')
		if n > uint64(^uint32(0)) {
			return 0, fmt.Errorf("out of range")
		}
	}
	return uint32(n), nil
}

func prepareExecRoot(rootDir string) (string, []string, error) {
	cleaned := strings.TrimSpace(rootDir)
	if cleaned == "" {
		return "", nil, nil
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "", nil, fmt.Errorf("root_dir must be absolute")
	}
	mounts := make([]string, 0, 6)
	if err := syscall.Mount(cleaned, cleaned, "", syscall.MS_BIND, ""); err != nil {
		return "", nil, fmt.Errorf("bind mount exec root %s: %w", cleaned, err)
	}
	mounts = append(mounts, cleaned)
	for _, dir := range []string{"/proc", "/sys", "/dev", "/run", "/tmp"} {
		target := cleaned + dir
		if err := os.MkdirAll(target, 0o755); err != nil {
			teardownExecRoot(mounts)
			return "", nil, fmt.Errorf("mkdir %s: %w", target, err)
		}
		flags := uintptr(syscall.MS_BIND)
		if dir == "/dev" {
			flags |= syscall.MS_REC
		}
		if err := syscall.Mount(dir, target, "", flags, ""); err != nil {
			teardownExecRoot(mounts)
			return "", nil, fmt.Errorf("bind mount %s -> %s: %w", dir, target, err)
		}
		mounts = append(mounts, target)
	}
	if err := copyExecRootNetworkFiles(cleaned); err != nil {
		teardownExecRoot(mounts)
		return "", nil, err
	}
	return cleaned, mounts, nil
}

func copyExecRootNetworkFiles(rootDir string) error {
	for _, name := range []string{"resolv.conf", "hosts", "hostname"} {
		src := filepath.Join("/etc", name)
		data, err := os.ReadFile(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read %s: %w", src, err)
		}
		dst := filepath.Join(rootDir, "etc", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := writeFileReplacingMissingSymlink(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}

func teardownExecRoot(mounts []string) {
	for i := len(mounts) - 1; i >= 0; i-- {
		_ = syscall.Unmount(mounts[i], 0)
	}
}

type sockaddrVM struct {
	Family   uint16
	Reserved uint16
	Port     uint32
	CID      uint32
	Zero     [4]byte
}

func connectVsock(port uint32) (*os.File, error) {
	fd, err := syscall.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	addr := sockaddrVM{
		Family: unix.AF_VSOCK,
		Port:   port,
		CID:    2,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&addr)), unsafe.Sizeof(addr))
	if errno != 0 {
		_ = syscall.Close(fd)
		return nil, errno
	}
	return os.NewFile(uintptr(fd), fmt.Sprintf("vsock:%d", port)), nil
}
