//go:build darwin && arm64

package hvf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/kernel/alpine"
	bootarm64 "j5.nz/cc/internal/linux/boot/arm64"
	"j5.nz/cc/internal/linux/initramfs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
)

const (
	containerMemoryBase   = 0xa0000000
	defaultMemorySize     = 512 << 20
	containerGICDistMin   = 0x08000000
	containerGICDistMax   = containerGICDistMin + 0x00010000
	containerGICRedistMin = 0x080a0000
	containerGICRedistMax = containerGICRedistMin + 0x00020000
	containerConsoleBase  = 0x0a100000
	containerConsoleSize  = 0x1000
	containerConsoleIRQ   = 40
	containerFSBase       = 0x0a101000
	containerFSSize       = 0x1000
	containerFSIRQ        = 41
	containerRootFSTag    = "rootfs"
	commandBeginMarker    = "__CCX3_BEGIN__"
	commandExitMarkerPref = "__CCX3_EXIT__:"
)

type ContainerRunRequest struct {
	Kernel   []byte
	Init     []byte
	Modules  []alpine.Module
	Image    *oci.Image
	Command  []string
	Env      []string
	WorkDir  string
	User     string
	MemoryMB uint64
	CPUs     int
	Dmesg    bool
}

type ContainerRunResult struct {
	ExitCode   int
	Output     string
	Transcript string
}

type ContainerSession struct {
	cancel context.CancelFunc
	doneCh chan sessionRunResult
}

type sessionRunResult struct {
	result ContainerRunResult
	err    error
}

func StartContainer(ctx context.Context, req ContainerRunRequest) (*ContainerSession, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan error, 1)
	doneCh := make(chan sessionRunResult, 1)

	go func() {
		result, err := runContainer(runCtx, req, readyCh)
		doneCh <- sessionRunResult{result: result, err: err}
	}()

	select {
	case err := <-readyCh:
		if err != nil {
			cancel()
			res := <-doneCh
			if res.err != nil {
				return nil, res.err
			}
			return nil, err
		}
		return &ContainerSession{cancel: cancel, doneCh: doneCh}, nil
	case <-ctx.Done():
		cancel()
		res := <-doneCh
		if res.err != nil {
			return nil, res.err
		}
		return nil, ctx.Err()
	}
}

func (s *ContainerSession) Wait() error {
	res := <-s.doneCh
	return res.err
}

func (s *ContainerSession) Close() error {
	s.cancel()
	return nil
}

func RunContainer(ctx context.Context, req ContainerRunRequest) (ContainerRunResult, error) {
	return runContainer(ctx, req, nil)
}

