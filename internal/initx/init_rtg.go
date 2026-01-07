package initx

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/rtg"
)

// RTG source code for the init program.
// This defines the main initialization logic in RTG format.
// Module preloading is handled separately since it requires binary data embedding.
const rtgInitSource = `package main

func main() int64 {
	// === Phase 1: Create /dev directory and device nodes ===

	// ensure /dev exists
	syscall(SYS_MKDIRAT, AT_FDCWD, "/dev", 0755)

	// create /dev/mem device node
	var devMemMode int64 = %d
	var devMemDeviceID int64 = %d
	syscall(SYS_MKNODAT, AT_FDCWD, "/dev/mem", devMemMode, devMemDeviceID)

	// === Phase 2: Mount filesystems ===

	// mount devtmpfs on /dev
	devMountErr := syscall(SYS_MOUNT, "devtmpfs", "/dev", "devtmpfs", 0, "")
	if devMountErr < 0 {
		if devMountErr != EBUSY {
			printf("failed to mount devtmpfs on /dev: 0x%%x\n", 0 - devMountErr)
			syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
		}
	}

	// create /dev/shm for POSIX shared memory
	syscall(SYS_MKDIRAT, AT_FDCWD, "/dev/shm", 0o1777)
	syscall(SYS_MOUNT, "tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777")

	// === Phase 3: Module preloading placeholder ===
	// Module preloading is injected here at IR level

	// === Phase 4: Console setup ===

	consoleFd := syscall(SYS_OPENAT, AT_FDCWD, "/dev/console", O_RDWR, 0)
	if consoleFd < 0 {
		printf("initx: failed to open /dev/console (errno=0x%%x)\n", 0 - consoleFd)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// create a new session to own the console
	setsidResult := syscall(SYS_SETSID)
	if setsidResult < 0 {
		printf("initx: failed to create session (errno=0x%%x)\n", 0 - setsidResult)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// set controlling terminal
	ttyResult := syscall(SYS_IOCTL, consoleFd, TIOCSCTTY, 0)
	if ttyResult < 0 {
		if ttyResult != EPERM {
			printf("initx: failed to set controlling TTY (errno=0x%%x)\n", 0 - ttyResult)
			syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
		}
	}

	// dup2 consoleFd to stdin, stdout, stderr
	syscall(SYS_DUP3, consoleFd, 0, 0)
	syscall(SYS_DUP3, consoleFd, 1, 0)
	syscall(SYS_DUP3, consoleFd, 2, 0)

	// === Phase 5: Memory mapping ===

	// open /dev/mem
	memFd := syscall(SYS_OPENAT, AT_FDCWD, "/dev/mem", O_RDWR | O_SYNC, 0)
	if memFd < 0 {
		printf("initx: failed to open /dev/mem (errno=0x%%x)\n", 0 - memFd)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// map mailbox region
	mailboxMem := syscall(SYS_MMAP, 0, 0x1000, PROT_READ | PROT_WRITE, MAP_SHARED, memFd, 0xf0000000)
	if mailboxMem < 0 {
		printf("initx: failed to map mailbox region (errno=0x%%x)\n", 0 - mailboxMem)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// map config region (4MB)
	configMem := syscall(SYS_MMAP, 0, 4194304, PROT_READ | PROT_WRITE, MAP_SHARED, memFd, 0xf0003000)
	if configMem < 0 {
		printf("initx: failed to map config region (errno=0x%%x)\n", 0 - configMem)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// map anonymous region for payload execution (4MB)
	anonMem := syscall(SYS_MMAP, 0, 4194304, PROT_READ | PROT_WRITE | PROT_EXEC, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0)
	if anonMem < 0 {
		printf("initx: failed to map anonymous payload region (errno=0x%%x)\n", 0 - anonMem)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	// === Phase 6: Time setup ===

	// allocate timespec buffer
	timespecMem := syscall(SYS_MMAP, 0, 16, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0)
	if timespecMem < 0 {
		goto skip_time_set
	}

	// read time from config region and store in timespec struct
	var timeSec int64 = 0
	var timeNsec int64 = 0
	timeSec = load64(configMem, %d)
	timeNsec = load64(configMem, %d)
	store64(timespecMem, 0, timeSec)
	store64(timespecMem, 8, timeNsec)

	// call clock_settime
	clockSetResult := syscall(SYS_CLOCK_SETTIME, CLOCK_REALTIME, timespecMem)
	if clockSetResult < 0 {
		printf("initx: clock_settime failed (errno=0x%%x), continuing anyway\n", 0 - clockSetResult)
	}

	// free timespec buffer
	syscall(SYS_MUNMAP, timespecMem, 16)

skip_time_set:

	// === Phase 7: Main loop ===

loop:
	// check for magic value
	var configMagic int64 = 0
	configMagic = load32(configMem, 0)
	if configMagic == %d {
		// load payload header
		var codeLen int64 = 0
		var relocCount int64 = 0
		var relocBytes int64 = 0
		var codeOffset int64 = 0

		codeLen = load32(configMem, 4)
		relocCount = load32(configMem, 8)
		relocBytes = relocCount << 2
		codeOffset = %d + relocBytes

		// copy binary payload
		var copySrc int64 = 0
		var copyDst int64 = 0
		var remaining int64 = 0

		copySrc = configMem + codeOffset
		copyDst = anonMem
		remaining = codeLen

	copy_qword_loop:
		if remaining < 4 {
			goto copy_qword_done
		}
		var copyVal32 int64 = 0
		copyVal32 = load32(copySrc, 0)
		store32(copyDst, 0, copyVal32)
		copyDst = copyDst + 4
		copySrc = copySrc + 4
		remaining = remaining - 4
		goto copy_qword_loop

	copy_qword_done:

	copy_tail_loop:
		if remaining == 0 {
			goto copy_done
		}
		var copyVal8 int64 = 0
		copyVal8 = load8(copySrc, 0)
		store8(copyDst, 0, copyVal8)
		copyDst = copyDst + 1
		copySrc = copySrc + 1
		remaining = remaining - 1
		goto copy_tail_loop

	copy_done:

		// apply relocations
		var relocPtr int64 = 0
		var relocIndex int64 = 0

		relocPtr = configMem + %d
		relocIndex = 0

	reloc_loop:
		if relocIndex >= relocCount {
			goto reloc_done
		}
		var relocEntryPtr int64 = 0
		var relocOffset int64 = 0
		var patchPtr int64 = 0
		var patchValue int64 = 0

		relocEntryPtr = relocPtr + (relocIndex << 2)
		relocOffset = load32(relocEntryPtr, 0)
		patchPtr = anonMem + relocOffset
		patchValue = load64(patchPtr, 0)
		patchValue = patchValue + anonMem
		store64(patchPtr, 0, patchValue)
		relocIndex = relocIndex + 1
		goto reloc_loop

	reloc_done:

		// call the payload
		payloadResult := call(anonMem)

		// publish return code for host-side exit propagation
		store32(mailboxMem, %d, payloadResult)

		// signal completion to host
		var doneSignal int64 = 0x444f4e45
		store32(mailboxMem, 0, doneSignal)

		goto loop
	} else {
		// magic value not found
		var actualMagic int64 = 0
		actualMagic = load32(configMem, 0)
		printf("Magic value not found in config region: %%x\n", actualMagic)
		syscall(SYS_REBOOT, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2, %d, 0)
	}

	goto loop

	return 0
}
`

