//go:build ignore

// Package main is the RTG source for the init program.
// This file is compiled by the RTG compiler at build time, not the Go compiler.
// The //go:build ignore tag excludes it from normal compilation.
// It imports the rtg/runtime stub package for IDE completion and type checking.
package main

import "github.com/tinyrange/cc/internal/rtg/runtime"

// Device configuration
const (
	devMemMode     = runtime.S_IFCHR | 0o600
	devMemDeviceID = (1 << 8) | 1 // Mkdev(1, 1)
)

// Memory layout
const (
	mailboxMapSize         = 0x1000
	configRegionSize       = 4194304 // 4MB
	mailboxPhysAddr        = 0xf0000000
	timesliceMMIOPhysAddr  = 0xf0001000
	timesliceMMIOMapSize   = 0x1000
	configRegionPhysAddr   = 0xf0003000
)

// Timeslice IDs - must match constants in hvf_darwin_arm64.go
// Guest writes these values to timesliceMem offset 0 to record markers
// 0=init_start, 1=phase1_dev_create, 2=phase2_mount_dev, 3=phase2_mount_shm
// 5=phase4_console_open, 6=phase4_setsid, 8=phase4_dup
// 9=phase5_mem_open, 10=phase5_mailbox_map, 11=phase5_ts_map
// 12=phase5_config_map, 13=phase5_anon_map, 14=phase6_time_setup
// 15=phase7_loop_start, 16=phase7_copy_payload, 17=phase7_relocate
// 18=phase7_isb, 19=phase7_call_payload, 20=phase7_payload_done

// Config region offsets
const (
	configTimeSecField           = 24
	configTimeNsecField          = 32
	configHeaderMagicValue       = 0xcafebabe
	configHeaderSize             = 40
	mailboxRunResultDetailOffset = 8
)