func runContainer(ctx context.Context, req ContainerRunRequest, readyCh chan<- error) (ContainerRunResult, error) {
	if req.Image == nil {
		return ContainerRunResult{}, fmt.Errorf("image is required")
	}
	if len(req.Kernel) == 0 {
		return ContainerRunResult{}, fmt.Errorf("kernel is required")
	}
	if req.CPUs > 1 {
		return ContainerRunResult{}, fmt.Errorf("only 1 CPU is supported")
	}
	user := strings.TrimSpace(req.User)
	if user == "" {
		user = strings.TrimSpace(req.Image.Config.User)
	}
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return ContainerRunResult{}, fmt.Errorf("only root user is supported")
	}

	command := req.Image.Command(req.Command)
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	if len(req.Init) == 0 {
		return ContainerRunResult{}, fmt.Errorf("guest init binary is required")
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = req.Image.Config.WorkingDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return ContainerRunResult{}, fmt.Errorf("workdir must be absolute")
	}

	env := mergeEnv(req.Image.Config.Env, req.Env)
	if !hasEnvKey(env, "PATH") {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if !hasEnvKey(env, "HOME") {
		env = append(env, "HOME=/root")
	}

	command, err := resolveGuestCommand(req.Image.RootFSDir, command, env)
	if err != nil {
		return ContainerRunResult{}, err
	}
	configJSON, err := json.Marshal(guestInitConfig{
		Command:          command,
		Env:              env,
		WorkDir:          workDir,
		Modules:          moduleConfig(req.Modules),
		RootFSTag:        containerRootFSTag,
		BeginMarker:      commandBeginMarker,
		ExitMarkerPrefix: commandExitMarkerPref,
	})
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("marshal guest init config: %w", err)
	}

	extraFiles := []initramfs.File{
		{Path: "/dev", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/proc", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/sys", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/run", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/tmp", Mode: 0o1777, Type: initramfs.TypeDirectory},
		{Path: "/etc", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/ccx3", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/ccx3/modules", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/dev/console", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 5, DevMinor: 1},
		{Path: "/dev/null", Mode: 0o666, Type: initramfs.TypeCharDevice, DevMajor: 1, DevMinor: 3},
		{Path: "/dev/kmsg", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 1, DevMinor: 11},
		{Path: "/etc/ccx3-init.json", Mode: 0o600, Data: configJSON, Type: initramfs.TypeRegular},
		{Path: "/init", Mode: 0o755, Data: req.Init, Type: initramfs.TypeRegular},
	}
	for _, mod := range req.Modules {
		extraFiles = append(extraFiles, initramfs.File{
			Path: "/ccx3/modules/" + mod.Name + ".ko",
			Mode: 0o644,
			Data: mod.Data,
			Type: initramfs.TypeRegular,
		})
	}
	initrd, err := initramfs.Build(extraFiles)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("build initramfs: %w", err)
	}

	vm, err := NewVM()
	if err != nil {
		return ContainerRunResult{}, err
	}
	defer vm.Close()

	memorySize := uint64(defaultMemorySize)
	if req.MemoryMB != 0 {
		memorySize = req.MemoryMB << 20
	}
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(containerMemoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("map guest memory: %w", err)
	}

	var serialOut bytes.Buffer
	var consoleOut bytes.Buffer
	var fsTrace bytes.Buffer
	uart := serial.NewUART8250(bootarm64.DefaultUARTBase, bootarm64.DefaultUARTRegShift, &serialOut)
	console := virtio.NewConsole(containerConsoleBase, containerConsoleSize, containerConsoleIRQ, &consoleOut)
	console.Attach(vm, vm)
	fsdev := virtio.NewFS(containerFSBase, containerFSSize, containerFSIRQ, containerRootFSTag, virtio.NewPassthroughFS(req.Image.RootFSDir))
	fsdev.Attach(vm, vm)
	fsdev.Log = &fsTrace

	plan, err := bootarm64.PrepareBoot(mem, req.Kernel, bootarm64.BootOptions{
		MemoryBase: containerMemoryBase,
		MemorySize: memorySize,
		NumCPUs:    1,
		Initrd:     initrd,
		ExtraNodes: []fdt.Node{console.DeviceTreeNode(), fsdev.DeviceTreeNode()},
		Cmdline: strings.Join([]string{
			"console=ttyS0,115200n8",
			fmt.Sprintf("earlycon=uart8250,mmio,0x%x", bootarm64.DefaultUARTBase),
			"keep_bootcon",
			"nokaslr",
			"panic=-1",
			"loglevel=8",
			"rdinit=/init",
		}, " "),
	})
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("prepare boot: %w", err)
	}

	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, bootarm64.DefaultPStateBits); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetSysReg(hvSysRegSP_EL1, plan.StackTopGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set SP_EL1: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set X0: %w", err)
	}
	for _, reg := range []Reg{hvRegX1, hvRegX2, hvRegX3} {
		if err := vm.SetReg(reg, 0); err != nil {
			return ContainerRunResult{}, fmt.Errorf("clear reg %d: %w", reg, err)
		}
	}

	readySent := false
	for {
		exitInfo, err, stalled := runWithCancel(ctx, vm, 500*time.Millisecond)
		if stalled {
			if ctx.Err() != nil {
				return ContainerRunResult{}, fmt.Errorf("%w\nserial:\n%s\nvirtio-fs:\n%s", ctx.Err(), serialOut.String(), fsTrace.String())
			}
			continue
		}
		if err != nil {
			return ContainerRunResult{}, fmt.Errorf("%w\nserial:\n%s\nvirtio-fs:\n%s", err, serialOut.String(), fsTrace.String())
		}

		transcript := serialOut.String()
		if !readySent && strings.Contains(transcript, commandBeginMarker) {
			readySent = true
			if readyCh != nil {
				readyCh <- nil
				close(readyCh)
				readyCh = nil
			}
		}
		if exitCode, output, ok := extractCommandResult(transcript, req.Dmesg); ok {
			return ContainerRunResult{
				ExitCode:   exitCode,
				Output:     output,
				Transcript: transcript,
			}, nil
		}

		if exitInfo == nil {
			return ContainerRunResult{}, fmt.Errorf("vcpu returned nil exit info")
		}
		if exitInfo.Reason != hvExitReasonException {
			return ContainerRunResult{}, fmt.Errorf("unexpected exit reason %v", exitInfo.Reason)
		}

		switch DecodeExceptionClass(exitInfo.Exception.Syndrome) {
		case ExceptionClassDataAbortLowerEL:
			if err := handleContainerDataAbort(vm, uart, console, fsdev, exitInfo); err != nil {
				return ContainerRunResult{}, err
			}
		case ExceptionClassHVC64:
			halt, err := handleContainerHVC(vm)
			if err != nil {
				return ContainerRunResult{}, err
			}
			if halt {
				if exitCode, output, ok := extractCommandResult(serialOut.String(), req.Dmesg); ok {
					return ContainerRunResult{
						ExitCode:   exitCode,
						Output:     output,
						Transcript: serialOut.String(),
					}, nil
				}
				return ContainerRunResult{}, fmt.Errorf("guest halted before command completed\nserial:\n%s\nvirtio-fs:\n%s", serialOut.String(), fsTrace.String())
			}
		default:
			return ContainerRunResult{}, fmt.Errorf("unexpected exception class %#x syndrome=%#x physical=%#x\nserial:\n%s\nvirtio-fs:\n%s",
				DecodeExceptionClass(exitInfo.Exception.Syndrome), exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress), serialOut.String(), fsTrace.String())
		}
	}
}

