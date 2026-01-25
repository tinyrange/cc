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
	timesliceMMIOMapSize = 0x1000
	configRegionSize     = 4194304 // 4MB (for payload execution buffer)
)

// Timeslice IDs - must match constants in hvf_darwin_arm64.go
// Guest writes these values to timesliceMem offset 0 to record markers
// 0=init_start, 1=phase1_dev_create, 2=phase2_mount_dev, 3=phase2_mount_shm
// 5=phase4_console_open, 6=phase4_setsid, 8=phase4_dup
// 9=phase5_mem_open, 10=phase5_mailbox_map, 11=phase5_ts_map
// 12=phase5_config_map, 13=phase5_anon_map, 14=phase6_time_setup
// 15=phase7_loop_start, 16=phase7_copy_payload, 17=phase7_relocate
// 18=phase7_isb, 19=phase7_call_payload, 20=phase7_payload_done


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
	runtime.LogKmsg("initx: phase4 starting console setup\n")

	consoleFd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/dev/console", runtime.O_RDWR, 0)
	runtime.LogKmsg("initx: phase4 console open done\n")
	if consoleFd < 0 {
		runtime.Printf("initx: failed to open /dev/console (errno=0x%x)\n", 0-consoleFd)
		reboot()
	}

	// create a new session to own the console
	runtime.LogKmsg("initx: phase4 calling setsid\n")
	setsidResult := runtime.Syscall(runtime.SYS_SETSID)
	runtime.LogKmsg("initx: phase4 setsid done\n")
	if setsidResult < 0 {
		runtime.Printf("initx: failed to create session (errno=0x%x)\n", 0-setsidResult)
		reboot()
	}

	// set controlling terminal
	runtime.LogKmsg("initx: phase4 calling TIOCSCTTY\n")
	ttyResult := runtime.Syscall(runtime.SYS_IOCTL, consoleFd, runtime.TIOCSCTTY, 0)
	if ttyResult < 0 {
		if ttyResult != runtime.EPERM {
			runtime.Printf("initx: failed to set controlling TTY (errno=0x%x)\n", 0-ttyResult)
			reboot()
		}
	}

	// dup2 consoleFd to stdin, stdout, stderr
	runtime.LogKmsg("initx: phase4 doing dup3\n")
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 0, 0)
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 1, 0)
	runtime.Syscall(runtime.SYS_DUP3, consoleFd, 2, 0)
	runtime.LogKmsg("initx: phase4 complete\n")

	// === Phase 5: Memory mapping ===
	runtime.LogKmsg("initx: phase5 starting\n")

	// open /dev/mem
	memFd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/dev/mem", runtime.O_RDWR|runtime.O_SYNC, 0)
	runtime.LogKmsg("initx: phase5 /dev/mem opened\n")
	if memFd < 0 {
		runtime.Printf("initx: failed to open /dev/mem (errno=0x%x)\n", 0-memFd)
		reboot()
	}

	// Timeslice MMIO is disabled when TIMESLICE_MMIO_PHYS_ADDR is 0
	// (MMIO handlers were removed as part of vsock migration)
	var timesliceMem int64 = 0
	var timesliceAddr int64 = 0
	timesliceAddr = runtime.Config("TIMESLICE_MMIO_PHYS_ADDR")
	if timesliceAddr > 0 {
		runtime.LogKmsg("initx: phase5 mapping timeslice MMIO\n")
		timesliceMem = runtime.Syscall(runtime.SYS_MMAP, 0, timesliceMMIOMapSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_SHARED, memFd, timesliceAddr)
		runtime.LogKmsg("initx: phase5 timeslice mmap done\n")
		if timesliceMem < 0 {
			// Timeslice mapping is optional - continue without it
			timesliceMem = 0
		}
		// Record: phase5_ts_map (11)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 11)
		}
	}

	// map anonymous region for payload execution (4MB)
	anonMem := runtime.Syscall(runtime.SYS_MMAP, 0, configRegionSize, runtime.PROT_READ|runtime.PROT_WRITE|runtime.PROT_EXEC, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	runtime.LogKmsg("initx: phase5 anonMem mapped\n")
	if anonMem < 0 {
		runtime.Printf("initx: failed to map anonymous payload region (errno=0x%x)\n", 0-anonMem)
		reboot()
	}
	// Record: phase5_anon_map (13)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 13)
	}

	// === Phase 6: Time setup ===
	// Time is now set via vsock message - see vsockMainLoop
	// Record: phase6_time_setup (14)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 14)
	}

	// === Phase 7: Vsock program loading ===
	// Enter vsock main loop - this never returns (reboots on error)
	runtime.LogKmsg("initx: entering vsockMainLoop\n")
	vsockMainLoop(anonMem, timesliceMem)

	// Unreachable - vsockMainLoop never returns
	return 0
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