// BuildFromRTG builds the init program from RTG source code.
// This is the new implementation that uses RTG instead of direct IR construction.
func BuildFromRTG(cfg BuilderConfig) (*ir.Program, error) {
	// Select reboot command based on architecture
	var rebootCmd int64
	switch cfg.Arch {
	case hv.ArchitectureX86_64:
		rebootCmd = int64(linux.LINUX_REBOOT_CMD_RESTART)
	case hv.ArchitectureARM64:
		rebootCmd = int64(linux.LINUX_REBOOT_CMD_POWER_OFF)
	default:
		return nil, fmt.Errorf("unsupported architecture for RTG init: %s", cfg.Arch)
	}

	// Format the RTG source with constants
	source := fmt.Sprintf(rtgInitSource,
		devMemMode,                      // devMemMode
		int64(devMemDeviceID),           // devMemDeviceID
		rebootCmd,                       // reboot after devtmpfs failure
		rebootCmd,                       // reboot after console failure
		rebootCmd,                       // reboot after setsid failure
		rebootCmd,                       // reboot after tty failure
		rebootCmd,                       // reboot after /dev/mem failure
		rebootCmd,                       // reboot after mailbox map failure
		rebootCmd,                       // reboot after config map failure
		rebootCmd,                       // reboot after anon map failure
		configTimeSecField,              // time sec offset in config
		configTimeNsecField,             // time nsec offset in config
		configHeaderMagicValue,          // expected magic value
		configHeaderSize,                // config header size
		configHeaderSize,                // reloc ptr base offset
		mailboxRunResultDetailOffset,    // mailbox result offset
		rebootCmd,                       // reboot on bad magic
	)

	// Compile the RTG source
	prog, err := rtg.CompileProgram(source)
	if err != nil {
		return nil, fmt.Errorf("failed to compile RTG init source: %w", err)
	}

	// If there are preload modules, we need to inject them into the main method
	// This is handled separately since RTG doesn't support binary data embedding
	if len(cfg.PreloadModules) > 0 {
		if err := injectModulePreloads(prog, cfg); err != nil {
			return nil, fmt.Errorf("failed to inject module preloads: %w", err)
		}
	}

	return prog, nil
}