// main is the entrypoint for the init program.
// Note: The RTG compiler uses the first function as the entrypoint,
// so main must be declared before reboot.
func main() int64 {
	// === Phase 1: Create /dev directory and device nodes ===

	// ensure /dev exists
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/dev", 0755)

	// create /dev/mem device node
	runtime.Syscall(runtime.SYS_MKNODAT, runtime.AT_FDCWD, "/dev/mem", devMemMode, devMemDeviceID)

	// === Phase 2: Mount filesystems ===

	// mount devtmpfs on /dev
	devMountErr := runtime.Syscall(runtime.SYS_MOUNT, "devtmpfs", "/dev", "devtmpfs", 0, "")
	if devMountErr < 0 {
		if devMountErr != runtime.EBUSY {
			runtime.Printf("failed to mount devtmpfs on /dev: 0x%x\n", 0-devMountErr)
			reboot()
		}
	}

	// create /dev/shm for POSIX shared memory
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/dev/shm", 0o1777)
	runtime.Syscall(runtime.SYS_MOUNT, "tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777")

	// === Phase 3: Module preloading placeholder ===
	// Module preloading is injected here at IR level

	// === Phase 4: Console setup ===

	consoleFd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/dev/console", runtime.O_RDWR, 0)
	if consoleFd < 0 {
		runtime.Printf("initx: failed to open /dev/console (errno=0x%x)\n", 0-consoleFd)
		reboot()
	}

	// create a new session to own the console
	setsidResult := runtime.Syscall(runtime.SYS_SETSID)
	if setsidResult < 0 {
		runtime.Printf("initx: failed to create session (errno=0x%x)\n", 0-setsidResult)
		reboot()
	}

	// set controlling terminal
	ttyResult := runtime.Syscall(runtime.SYS_IOCTL, consoleFd, runtime.TIOCSCTTY, 0)
	if ttyResult < 0 {
		if ttyResult != runtime.EPERM {
			runtime.Printf("initx: failed to set controlling TTY (errno=0x%x)\n", 0-ttyResult)
			reboot()
		}
	}

	// dup2 consoleFd to stdin, stdout, stderr
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 0, 0)
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 1, 0)
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 2, 0)

	// === Phase 5: Memory mapping ===

	// open /dev/mem
	memFd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/dev/mem", runtime.O_RDWR|runtime.O_SYNC, 0)
	if memFd < 0 {
		runtime.Printf("initx: failed to open /dev/mem (errno=0x%x)\n", 0-memFd)
		reboot()
	}

	// map mailbox region
	mailboxMem := runtime.Syscall(runtime.SYS_MMAP, 0, mailboxMapSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_SHARED, memFd, mailboxPhysAddr)
	if mailboxMem < 0 {
		runtime.Printf("initx: failed to map mailbox region (errno=0x%x)\n", 0-mailboxMem)
		reboot()
	}

	// map timeslice MMIO region for guest-side timeslice recording
	timesliceMem := runtime.Syscall(runtime.SYS_MMAP, 0, timesliceMMIOMapSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_SHARED, memFd, timesliceMMIOPhysAddr)
	if timesliceMem < 0 {
		// Timeslice mapping is optional - continue without it
		timesliceMem = 0
	}
	// Record: phase5_ts_map (11)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 11)
	}

	// map config region (4MB)
	configMem := runtime.Syscall(runtime.SYS_MMAP, 0, configRegionSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_SHARED, memFd, configRegionPhysAddr)
	if configMem < 0 {
		runtime.Printf("initx: failed to map config region (errno=0x%x)\n", 0-configMem)
		reboot()
	}
	// Record: phase5_config_map (12)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 12)
	}

	// map anonymous region for payload execution (4MB)
	anonMem := runtime.Syscall(runtime.SYS_MMAP, 0, configRegionSize, runtime.PROT_READ|runtime.PROT_WRITE|runtime.PROT_EXEC, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if anonMem < 0 {
		runtime.Printf("initx: failed to map anonymous payload region (errno=0x%x)\n", 0-anonMem)
		reboot()
	}
	// Record: phase5_anon_map (13)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 13)
	}

	// === Phase 6: Time setup ===

	// allocate timespec buffer
	timespecMem := runtime.Syscall(runtime.SYS_MMAP, 0, 16, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if timespecMem >= 0 {
		// read time from config region and store in timespec struct
		var timeSec int64 = 0
		var timeNsec int64 = 0
		timeSec = runtime.Load64(configMem, configTimeSecField)
		timeNsec = runtime.Load64(configMem, configTimeNsecField)
		runtime.Store64(timespecMem, 0, timeSec)
		runtime.Store64(timespecMem, 8, timeNsec)

		// call clock_settime
		clockSetResult := runtime.Syscall(runtime.SYS_CLOCK_SETTIME, runtime.CLOCK_REALTIME, timespecMem)
		if clockSetResult < 0 {
			runtime.Printf("initx: clock_settime failed (errno=0x%x), continuing anyway\n", 0-clockSetResult)
		}

		// free timespec buffer
		runtime.Syscall(runtime.SYS_MUNMAP, timespecMem, 16)
	}
	// Record: phase6_time_setup (14)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 14)
	}

	// === Phase 7: Main loop ===

	for {
		// Record: phase7_loop_start (15)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 15)
		}
		// check for magic value
		var configMagic int64 = 0
		configMagic = runtime.Load32(configMem, 0)
		if configMagic == configHeaderMagicValue {
			// load payload header
			var codeLen int64 = 0
			var relocCount int64 = 0
			var relocBytes int64 = 0
			var codeOffset int64 = 0

			codeLen = runtime.Load32(configMem, 4)
			relocCount = runtime.Load32(configMem, 8)
			relocBytes = relocCount << 2
			codeOffset = configHeaderSize + relocBytes

			// copy binary payload
			var copySrc int64 = 0
			var copyDst int64 = 0
			var remaining int64 = 0

			copySrc = configMem + codeOffset
			copyDst = anonMem
			remaining = codeLen

			// copy 4 bytes at a time
			for remaining >= 4 {
				var copyVal32 int64 = 0
				copyVal32 = runtime.Load32(copySrc, 0)
				runtime.Store32(copyDst, 0, copyVal32)
				copyDst = copyDst + 4
				copySrc = copySrc + 4
				remaining = remaining - 4
			}

			// copy remaining bytes
			for remaining > 0 {
				var copyVal8 int64 = 0
				copyVal8 = runtime.Load8(copySrc, 0)
				runtime.Store8(copyDst, 0, copyVal8)
				copyDst = copyDst + 1
				copySrc = copySrc + 1
				remaining = remaining - 1
			}
			// Record: phase7_copy_payload (16)
			if timesliceMem > 0 {
				runtime.Store32(timesliceMem, 0, 16)
			}

			// apply relocations
			var relocPtr int64 = 0
			var relocIndex int64 = 0

			relocPtr = configMem + configHeaderSize
			relocIndex = 0

			for relocIndex < relocCount {
				var relocEntryPtr int64 = 0
				var relocOffset int64 = 0
				var patchPtr int64 = 0
				var patchValue int64 = 0

				relocEntryPtr = relocPtr + (relocIndex << 2)
				relocOffset = runtime.Load32(relocEntryPtr, 0)
				patchPtr = anonMem + relocOffset
				patchValue = runtime.Load64(patchPtr, 0)
				patchValue = patchValue + anonMem
				runtime.Store64(patchPtr, 0, patchValue)
				relocIndex = relocIndex + 1
			}
			// Record: phase7_relocate (17)
			if timesliceMem > 0 {
				runtime.Store32(timesliceMem, 0, 17)
			}

			// Instruction synchronization barrier - required on ARM64 after modifying
			// code in memory before executing it. Without this, the instruction cache
			// may contain stale data and cause SIGILL.
			runtime.ISB()
			// Record: phase7_isb (18)
			if timesliceMem > 0 {
				runtime.Store32(timesliceMem, 0, 18)
			}

			// Record: phase7_call_payload (19)
			if timesliceMem > 0 {
				runtime.Store32(timesliceMem, 0, 19)
			}
			// call the payload
			payloadResult := runtime.Call(anonMem)
			// Record: phase7_payload_done (20)
			if timesliceMem > 0 {
				runtime.Store32(timesliceMem, 0, 20)
			}

			// publish return code for host-side exit propagation
			runtime.Store32(mailboxMem, mailboxRunResultDetailOffset, payloadResult)

			// signal completion to host
			var doneSignal int64 = 0x444f4e45
			runtime.Store32(mailboxMem, 0, doneSignal)
		} else {
			// magic value not found
			var actualMagic int64 = 0
			actualMagic = runtime.Load32(configMem, 0)
			runtime.Printf("Magic value not found in config region: %x\n", actualMagic)
			reboot()
		}
	}
}

// reboot issues a reboot syscall with the architecture-appropriate command.
// On x86_64, it uses RESTART; on ARM64, it uses POWER_OFF.
func reboot() {
	if runtime.GOARCH == "amd64" {
		runtime.Syscall(runtime.SYS_REBOOT,
			runtime.LINUX_REBOOT_MAGIC1,
			runtime.LINUX_REBOOT_MAGIC2,
			runtime.LINUX_REBOOT_CMD_RESTART, 0)
	} else {
		runtime.Syscall(runtime.SYS_REBOOT,
			runtime.LINUX_REBOOT_MAGIC1,
			runtime.LINUX_REBOOT_MAGIC2,
			runtime.LINUX_REBOOT_CMD_POWER_OFF, 0)
	}
}