func runWithCancel(ctx context.Context, vm *VM, timeout time.Duration) (*VcpuExit, error, bool) {
	resCh := make(chan runResultVM, 1)
	go func() {
		exitInfo, err := vm.Run()
		resCh <- runResultVM{exit: exitInfo, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-resCh:
		return res.exit, res.err, false
	case <-ctx.Done():
		if err := vm.CancelRun(); err != nil {
			return nil, err, false
		}
		res := <-resCh
		if res.err != nil {
			return nil, res.err, false
		}
		return nil, ctx.Err(), false
	case <-timer.C:
		if err := vm.CancelRun(); err != nil {
			return nil, err, false
		}
		res := <-resCh
		if res.err != nil {
			return nil, res.err, false
		}
		if res.exit == nil || res.exit.Reason != hvExitReasonCanceled {
			return nil, fmt.Errorf("cancelled run returned unexpected exit %#v", res.exit), false
		}
		return nil, nil, true
	}
}

type runResultVM struct {
	exit *VcpuExit
	err  error
}

func handleContainerDataAbort(vm *VM, uart *serial.UART8250, console *virtio.Console, fsdev *virtio.FS, exitInfo *VcpuExit) error {
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)

	switch {
	case uart.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := uart.WriteValue(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := uart.ReadValue(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case console != nil && console.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := console.Write(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := console.Read(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case fsdev != nil && fsdev.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := fsdev.Write(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := fsdev.Read(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case mmioInRange(addr, containerGICDistMin, containerGICDistMax) || mmioInRange(addr, containerGICRedistMin, containerGICRedistMax):
		value, err := handleGICAccess(vm, addr, info)
		if err != nil {
			return err
		}
		if !info.Write {
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unhandled MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}

	return vm.AdvanceProgramCounter()
}

func handleContainerHVC(vm *VM) (bool, error) {
	x0, err := vm.GetReg(hvRegX0)
	if err != nil {
		return false, err
	}

	const (
		psciVersion         = 0x84000000
		psciCpuSuspend      = 0x84000001
		psciCpuOff          = 0x84000002
		psciCpuOn           = 0x84000003
		psciAffinityInfo    = 0x84000004
		psciMigrateInfoType = 0x84000006
		psciSystemOff       = 0x84000008
		psciSystemReset     = 0x84000009
		psciFeatures        = 0x8400000a
		psciSuccess         = 0
		psciNotSupported    = 0xffffffff
		psciInvalidParams   = 0xfffffffe
		psciTosNotPresent   = 2
	)

	var ret uint64
	switch x0 {
	case psciVersion:
		ret = 0x00010000
	case psciMigrateInfoType:
		ret = psciTosNotPresent
	case psciFeatures:
		ret = psciNotSupported
	case psciCpuSuspend:
		ret = psciNotSupported
	case psciCpuOff:
		ret = psciSuccess
	case psciAffinityInfo:
		ret = psciInvalidParams
	case psciCpuOn:
		ret = psciInvalidParams
	case psciSystemOff, psciSystemReset:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported PSCI call %#x", x0)
	}

	return false, vm.SetReg(hvRegX0, ret)
}

func readAbortValue(vm *VM, info DataAbortInfo) (uint64, error) {
	if info.Target == hvRegXZR {
		return 0, nil
	}
	value, err := vm.GetReg(info.Target)
	if err != nil {
		return 0, err
	}
	if info.SizeBytes >= 8 {
		return value, nil
	}
	return value & ((uint64(1) << (8 * info.SizeBytes)) - 1), nil
}

func writeAbortValue(vm *VM, info DataAbortInfo, value uint64) error {
	if info.Target == hvRegXZR {
		return nil
	}
	if info.SizeBytes < 8 {
		value &= (uint64(1) << (8 * info.SizeBytes)) - 1
	}
	return vm.SetReg(info.Target, value)
}

func mmioInRange(addr, start, end uint64) bool {
	return addr >= start && addr < end
}

func handleGICAccess(vm *VM, addr uint64, info DataAbortInfo) (uint64, error) {
	var value uint64
	if info.Write {
		v, err := readAbortValue(vm, info)
		if err != nil {
			return 0, err
		}
		value = v
	}

	switch {
	case mmioInRange(addr, containerGICDistMin, containerGICDistMax):
		reg := GICDistributorReg(addr - containerGICDistMin)
		if info.Write {
			err := vm.SetGICDistributorReg(reg, value)
			if err != nil && strings.Contains(err.Error(), "denied") {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICDistributorReg(reg)
		if err != nil && strings.Contains(err.Error(), "denied") && reg == 0xffe8 {
			return 0x30, nil
		}
		return val, err
	case mmioInRange(addr, containerGICRedistMin, containerGICRedistMax):
		reg := GICRedistributorReg(addr - containerGICRedistMin)
		if info.Write {
			err := vm.SetGICRedistributorReg(reg, value)
			if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICRedistributorReg(reg)
		if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
			switch reg {
			case 0x0:
				return 0, nil
			case 0xffe8:
				return 0x30, nil
			case 0x8:
				return 1 << 4, nil
			case 0x14:
				return 0, nil
			default:
				return 0, nil
			}
		}
		return val, err
	default:
		return 0, fmt.Errorf("address %#x outside GIC MMIO ranges", addr)
	}
}

func extractCommandResult(serial string, dmesg bool) (int, string, bool) {
	begin := strings.Index(serial, commandBeginMarker)
	exit := strings.Index(serial, commandExitMarkerPref)
	if begin == -1 || exit == -1 || exit < begin {
		return 0, "", false
	}

	rest := serial[exit+len(commandExitMarkerPref):]
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd == -1 {
		return 0, "", false
	}
	code, err := strconv.Atoi(strings.TrimSpace(rest[:lineEnd]))
	if err != nil {
		return 0, "", false
	}

	output := serial
	if !dmesg {
		beginOutput := serial[begin+len(commandBeginMarker):]
		if strings.HasPrefix(beginOutput, "\r\n") {
			beginOutput = beginOutput[2:]
		} else if strings.HasPrefix(beginOutput, "\n") {
			beginOutput = beginOutput[1:]
		}
		endOffset := strings.Index(beginOutput, commandExitMarkerPref)
		if endOffset >= 0 {
			output = strings.TrimRight(beginOutput[:endOffset], "\r\n")
		} else {
			output = strings.TrimRight(beginOutput, "\r\n")
		}
		output = cleanCommandOutput(output)
	}
	return code, output, true
}

func mergeEnv(base, overrides []string) []string {
	index := map[string]int{}
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range overrides {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		if idx, ok := index[key]; ok {
			out[idx] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

type guestInitConfig struct {
	Command          []string `json:"command"`
	Env              []string `json:"env"`
	WorkDir          string   `json:"workdir"`
	Modules          []string `json:"modules,omitempty"`
	RootFSTag        string   `json:"rootfs_tag,omitempty"`
	BeginMarker      string   `json:"begin_marker"`
	ExitMarkerPrefix string   `json:"exit_marker_prefix"`
}

func moduleConfig(modules []alpine.Module) []string {
	if len(modules) == 0 {
		return nil
	}
	out := make([]string, 0, len(modules))
	for _, mod := range modules {
		out = append(out, "/ccx3/modules/"+mod.Name+".ko")
	}
	return out
}

func resolveGuestCommand(rootfs string, command []string, env []string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is empty")
	}
	if strings.Contains(command[0], "/") {
		hostPath := filepath.Join(rootfs, strings.TrimPrefix(command[0], "/"))
		info, err := os.Lstat(hostPath)
		if err != nil {
			return nil, fmt.Errorf("resolve command %q: %w", command[0], err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("command %q is a directory", command[0])
		}
		return command, nil
	}
	pathEnv := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathEnv = strings.TrimPrefix(kv, "PATH=")
			break
		}
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		guestPath := filepath.ToSlash(filepath.Join(dir, command[0]))
		hostPath := filepath.Join(rootfs, strings.TrimPrefix(guestPath, "/"))
		info, err := os.Lstat(hostPath)
		if err == nil && !info.IsDir() && (info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o111 != 0) {
			resolved := append([]string{guestPath}, command[1:]...)
			return resolved, nil
		}
	}
	return nil, fmt.Errorf("resolve command %q in PATH", command[0])
}

func cleanCommandOutput(output string) string {
	lines := strings.Split(output, "\n")
	cleaned := make([]string, 0, len(lines))
	last := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "[") {
			if idx := strings.Index(line, "] "); idx >= 0 {
				lines[i] = line[idx+2:]
			}
		}
		line = strings.TrimSpace(lines[i])
		if line == "" || line == commandBeginMarker || strings.HasPrefix(line, commandExitMarkerPref) {
			continue
		}
		if line == last {
			continue
		}
		cleaned = append(cleaned, line)
		last = line
	}
	return strings.Join(cleaned, "\n")
}