// vsockMainLoop handles program loading via vsock.
// This function never returns - it either loops forever or reboots on error.
// Protocol:
//   - Host → Guest: [len:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len]
//   - Guest → Host: [len:4][exit_code:4]
func vsockMainLoop(anonMem int64, timesliceMem int64) {
	// Get vsock port from compile-time config (default 9998)
	var vsockPort int64 = 0
	vsockPort = runtime.Config("VSOCK_PORT")
	runtime.LogKmsg("initx: vsockMainLoop starting\n")

	// Create vsock socket
	sockFd := runtime.Syscall(runtime.SYS_SOCKET, runtime.AF_VSOCK, runtime.SOCK_STREAM, 0)
	if sockFd < 0 {
		runtime.Printf("initx: failed to create vsock socket (errno=0x%x)\n", 0-sockFd)
		reboot()
	}
	runtime.LogKmsg("initx: vsock socket created\n")

	// Build sockaddr_vm structure (16 bytes)
	// struct sockaddr_vm {
	//   sa_family_t svm_family;     // AF_VSOCK (2 bytes)
	//   unsigned short svm_reserved1; // 0 (2 bytes)
	//   unsigned int svm_port;      // port (4 bytes)
	//   unsigned int svm_cid;       // CID (4 bytes)
	//   unsigned char svm_flags;    // 0 (1 byte)
	//   unsigned char svm_zero[3];  // 0 (3 bytes)
	// }
	sockaddrMem := runtime.Syscall(runtime.SYS_MMAP, 0, 16, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if sockaddrMem < 0 {
		runtime.Printf("initx: failed to alloc sockaddr_vm (errno=0x%x)\n", 0-sockaddrMem)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Zero the structure
	runtime.Store64(sockaddrMem, 0, 0)
	runtime.Store64(sockaddrMem, 8, 0)

	// Fill in the fields
	runtime.Store16(sockaddrMem, 0, runtime.AF_VSOCK) // svm_family
	// svm_reserved1 is already 0
	runtime.Store32(sockaddrMem, 4, vsockPort)               // svm_port
	runtime.Store32(sockaddrMem, 8, runtime.VMADDR_CID_HOST) // svm_cid = 2 (host)
	// svm_flags and svm_zero are already 0

	// Connect to host
	runtime.LogKmsg("initx: connecting to vsock host...\n")
	connectResult := runtime.Syscall(runtime.SYS_CONNECT, sockFd, sockaddrMem, 16)
	if connectResult < 0 {
		runtime.Printf("initx: failed to connect vsock (errno=0x%x)\n", 0-connectResult)
		runtime.Syscall(runtime.SYS_MUNMAP, sockaddrMem, 16)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}
	runtime.LogKmsg("initx: vsock connected successfully\n")

	// Free sockaddr_vm - no longer needed
	runtime.Syscall(runtime.SYS_MUNMAP, sockaddrMem, 16)

	// Allocate receive buffer for length prefix (4 bytes)
	lenBuf := runtime.Syscall(runtime.SYS_MMAP, 0, 4096, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if lenBuf < 0 {
		runtime.Printf("initx: failed to alloc length buffer (errno=0x%x)\n", 0-lenBuf)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Allocate program receive buffer (4MB)
	progBuf := runtime.Syscall(runtime.SYS_MMAP, 0, configRegionSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if progBuf < 0 {
		runtime.Printf("initx: failed to alloc program buffer (errno=0x%x)\n", 0-progBuf)
		runtime.Syscall(runtime.SYS_MUNMAP, lenBuf, 4)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Allocate result buffer (page size for safety)
	resultBuf := runtime.Syscall(runtime.SYS_MMAP, 0, 4096, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if resultBuf < 0 {
		runtime.Printf("initx: failed to alloc result buffer (errno=0x%x)\n", 0-resultBuf)
		runtime.Syscall(runtime.SYS_MUNMAP, progBuf, configRegionSize)
		runtime.Syscall(runtime.SYS_MUNMAP, lenBuf, 4096)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Pre-fill result buffer length field (always 4)
	runtime.Store32(resultBuf, 0, 4)

	// Main vsock loop
	for {
		// Record: phase7_loop_start (15)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 15)
		}

		// Read 4-byte length prefix
		readLen := vsockReadFull(sockFd, lenBuf, 4)
		if readLen < 0 {
			runtime.Printf("initx: vsock read length failed (errno=0x%x)\n", 0-readLen)
			reboot()
		}

		var payloadLen int64 = 0
		payloadLen = runtime.Load32(lenBuf, 0)
		if payloadLen <= 0 {
			runtime.Printf("initx: invalid payload length: 0x%x\n", payloadLen)
			reboot()
		}
		if payloadLen > configRegionSize {
			runtime.Printf("initx: payload length too large: 0x%x\n", payloadLen)
			reboot()
		}

		// Read payload: first do a test read, then read the rest
		// The test read helps verify the connection is working before committing to a large read
		testRead := runtime.Syscall(runtime.SYS_READ, sockFd, progBuf, 4)
		if testRead < 0 {
			runtime.Printf("initx: test read failed (errno=0x%x)\n", 0-testRead)
			reboot()
		}

		// Read remaining payload
		var remainingLen int64 = 0
		remainingLen = payloadLen - 4
		if remainingLen < 0 {
			remainingLen = 0
		}
		readPayload := vsockReadFull(sockFd, progBuf+4, remainingLen)
		if readPayload < 0 {
			runtime.Printf("initx: vsock read payload failed (errno=0x%x)\n", 0-readPayload)
			reboot()
		}

		// Parse header: time_sec(8) + time_nsec(8) + code_len(4) + reloc_count(4)
		// First, set the system clock from the host time
		var timeSec int64 = 0
		var timeNsec int64 = 0
		timeSec = runtime.Load64(progBuf, 0)
		timeNsec = runtime.Load64(progBuf, 8)

		// Allocate timespec and call clock_settime
		timespecMem := runtime.Syscall(runtime.SYS_MMAP, 0, 16, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
		if timespecMem >= 0 {
			runtime.Store64(timespecMem, 0, timeSec)
			runtime.Store64(timespecMem, 8, timeNsec)
			clockSetResult := runtime.Syscall(runtime.SYS_CLOCK_SETTIME, runtime.CLOCK_REALTIME, timespecMem)
			if clockSetResult < 0 {
				runtime.Printf("initx: clock_settime failed (errno=0x%x)\n", 0-clockSetResult)
			}
			runtime.Syscall(runtime.SYS_MUNMAP, timespecMem, 16)
		}

		var codeLen int64 = 0
		var relocCount int64 = 0
		codeLen = runtime.Load32(progBuf, 16)
		relocCount = runtime.Load32(progBuf, 20)

		// Calculate offsets
		var relocBytes int64 = 0
		var codeOffset int64 = 0
		relocBytes = relocCount << 2
		codeOffset = 24 + relocBytes // 24 = time_sec(8) + time_nsec(8) + code_len(4) + reloc_count(4)

		// Copy code to anonMem
		var copySrc int64 = 0
		var copyDst int64 = 0
		var remaining int64 = 0
		copySrc = progBuf + codeOffset
		copyDst = anonMem
		remaining = codeLen

		// Copy 4 bytes at a time
		for remaining >= 4 {
			var copyVal32 int64 = 0
			copyVal32 = runtime.Load32(copySrc, 0)
			runtime.Store32(copyDst, 0, copyVal32)
			copyDst = copyDst + 4
			copySrc = copySrc + 4
			remaining = remaining - 4
		}

		// Copy remaining bytes
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

		// Apply relocations
		var relocPtr int64 = 0
		var relocIndex int64 = 0
		relocPtr = progBuf + 24 // relocations start at offset 24 (after time_sec, time_nsec, code_len, reloc_count)
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

		// Instruction synchronization barrier
		runtime.ISB()

		// Record: phase7_isb (18)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 18)
		}

		// Record: phase7_call_payload (19)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 19)
		}

		// Call the payload
		payloadResult := runtime.Call(anonMem)

		// Record: phase7_payload_done (20)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 20)
		}

		// Send result back: [len=4][exit_code]
		runtime.Store32(resultBuf, 4, payloadResult)

		// Try a direct write syscall to debug
		var directWrite int64 = 0
		directWrite = runtime.Syscall(runtime.SYS_WRITE, sockFd, resultBuf, 8)
		if directWrite < 0 {
			runtime.Printf("initx: vsock write result failed (errno=0x%x)\n", 0-directWrite)
			reboot()
		}
	}
}

// vsockReadFull reads exactly n bytes from fd into buf.
// Returns 0 on success, negative errno on error.
func vsockReadFull(fd int64, buf int64, n int64) int64 {
	var totalRead int64 = 0
	for totalRead < n {
		var readResult int64 = 0
		readResult = runtime.Syscall(runtime.SYS_READ, fd, buf+totalRead, n-totalRead)
		if readResult < 0 {
			return readResult
		}
		if readResult == 0 {
			// EOF - connection closed
			return runtime.EPIPE
		}
		totalRead = totalRead + readResult
	}
	return 0
}

// vsockWriteFull writes exactly n bytes from buf to fd.
// Returns 0 on success, negative errno on error.
func vsockWriteFull(fd int64, buf int64, n int64) int64 {
	var totalWritten int64 = 0
	for totalWritten < n {
		var writeResult int64 = 0
		writeResult = runtime.Syscall(runtime.SYS_WRITE, fd, buf+totalWritten, n-totalWritten)
		if writeResult < 0 {
			return writeResult
		}
		if writeResult == 0 {
			return runtime.EPIPE
		}
		totalWritten = totalWritten + writeResult
	}
	return 0
}
