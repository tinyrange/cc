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
	configRegionSize     = 4194304  // 4MB (for payload execution buffer)
	captureBufferSize    = 16777216 // 16MB max per capture stream
)

// Capture flags (must match loader.go)
const (
	captureFlagNone    = 0x00
	captureFlagStdout  = 0x01
	captureFlagStderr  = 0x02
	captureFlagCombine = 0x04
	captureFlagStdin   = 0x08
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

	// mount /proc for /proc/self/fd access
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/proc", 0755)
	procMountErr := runtime.Syscall(runtime.SYS_MOUNT, "proc", "/proc", "proc", 0, "")
	if procMountErr < 0 {
		if procMountErr != runtime.EBUSY {
			runtime.Printf("failed to mount proc on /proc: 0x%x\n", 0-procMountErr)
		}
	}

	// create /dev/stdin, /dev/stdout, /dev/stderr symlinks
	runtime.Syscall(runtime.SYS_SYMLINKAT, "/proc/self/fd/0", runtime.AT_FDCWD, "/dev/stdin")
	runtime.Syscall(runtime.SYS_SYMLINKAT, "/proc/self/fd/1", runtime.AT_FDCWD, "/dev/stdout")
	runtime.Syscall(runtime.SYS_SYMLINKAT, "/proc/self/fd/2", runtime.AT_FDCWD, "/dev/stderr")

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
//   - Host → Guest: [len:4][time_sec:8][time_nsec:8][flags:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len]
//   - Guest → Host: [len:4][exit_code:4][stdout_len:4][stdout_data][stderr_len:4][stderr_data]
//
// Note: time_sec and time_nsec come before flags to maintain 8-byte alignment for ARM64.
// When flags=0, response is just [len:4][exit_code:4] for backward compatibility.
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

	// Allocate larger result buffer for captured output (16MB + header space)
	// Format: [len:4][exit_code:4][stdout_len:4][stdout_data][stderr_len:4][stderr_data]
	resultBufSize := captureBufferSize + captureBufferSize + 64 // stdout + stderr + headers
	resultBuf := runtime.Syscall(runtime.SYS_MMAP, 0, resultBufSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if resultBuf < 0 {
		runtime.Printf("initx: failed to alloc result buffer (errno=0x%x)\n", 0-resultBuf)
		runtime.Syscall(runtime.SYS_MUNMAP, progBuf, configRegionSize)
		runtime.Syscall(runtime.SYS_MUNMAP, lenBuf, 4096)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Allocate pipe fd array (2 ints = 8 bytes, but allocate a page for safety)
	pipeFdBuf := runtime.Syscall(runtime.SYS_MMAP, 0, 4096, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if pipeFdBuf < 0 {
		runtime.Printf("initx: failed to alloc pipe fd buffer (errno=0x%x)\n", 0-pipeFdBuf)
		runtime.Syscall(runtime.SYS_MUNMAP, resultBuf, resultBufSize)
		runtime.Syscall(runtime.SYS_MUNMAP, progBuf, configRegionSize)
		runtime.Syscall(runtime.SYS_MUNMAP, lenBuf, 4096)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Allocate reader buffer for concurrent capture (stdout + stderr + overhead)
	readerBufSize := captureBufferSize + captureBufferSize + 64
	readerBuf := runtime.Syscall(runtime.SYS_MMAP, 0, readerBufSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if readerBuf < 0 {
		runtime.Printf("initx: failed to alloc reader buffer (errno=0x%x)\n", 0-readerBuf)
		runtime.Syscall(runtime.SYS_MUNMAP, pipeFdBuf, 4096)
		runtime.Syscall(runtime.SYS_MUNMAP, resultBuf, resultBufSize)
		runtime.Syscall(runtime.SYS_MUNMAP, progBuf, configRegionSize)
		runtime.Syscall(runtime.SYS_MUNMAP, lenBuf, 4096)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

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

		// Parse header: time_sec(8) + time_nsec(8) + flags(4) + stdin_len(4) + code_len(4) + reloc_count(4)
		// time_sec and time_nsec come first for ARM64 8-byte alignment
		var flags int64 = 0
		var timeSec int64 = 0
		var timeNsec int64 = 0
		var stdinLen int64 = 0
		timeSec = runtime.Load64(progBuf, 0)
		timeNsec = runtime.Load64(progBuf, 8)
		flags = runtime.Load32(progBuf, 16)
		stdinLen = runtime.Load32(progBuf, 20) // offset 20 = time_sec(8) + time_nsec(8) + flags(4)

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
		codeLen = runtime.Load32(progBuf, 24)    // offset 24 = time_sec(8) + time_nsec(8) + flags(4) + stdin_len(4)
		relocCount = runtime.Load32(progBuf, 28) // offset 28 = above + code_len(4)

		// Calculate offsets
		var relocBytes int64 = 0
		var codeOffset int64 = 0
		var stdinOffset int64 = 0
		relocBytes = relocCount << 2
		codeOffset = 32 + relocBytes       // 32 = time_sec(8) + time_nsec(8) + flags(4) + stdin_len(4) + code_len(4) + reloc_count(4)
		stdinOffset = codeOffset + codeLen // stdin data follows code

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
		relocPtr = progBuf + 32 // relocations start at offset 32 (after time_sec, time_nsec, flags, stdin_len, code_len, reloc_count)
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

		// Flush caches for self-modifying code (required on ARM64)
		// This performs DC CVAU + DSB ISH + IC IVAU + DSB ISH + ISB
		runtime.CacheFlush(anonMem, codeLen)

		// Record: phase7_isb (18)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 18)
		}

		// Set up stdin pipe if stdin data is present
		var stdinPipeRead int64 = -1
		var stdinPipeWrite int64 = -1
		var stdinWriterPid int64 = -1
		var savedStdin int64 = -1

		var hasStdinFlag int64 = 0
		hasStdinFlag = flags & captureFlagStdin
		if hasStdinFlag != 0 {
			// Create pipe for stdin (even if empty, to signal EOF)
			stdinPipeResult := runtime.Syscall(runtime.SYS_PIPE2, pipeFdBuf, 0)
			if stdinPipeResult >= 0 {
				stdinPipeRead = runtime.Load32(pipeFdBuf, 0)
				stdinPipeWrite = runtime.Load32(pipeFdBuf, 4)

				// Fork writer process to write stdin data to pipe
				// This avoids blocking on >64KB stdin (pipe buffer limit)
				stdinWriterPid = runtime.Syscall(runtime.SYS_CLONE, runtime.SIGCHLD, 0, 0, 0, 0)

				if stdinWriterPid == 0 {
					// === STDIN WRITER PROCESS (child) ===
					// Close read end
					runtime.Syscall(runtime.SYS_CLOSE, stdinPipeRead)

					// Write stdin data to pipe (if any)
					if stdinLen > 0 {
						var stdinDataPtr int64 = 0
						stdinDataPtr = progBuf + stdinOffset
						vsockWriteFull(stdinPipeWrite, stdinDataPtr, stdinLen)
					}

					// Close write end (signals EOF to reader)
					runtime.Syscall(runtime.SYS_CLOSE, stdinPipeWrite)

					// Exit writer process
					runtime.Syscall(runtime.SYS_EXIT, 0)
				}

				// === PARENT PROCESS ===
				// Close write end (child will write to it)
				runtime.Syscall(runtime.SYS_CLOSE, stdinPipeWrite)
				stdinPipeWrite = -1

				// Save original stdin and redirect to pipe read end
				savedStdin = runtime.Syscall(runtime.SYS_FCNTL, 0, runtime.F_DUPFD_CLOEXEC, 10)
				runtime.Syscall(runtime.SYS_DUP3, stdinPipeRead, 0, 0)
				runtime.Syscall(runtime.SYS_CLOSE, stdinPipeRead)
				stdinPipeRead = -1
			}
		}

		// Set up capture if flags are set
		var captureStdout int64 = 0
		var captureStderr int64 = 0
		var stdoutPipeRead int64 = -1
		var stdoutPipeWrite int64 = -1
		var stderrPipeRead int64 = -1
		var stderrPipeWrite int64 = -1
		var returnPipeRead int64 = -1
		var returnPipeWrite int64 = -1
		var savedStdout int64 = -1
		var savedStderr int64 = -1
		var readerPid int64 = -1

		captureStdout = flags & captureFlagStdout
		captureStderr = flags & captureFlagStderr

		// Check for combined mode flag separately
		var combineMode int64 = 0
		combineMode = flags & captureFlagCombine

		// Captured output lengths (will be read from return pipe)
		var stdoutLen int64 = 0
		var stderrLen int64 = 0

		// For capture mode, we use a concurrent reader process to avoid deadlock
		// when payload output exceeds the pipe buffer (64KB on Linux).
		// The reader process drains stdout/stderr pipes while the payload runs,
		// then sends captured data back via a return pipe.
		var needCapture int64 = 0
		if captureStdout != 0 {
			needCapture = 1
		}
		if captureStderr != 0 {
			needCapture = 1
		}

		if needCapture != 0 {
			// Create stdout pipe
			var stdoutPipeResult int64 = 0
			if captureStdout != 0 {
				stdoutPipeResult = runtime.Syscall(runtime.SYS_PIPE2, pipeFdBuf, 0)
				if stdoutPipeResult >= 0 {
					stdoutPipeRead = runtime.Load32(pipeFdBuf, 0)
					stdoutPipeWrite = runtime.Load32(pipeFdBuf, 4)
				}
			}

			// Create stderr pipe (only if separate capture, not combined mode)
			var stderrPipeResult int64 = 0
			// Check conditions explicitly
			var shouldCreateStderrPipe int64 = 0
			if captureStderr != 0 {
				if combineMode == 0 {
					shouldCreateStderrPipe = 1
				}
			}
			if shouldCreateStderrPipe != 0 {
				stderrPipeResult = runtime.Syscall(runtime.SYS_PIPE2, pipeFdBuf, 0)
				if stderrPipeResult >= 0 {
					stderrPipeRead = runtime.Load32(pipeFdBuf, 0)
					stderrPipeWrite = runtime.Load32(pipeFdBuf, 4)
				}
			}

			// Create return pipe for reader→parent data transfer
			returnPipeResult := runtime.Syscall(runtime.SYS_PIPE2, pipeFdBuf, 0)
			if returnPipeResult >= 0 {
				returnPipeRead = runtime.Load32(pipeFdBuf, 0)
				returnPipeWrite = runtime.Load32(pipeFdBuf, 4)
			}

			// Fork the reader process
			// clone(SIGCHLD, 0, 0, 0, 0) - creates a new process without shared memory
			readerPid = runtime.Syscall(runtime.SYS_CLONE, runtime.SIGCHLD, 0, 0, 0, 0)

			if readerPid == 0 {
				// === READER PROCESS (child) ===
				// Close pipe ends we don't need
				if stdoutPipeWrite >= 0 {
					runtime.Syscall(runtime.SYS_CLOSE, stdoutPipeWrite)
				}
				if stderrPipeWrite >= 0 {
					runtime.Syscall(runtime.SYS_CLOSE, stderrPipeWrite)
				}
				if returnPipeRead >= 0 {
					runtime.Syscall(runtime.SYS_CLOSE, returnPipeRead)
				}

				// Set pipes to non-blocking for polling
				var pipeFlags int64 = 0
				if stdoutPipeRead >= 0 {
					pipeFlags = runtime.Syscall(runtime.SYS_FCNTL, stdoutPipeRead, runtime.F_GETFL, 0)
					if pipeFlags >= 0 {
						runtime.Syscall(runtime.SYS_FCNTL, stdoutPipeRead, runtime.F_SETFL, pipeFlags|runtime.O_NONBLOCK)
					}
				}
				if stderrPipeRead >= 0 {
					pipeFlags = runtime.Syscall(runtime.SYS_FCNTL, stderrPipeRead, runtime.F_GETFL, 0)
					if pipeFlags >= 0 {
						runtime.Syscall(runtime.SYS_FCNTL, stderrPipeRead, runtime.F_SETFL, pipeFlags|runtime.O_NONBLOCK)
					}
				}

				// Read from pipes into readerBuf
				// Layout: [stdout_data...][stderr_data...]
				var readerStdoutLen int64 = 0
				var readerStderrLen int64 = 0
				var stdoutEof int64 = 0
				var stderrEof int64 = 0

				// Compute stderr buffer base pointer (once, before loop)
				// stderr data starts at readerBuf + captureBufferSize
				var stderrBufBase int64 = 0
				stderrBufBase = readerBuf + captureBufferSize

				// If no stdout pipe, mark as EOF
				if stdoutPipeRead < 0 {
					stdoutEof = 1
				}
				// If no stderr pipe, mark as EOF
				if stderrPipeRead < 0 {
					stderrEof = 1
				}

				// Read loop - drain both pipes until EOF on both
				var bothEof int64 = 0
				var readResult int64 = 0
				var gotDataThisIter int64 = 0
				for bothEof == 0 {
					gotDataThisIter = 0

					// Try to read from stdout pipe
					if stdoutEof == 0 {
						if readerStdoutLen < captureBufferSize {
							readResult = runtime.Syscall(runtime.SYS_READ, stdoutPipeRead, readerBuf+readerStdoutLen, captureBufferSize-readerStdoutLen)
							if readResult > 0 {
								readerStdoutLen = readerStdoutLen + readResult
								gotDataThisIter = 1
							} else {
								if readResult == 0 {
									// EOF
									stdoutEof = 1
								} else {
									// EAGAIN (-11) means no data available, other errors mark EOF
									if readResult != -11 {
										stdoutEof = 1
									}
								}
							}
						} else {
							// Buffer full, mark EOF to stop trying
							stdoutEof = 1
						}
					}

					// Try to read from stderr pipe
					if stderrEof == 0 {
						if readerStderrLen < captureBufferSize {
							readResult = runtime.Syscall(runtime.SYS_READ, stderrPipeRead, stderrBufBase+readerStderrLen, captureBufferSize-readerStderrLen)
							if readResult > 0 {
								readerStderrLen = readerStderrLen + readResult
								gotDataThisIter = 1
							} else {
								if readResult == 0 {
									// EOF
									stderrEof = 1
								} else {
									// EAGAIN (-11) means no data available, other errors mark EOF
									if readResult != -11 {
										stderrEof = 1
									}
								}
							}
						} else {
							// Buffer full, mark EOF to stop trying
							stderrEof = 1
						}
					}

					// Check if both pipes are at EOF
					if stdoutEof != 0 {
						if stderrEof != 0 {
							bothEof = 1
						}
					}

					// If no data was read this iteration and not done, yield to scheduler
					// This prevents starving the parent process on single-vCPU VMs
					if gotDataThisIter == 0 {
						if bothEof == 0 {
							// Use getpid() as a cheap syscall to yield to scheduler
							runtime.Syscall(runtime.SYS_GETPID)
						}
					}
				}

				// Close read ends
				if stdoutPipeRead >= 0 {
					runtime.Syscall(runtime.SYS_CLOSE, stdoutPipeRead)
				}
				if stderrPipeRead >= 0 {
					runtime.Syscall(runtime.SYS_CLOSE, stderrPipeRead)
				}

				// Write captured data to return pipe
				// Format: [stdout_len:4][stdout_data][stderr_len:4][stderr_data]
				// Use a small header buffer at end of readerBuf
				var headerPtr int64 = 0
				headerPtr = readerBuf + captureBufferSize + captureBufferSize

				// Write stdout_len
				runtime.Store32(headerPtr, 0, readerStdoutLen)
				runtime.Syscall(runtime.SYS_WRITE, returnPipeWrite, headerPtr, 4)

				// Write stdout_data
				if readerStdoutLen > 0 {
					vsockWriteFull(returnPipeWrite, readerBuf, readerStdoutLen)
				}

				// Write stderr_len
				runtime.Store32(headerPtr, 0, readerStderrLen)
				runtime.Syscall(runtime.SYS_WRITE, returnPipeWrite, headerPtr, 4)

				// Write stderr_data
				var stderrDataPtr int64 = 0
				stderrDataPtr = readerBuf + captureBufferSize
				if readerStderrLen > 0 {
					vsockWriteFull(returnPipeWrite, stderrDataPtr, readerStderrLen)
				}

				// Close return pipe and exit
				runtime.Syscall(runtime.SYS_CLOSE, returnPipeWrite)
				runtime.Syscall(runtime.SYS_EXIT, 0)
			}

			// === PARENT PROCESS ===
			// Close pipe ends we don't need
			if stdoutPipeRead >= 0 {
				runtime.Syscall(runtime.SYS_CLOSE, stdoutPipeRead)
				stdoutPipeRead = -1
			}
			if stderrPipeRead >= 0 {
				runtime.Syscall(runtime.SYS_CLOSE, stderrPipeRead)
				stderrPipeRead = -1
			}
			if returnPipeWrite >= 0 {
				runtime.Syscall(runtime.SYS_CLOSE, returnPipeWrite)
				returnPipeWrite = -1
			}

			// Save original stdout/stderr and redirect to pipes
			if captureStdout != 0 {
				if stdoutPipeWrite >= 0 {
					savedStdout = runtime.Syscall(runtime.SYS_FCNTL, 1, runtime.F_DUPFD_CLOEXEC, 10)
					runtime.Syscall(runtime.SYS_DUP3, stdoutPipeWrite, 1, 0)
					runtime.Syscall(runtime.SYS_CLOSE, stdoutPipeWrite)
					stdoutPipeWrite = -1
				}
			}

			// Handle combined mode: redirect stderr to stdout
			if combineMode != 0 {
				if savedStdout >= 0 {
					savedStderr = runtime.Syscall(runtime.SYS_FCNTL, 2, runtime.F_DUPFD_CLOEXEC, 10)
					runtime.Syscall(runtime.SYS_DUP3, 1, 2, 0)
				}
			}

			// Redirect stderr to its own pipe (if separate capture)
			// NOTE: Use flattened conditionals to work around RTG compiler bug
			// with nested if statements (the inner block runs unconditionally)
			var shouldRedirectStderr int64 = 0
			if captureStderr != 0 {
				shouldRedirectStderr = 1
			}
			if combineMode != 0 {
				shouldRedirectStderr = 0
			}
			if stderrPipeWrite < 0 {
				shouldRedirectStderr = 0
			}
			if shouldRedirectStderr != 0 {
				savedStderr = runtime.Syscall(runtime.SYS_FCNTL, 2, runtime.F_DUPFD_CLOEXEC, 10)
				runtime.Syscall(runtime.SYS_DUP3, stderrPipeWrite, 2, 0)
				runtime.Syscall(runtime.SYS_CLOSE, stderrPipeWrite)
				stderrPipeWrite = -1
			}
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

		// Restore stdin and wait for stdin writer process
		if savedStdin >= 0 {
			runtime.Syscall(runtime.SYS_DUP3, savedStdin, 0, 0)
			runtime.Syscall(runtime.SYS_CLOSE, savedStdin)
			savedStdin = -1
		}
		if stdinWriterPid > 0 {
			// Wait for stdin writer process to complete
			stdinStatusBuf := runtime.Syscall(runtime.SYS_MMAP, 0, 4096, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
			if stdinStatusBuf >= 0 {
				runtime.Syscall(runtime.SYS_WAIT4, stdinWriterPid, stdinStatusBuf, 0, 0)
				runtime.Syscall(runtime.SYS_MUNMAP, stdinStatusBuf, 4096)
			}
			stdinWriterPid = -1
		}

		// Restore stdout/stderr and wait for reader
		if needCapture != 0 {
			// Restore stdout (closes pipe write end, signals EOF to reader)
			if savedStdout >= 0 {
				runtime.Syscall(runtime.SYS_DUP3, savedStdout, 1, 0)
				runtime.Syscall(runtime.SYS_CLOSE, savedStdout)
				savedStdout = -1
			}

			// Restore stderr
			if savedStderr >= 0 {
				runtime.Syscall(runtime.SYS_DUP3, savedStderr, 2, 0)
				runtime.Syscall(runtime.SYS_CLOSE, savedStderr)
				savedStderr = -1
			}

			// Read captured data from return pipe BEFORE waiting for reader
			// This prevents deadlock when captured data exceeds pipe buffer (64KB)
			// Format: [stdout_len:4][stdout_data][stderr_len:4][stderr_data]
			if returnPipeRead >= 0 {
				// Read stdout_len
				vsockReadFull(returnPipeRead, resultBuf, 4)
				stdoutLen = runtime.Load32(resultBuf, 0)
				if stdoutLen < 0 {
					stdoutLen = 0
				}
				if stdoutLen > captureBufferSize {
					stdoutLen = captureBufferSize
				}

				// Read stdout_data into result buffer at offset 12
				if stdoutLen > 0 {
					vsockReadFull(returnPipeRead, resultBuf+12, stdoutLen)
				}

				// Read stderr_len
				vsockReadFull(returnPipeRead, resultBuf, 4)
				stderrLen = runtime.Load32(resultBuf, 0)
				if stderrLen < 0 {
					stderrLen = 0
				}
				if stderrLen > captureBufferSize {
					stderrLen = captureBufferSize
				}

				// Read stderr_data into result buffer after stdout
				if stderrLen > 0 {
					var stderrDataOffset int64 = 0
					stderrDataOffset = 12 + stdoutLen + 4
					vsockReadFull(returnPipeRead, resultBuf+stderrDataOffset, stderrLen)
				}

				runtime.Syscall(runtime.SYS_CLOSE, returnPipeRead)
				returnPipeRead = -1
			}

			// Wait for reader process to complete (after reading all data)
			if readerPid > 0 {
				// Allocate status buffer for wait4
				statusBuf := runtime.Syscall(runtime.SYS_MMAP, 0, 4096, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
				if statusBuf >= 0 {
					runtime.Syscall(runtime.SYS_WAIT4, readerPid, statusBuf, 0, 0)
					runtime.Syscall(runtime.SYS_MUNMAP, statusBuf, 4096)
				}
			}
		}

		// Build response
		if flags == captureFlagNone {
			// Legacy response: [len=4][exit_code]
			runtime.Store32(resultBuf, 0, 4)
			runtime.Store32(resultBuf, 4, payloadResult)

			directWrite := runtime.Syscall(runtime.SYS_WRITE, sockFd, resultBuf, 8)
			if directWrite < 0 {
				reboot()
			}
		} else {
			// New response: [len:4][exit_code:4][stdout_len:4][stdout_data][stderr_len:4][stderr_data]
			// Calculate total response length (excluding the len field itself)
			var responseLen int64 = 0
			responseLen = 4 + 4 + stdoutLen + 4 + stderrLen // exit_code + stdout_len + stdout_data + stderr_len + stderr_data

			runtime.Store32(resultBuf, 0, responseLen)
			runtime.Store32(resultBuf, 4, payloadResult)
			runtime.Store32(resultBuf, 8, stdoutLen)
			// stdout_data is already at offset 12

			// Write stderr_len after stdout_data using pointer arithmetic
			// We use Store32 with the base pointer + offset and constant 0
			var stderrLenPtr int64 = 0
			stderrLenPtr = resultBuf + 12 + stdoutLen
			runtime.Store32(stderrLenPtr, 0, stderrLen)
			// stderr_data is already at stderrLenPtr + 4

			// Write the entire response
			var totalWriteLen int64 = 0
			totalWriteLen = 4 + responseLen // len field + response data
			writeResult := vsockWriteFull(sockFd, resultBuf, totalWriteLen)
			if writeResult < 0 {
				runtime.Printf("initx: vsock write result failed (errno=0x%x)\n", 0-writeResult)
				reboot()
			}
		}
	}
}

// readPipeNonBlocking reads all available data from a pipe fd into buf.
// Returns the number of bytes read, or negative errno on error.
// This function sets the pipe to non-blocking mode to avoid hanging.
func readPipeNonBlocking(fd int64, buf int64, maxLen int64) int64 {
	// Set non-blocking mode
	oldFlags := runtime.Syscall(runtime.SYS_FCNTL, fd, runtime.F_GETFL, 0)
	if oldFlags >= 0 {
		runtime.Syscall(runtime.SYS_FCNTL, fd, runtime.F_SETFL, oldFlags|runtime.O_NONBLOCK)
	}

	var totalRead int64 = 0
	var done int64 = 0
	var errorResult int64 = 0

	for done == 0 {
		if totalRead >= maxLen {
			done = 1
		}
		if done == 0 {
			var readResult int64 = 0
			readResult = runtime.Syscall(runtime.SYS_READ, fd, buf+totalRead, maxLen-totalRead)
			if readResult < 0 {
				// EAGAIN/EWOULDBLOCK means no more data available
				if readResult == -11 {
					done = 1
				} else {
					// Other error
					if totalRead > 0 {
						done = 1
					} else {
						errorResult = readResult
						done = 1
					}
				}
			} else {
				if readResult == 0 {
					// EOF
					done = 1
				} else {
					totalRead = totalRead + readResult
				}
			}
		}
	}

	if errorResult < 0 {
		return errorResult
	}
	return totalRead
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