// injectModulePreloads adds module loading code to the IR program.
// This is necessary because RTG doesn't support embedding binary data.
func injectModulePreloads(prog *ir.Program, cfg BuilderConfig) error {
	// Get the main method
	main, ok := prog.Methods["main"]
	if !ok {
		return fmt.Errorf("main method not found")
	}

	// Build preload fragments
	var preloads ir.Block
	moduleParamArg := any(ir.Int64(0))

	if len(cfg.PreloadModules) > 0 {
		moduleParamPtr := ir.Var("__initx_module_params_ptr")
		moduleParamArg = moduleParamPtr

		preloads = append(preloads, ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
			Target:        nextExecVar(),
			Data:          nil,
			ZeroTerminate: true,
			Pointer:       moduleParamPtr,
		}))
	}

	for idx, mod := range cfg.PreloadModules {
		dataPtr := ir.Var(fmt.Sprintf("__initx_module_%d_data_ptr", idx))
		dataLen := ir.Var(fmt.Sprintf("__initx_module_%d_data_len", idx))
		resultVar := ir.Var(fmt.Sprintf("__initx_module_%d_result", idx))

		preloads = append(preloads, ir.Block{
			ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
				Target:  nextExecVar(),
				Data:    mod.Data,
				Pointer: dataPtr,
				Length:  dataLen,
			}),
			LogKmsg(fmt.Sprintf("initx: loading module %s\n", mod.Name)),
			ir.Assign(resultVar, ir.Syscall(
				defs.SYS_INIT_MODULE,
				dataPtr,
				dataLen,
				moduleParamArg,
			)),
			rebootOnError(cfg.Arch, resultVar, fmt.Sprintf("initx: failed to load module %s (errno=0x%%x)\n", mod.Name)),
			LogKmsg(fmt.Sprintf("initx: module %s ready\n", mod.Name)),
		})
	}

	// Find the marker comment in main and inject preloads
	// For now, insert preloads near the beginning after the filesystem setup
	// We'll look for the console opening phase and insert before it
	var newMain ir.Method
	injected := false

	for _, frag := range main {
		// Look for the console open syscall and inject preloads before it
		if !injected {
			if assign, ok := frag.(ir.AssignFragment); ok {
				if v, ok := assign.Dst.(ir.Var); ok && string(v) == "consoleFd" {
					// Inject preloads here
					for _, p := range preloads {
						newMain = append(newMain, p)
					}
					injected = true
				}
			}
		}
		newMain = append(newMain, frag)
	}

	// If we couldn't find the injection point, append at end (shouldn't happen)
	if !injected && len(preloads) > 0 {
		return fmt.Errorf("could not find injection point for module preloads")
	}

	prog.Methods["main"] = newMain
	return nil
}
