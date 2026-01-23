//go:build ignore

// Package main is the RTG source for the container init program.
// This file is compiled by the RTG compiler at build time, not the Go compiler.
// The //go:build ignore tag excludes it from normal compilation.
// It imports the rtg/runtime stub package for IDE completion and type checking.
package main

import "github.com/tinyrange/cc/internal/rtg/runtime"

// MMIO configuration sizes (constants)
const (
	timesliceMMIOMapSize = 0x1000
)

// Timeslice IDs for container init phases (50-99) - must match hvf_darwin_arm64.go
// 50=container_start, 51=container_mkdir, 52=container_virtiofs, 53=container_mkdir_mnt
// 54=container_mount_fs, 55=container_chroot, 56=container_devpts, 57=container_qemu
// 58=container_hostname, 59=container_loopback, 60=container_hosts, 61=container_network
// 62=container_workdir, 63=container_drop_priv, 64=container_exec, 65=container_complete
// 70=container_cmd_loop_start, 71=container_cmd_read, 72=container_cmd_exec, 73=container_cmd_done

// Vsock command protocol constants
// These are used for vsock-based command execution instead of MMIO
const (
	// vsockCmdPort is the vsock port for container command execution (separate from program loading port 9998)
	vsockCmdPort = 9997

	// Message types for vsock command protocol
	vsockMsgReady   = 0x52454459 // "REDY" - guest -> host: container ready
	vsockMsgCmdDone = 0x444F4E45 // "DONE" - guest -> host: command done (followed by exit code)
	vsockMsgExecCmd = 0x45584543 // "EXEC" - host -> guest: execute command
)

// main is the entrypoint for the container init program.
// Helper function bodies are replaced at IR level with actual implementations.
// Ifdef flags control which code paths are included:
//   - "network": include network configuration (ConfigureInterface, AddDefaultRoute, SetResolvConf)
//   - "command_loop": use command loop instead of baked-in command (for late snapshots)
//   - "exec": use exec instead of fork/exec/wait (only when command_loop is false)
func main() int64 {
	// Map MMIO regions for performance instrumentation
	// This requires /dev/mem to be available (usually from devtmpfs)
	var timesliceMem int64 = 0

	memFd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/dev/mem", runtime.O_RDWR|runtime.O_SYNC, 0)
	if memFd >= 0 {
		// Map timeslice MMIO region (optional - for performance tracing)
		timesliceMem = runtime.Syscall(runtime.SYS_MMAP, 0, timesliceMMIOMapSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_SHARED, memFd, runtime.Config("TIMESLICE_MMIO_PHYS_ADDR"))
		if timesliceMem < 0 {
			timesliceMem = 0
		}

		runtime.Syscall(runtime.SYS_CLOSE, memFd)
	}
	// Record: container_start (50)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 50)
	}

	runtime.LogKmsg("cc: running container init program\n")

	// === Phase 1: Create mount points ===
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/proc", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/sys", 0o755)
	// Record: container_mkdir (51)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 51)
	}

	// === Phase 2: Mount virtiofs ===
	virtiofsMountErr := runtime.Syscall(runtime.SYS_MOUNT, "rootfs", "/mnt", "virtiofs", 0, "")
	if virtiofsMountErr < 0 {
		runtime.Printf("cc: failed to mount virtiofs: errno=0x%x\n", 0-virtiofsMountErr)
		reboot()
	}
	// Record: container_virtiofs (52)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 52)
	}

	// === Phase 3: Create directories in container ===
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/proc", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/sys", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/dev", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/tmp", 0o1777)
	// Record: container_mkdir_mnt (53)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 53)
	}

	// === Phase 4: Mount filesystems ===
	runtime.Syscall(runtime.SYS_MOUNT, "proc", "/mnt/proc", "proc", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "sysfs", "/mnt/sys", "sysfs", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "devtmpfs", "/mnt/dev", "devtmpfs", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "tmpfs", "/mnt/tmp", "tmpfs", 0, "mode=1777")

	// Mount /dev/shm (wlroots/xkbcommon use it for shm-backed buffers like keymaps)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/dev/shm", 0o1777)
	runtime.Syscall(runtime.SYS_MOUNT, "tmpfs", "/mnt/dev/shm", "tmpfs", 0, "mode=1777")
	// Record: container_mount_fs (54)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 54)
	}

	runtime.LogKmsg("cc: mounted filesystems\n")

	// === Phase 5: Change root to container ===
	chdirErr := runtime.Syscall(runtime.SYS_CHDIR, "/mnt")
	if chdirErr < 0 {
		runtime.Printf("cc: failed to chdir to /mnt: errno=0x%x\n", 0-chdirErr)
		reboot()
	}

	// Create oldroot for pivot_root
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "oldroot", 0o755)

	// Try pivot_root first
	pivotResult := runtime.Syscall(runtime.SYS_PIVOT_ROOT, ".", "oldroot")
	if pivotResult < 0 {
		// Fallback to chroot
		chrootErr := runtime.Syscall(runtime.SYS_CHROOT, ".")
		if chrootErr < 0 {
			runtime.Printf("cc: failed to chroot: errno=0x%x\n", 0-chrootErr)
			reboot()
		}
	}

	if pivotResult >= 0 {
		// pivot_root succeeded, clean up oldroot
		chdirRootErr := runtime.Syscall(runtime.SYS_CHDIR, "/")
		if chdirRootErr < 0 {
			runtime.Printf("cc: failed to chdir to new root: errno=0x%x\n", 0-chdirRootErr)
			reboot()
		}

		umountErr := runtime.Syscall(runtime.SYS_UMOUNT2, "/oldroot", runtime.MNT_DETACH)
		if umountErr < 0 {
			runtime.Printf("cc: failed to unmount oldroot: errno=0x%x\n", 0-umountErr)
			reboot()
		}
	}

	runtime.LogKmsg("cc: changed root to container\n")
	// Record: container_chroot (55)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 55)
	}

	// Remove oldroot directory
	unlinkErr := runtime.Syscall(runtime.SYS_UNLINKAT, runtime.AT_FDCWD, "/oldroot", runtime.AT_REMOVEDIR)
	if unlinkErr < 0 {
		runtime.Printf("cc: failed to remove oldroot: errno=0x%x\n", 0-unlinkErr)
		reboot()
	}

	// === Phase 6: Additional setup ===

	// Mount devpts
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/dev/pts", 0o755)
	devptsErr := runtime.Syscall(runtime.SYS_MOUNT, "devpts", "/dev/pts", "devpts", 0, "")
	if devptsErr < 0 {
		runtime.Printf("cc: failed to mount devpts: errno=0x%x\n", 0-devptsErr)
		reboot()
	}

	// Create /dev/fd symlink to /proc/self/fd
	symlinkErr := runtime.Syscall(runtime.SYS_SYMLINKAT, "/proc/self/fd", runtime.AT_FDCWD, "/dev/fd")
	if symlinkErr < 0 {
		runtime.Printf("cc: failed to symlink /proc/self/fd to /dev/fd: errno=0x%x\n", 0-symlinkErr)
		reboot()
	}
	// Record: container_devpts (56)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 56)
	}

	runtime.LogKmsg("cc: mounted devpts\n")

	// === Phase 6.5: QEMU Emulation Setup (conditional) ===
	if runtime.Ifdef("qemu_emulation") {
		setupQEMUEmulation()
		// Record: container_qemu (57)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 57)
		}
		runtime.LogKmsg("cc: configured QEMU user emulation\n")
	}

	// === Phase 7: System configuration ===
	// These helper functions are placeholders - their bodies are replaced at IR level

	setHostname()
	// Record: container_hostname (58)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 58)
	}
	runtime.LogKmsg("cc: set hostname\n")

	configureLoopback()
	// Record: container_loopback (59)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 59)
	}
	runtime.LogKmsg("cc: configured loopback interface\n")

	setHostsFile()
	// Record: container_hosts (60)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 60)
	}
	runtime.LogKmsg("cc: configured /etc/hosts\n")

	// === Phase 8: Network configuration (conditional) ===
	if runtime.Ifdef("network") {
		configureInterface()
		addDefaultRoute()
		setResolvConf()
		// Record: container_network (61)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 61)
		}
		runtime.LogKmsg("cc: configured network interface\n")
	}

	// === Phase 9: Change to working directory ===
	changeWorkDir()
	// Record: container_workdir (62)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 62)
	}

	// === Phase 9.5: Drop privileges if configured ===
	if runtime.Ifdef("drop_privileges") {
		dropPrivileges()
		// Record: container_drop_priv (63)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 63)
		}
		runtime.LogKmsg("cc: dropped privileges\n")
	}

	// === Phase 10: Execute command or enter command loop ===
	// Record: container_exec (64)
	if timesliceMem > 0 {
		runtime.Store32(timesliceMem, 0, 64)
	}

	if runtime.Ifdef("command_loop") {
		// Late snapshot mode: enter vsock command loop
		vsockCommandLoop(timesliceMem)
		// vsockCommandLoop never returns
	} else {
		// Legacy mode: execute baked-in command
		if runtime.Ifdef("exec") {
			runtime.LogKmsg("cc: executing command\n")
			execCommand()
		} else {
			forkExecWait()
		}

		// === Phase 11: Complete
		// Record: container_complete (65)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 65)
		}
	}

	return 0
}

// Helper function implementations using pure RTG.

// setHostname sets the system hostname from the config value.
func setHostname() int64 {
	ptr, length := runtime.EmbedConfigCString("hostname")
	err := runtime.Syscall(runtime.SYS_SETHOSTNAME, ptr, length)
	if err < 0 {
		runtime.Printf("cc: failed to set hostname: errno=0x%x\n", 0-err)
		reboot()
	}
	return 0
}

// configureLoopback brings up the loopback interface (lo) with IP 127.0.0.1.
func configureLoopback() int64 {
	var ifreq [40]byte

	// Create socket
	fd := runtime.Syscall(runtime.SYS_SOCKET, runtime.AF_INET, runtime.SOCK_DGRAM, 0)
	if fd < 0 {
		runtime.Printf("cc: failed to create socket for loopback: errno=0x%x\n", 0-fd)
		reboot()
	}

	// Zero out entire structure
	runtime.Store64(&ifreq[0], 0, 0)
	runtime.Store64(&ifreq[0], 8, 0)
	runtime.Store64(&ifreq[0], 16, 0)
	runtime.Store64(&ifreq[0], 24, 0)
	runtime.Store64(&ifreq[0], 32, 0)

	// Copy "lo" to interface name (offset 0)
	runtime.Store8(&ifreq[0], 0, 0x6c)
	runtime.Store8(&ifreq[0], 1, 0x6f)
	runtime.Store8(&ifreq[0], 2, 0)

	// Set IP address: 127.0.0.1 in network byte order = 0x0100007F
	// sockaddr_in: family (AF_INET) at offset 16, sin_addr at offset 20
	runtime.Store32(&ifreq[0], 16, runtime.AF_INET) // sa_family + sin_port as uint32
	runtime.Store32(&ifreq[0], 20, 0x0100007F)      // 127.0.0.1 in network byte order
	runtime.Syscall(runtime.SYS_IOCTL, fd, runtime.SIOCSIFADDR, &ifreq[0])

	// Set netmask: 255.0.0.0 in network byte order = 0x000000FF
	// Re-copy interface name
	runtime.Store8(&ifreq[0], 0, 0x6c)
	runtime.Store8(&ifreq[0], 1, 0x6f)
	runtime.Store8(&ifreq[0], 2, 0)
	runtime.Store64(&ifreq[0], 16, 0) // Zero out sockaddr
	runtime.Store64(&ifreq[0], 24, 0)
	runtime.Store64(&ifreq[0], 32, 0)
	runtime.Store32(&ifreq[0], 16, runtime.AF_INET)
	runtime.Store32(&ifreq[0], 20, 0x000000FF) // 255.0.0.0 in network byte order
	runtime.Syscall(runtime.SYS_IOCTL, fd, runtime.SIOCSIFNETMASK, &ifreq[0])

	// Bring up interface (IFF_UP = 0x1)
	runtime.Store8(&ifreq[0], 0, 0x6c)
	runtime.Store8(&ifreq[0], 1, 0x6f)
	runtime.Store8(&ifreq[0], 2, 0)
	runtime.Store64(&ifreq[0], 16, 0)
	runtime.Store64(&ifreq[0], 24, 0)
	runtime.Store64(&ifreq[0], 32, 0)
	runtime.Store32(&ifreq[0], 16, runtime.IFF_UP)
	err := runtime.Syscall(runtime.SYS_IOCTL, fd, runtime.SIOCSIFFLAGS, &ifreq[0])
	if err < 0 {
		runtime.Syscall(runtime.SYS_CLOSE, fd)
		runtime.Printf("cc: failed to bring up loopback: errno=0x%x\n", 0-err)
		reboot()
	}

	runtime.Syscall(runtime.SYS_CLOSE, fd)
	return 0
}

// setHostsFile writes /etc/hosts with localhost entries.
// The hosts content is provided via the "hosts_content" config value.
func setHostsFile() int64 {
	ptr, length := runtime.EmbedConfigString("hosts_content")

	// Open /etc/hosts for writing
	fd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/etc/hosts",
		runtime.O_WRONLY|runtime.O_CREAT|runtime.O_TRUNC, 0o644)
	if fd < 0 {
		// Non-fatal - just skip if we can't write
		return 0
	}

	// Write content
	runtime.Syscall(runtime.SYS_WRITE, fd, ptr, length)
	runtime.Syscall(runtime.SYS_CLOSE, fd)
	return 0
}

// configureInterface configures the network interface.
// This is a placeholder - body is replaced at IR level with actual implementation.
func configureInterface() int64 { return 0 }
func addDefaultRoute() int64    { return 0 }

// setResolvConf writes /etc/resolv.conf with the DNS server.
// The resolv.conf content is provided via the "resolv_content" config value.
func setResolvConf() int64 {
	ptr, length := runtime.EmbedConfigString("resolv_content")

	// Open /etc/resolv.conf for writing
	fd := runtime.Syscall(runtime.SYS_OPENAT, runtime.AT_FDCWD, "/etc/resolv.conf",
		runtime.O_WRONLY|runtime.O_CREAT|runtime.O_TRUNC, 0o644)
	if fd < 0 {
		runtime.Printf("cc: failed to open /etc/resolv.conf: errno=0x%x\n", 0-fd)
		reboot()
	}

	// Write content
	err := runtime.Syscall(runtime.SYS_WRITE, fd, ptr, length)
	if err < 0 {
		runtime.Printf("cc: failed to write /etc/resolv.conf: errno=0x%x\n", 0-err)
		runtime.Syscall(runtime.SYS_CLOSE, fd)
		reboot()
	}

	runtime.Syscall(runtime.SYS_CLOSE, fd)
	return 0
}

// changeWorkDir changes to the working directory from config.
func changeWorkDir() int64 {
	ptr, _ := runtime.EmbedConfigCString("workdir")
	runtime.Syscall(runtime.SYS_CHDIR, ptr)
	return 0
}
func execCommand() int64        { return 0 }
func forkExecWait() int64       { return 0 }
func dropPrivileges() int64     { return 0 }
func setupQEMUEmulation() int64 { return 0 }

// vsockCommandLoop handles vsock-based command execution.
// This function never returns - it loops forever waiting for commands or reboots on error.
// Protocol:
//   - Guest -> Host: [type:4=READY]
//   - Host -> Guest: [type:4=EXEC][data_len:4][path_len:4][argc:4][envc:4][path\0][args\0...][envs\0...]
//   - Guest -> Host: [type:4=DONE][exit_code:4]
func vsockCommandLoop(timesliceMem int64) {
	// Get vsock port from compile-time config
	var port int64 = 0
	port = runtime.Config("VSOCK_CMD_PORT")

	// Create vsock socket
	sockFd := runtime.Syscall(runtime.SYS_SOCKET, runtime.AF_VSOCK, runtime.SOCK_STREAM, 0)
	if sockFd < 0 {
		runtime.Printf("cc: failed to create vsock socket (errno=0x%x)\n", 0-sockFd)
		reboot()
	}

	// Allocate sockaddr_vm structure (16 bytes)
	sockaddrMem := runtime.Syscall(runtime.SYS_MMAP, 0, 16, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if sockaddrMem < 0 {
		runtime.Printf("cc: failed to alloc sockaddr_vm (errno=0x%x)\n", 0-sockaddrMem)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Zero and fill sockaddr_vm
	runtime.Store64(sockaddrMem, 0, 0)
	runtime.Store64(sockaddrMem, 8, 0)
	runtime.Store16(sockaddrMem, 0, runtime.AF_VSOCK)        // svm_family
	runtime.Store32(sockaddrMem, 4, port)                    // svm_port
	runtime.Store32(sockaddrMem, 8, runtime.VMADDR_CID_HOST) // svm_cid = 2 (host)

	// Connect to host
	connectResult := runtime.Syscall(runtime.SYS_CONNECT, sockFd, sockaddrMem, 16)
	if connectResult < 0 {
		runtime.Printf("cc: failed to connect vsock (errno=0x%x)\n", 0-connectResult)
		runtime.Syscall(runtime.SYS_MUNMAP, sockaddrMem, 16)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Free sockaddr_vm
	runtime.Syscall(runtime.SYS_MUNMAP, sockaddrMem, 16)

	// Allocate receive buffer (1MB should be sufficient for command data)
	var recvBufSize int64 = 1048576
	recvBuf := runtime.Syscall(runtime.SYS_MMAP, 0, recvBufSize, runtime.PROT_READ|runtime.PROT_WRITE, runtime.MAP_PRIVATE|runtime.MAP_ANONYMOUS, -1, 0)
	if recvBuf < 0 {
		runtime.Printf("cc: failed to alloc recv buffer (errno=0x%x)\n", 0-recvBuf)
		runtime.Syscall(runtime.SYS_CLOSE, sockFd)
		reboot()
	}

	// Send READY message to host
	runtime.LogKmsg("cc: container ready, sending vsock READY\n")
	runtime.Store32(recvBuf, 0, vsockMsgReady)
	writeResult := runtime.Syscall(runtime.SYS_WRITE, sockFd, recvBuf, 4)
	if writeResult < 0 {
		runtime.Printf("cc: failed to send READY (errno=0x%x)\n", 0-writeResult)
		reboot()
	}

	// Main command loop
	for {
		// Record: container_cmd_loop_start (70)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 70)
		}

		// Read message header: type(4) + data_len(4) + path_len(4) + argc(4) + envc(4) = 20 bytes
		readResult := vsockReadFull(sockFd, recvBuf, 20)
		if readResult < 0 {
			runtime.Printf("cc: failed to read message header (errno=0x%x)\n", 0-readResult)
			reboot()
		}

		msgType := runtime.Load32(recvBuf, 0)
		if msgType != vsockMsgExecCmd {
			runtime.Printf("cc: unexpected message type: 0x%x\n", msgType)
			reboot()
		}

		// Record: container_cmd_read (71)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 71)
		}

		dataLen := runtime.Load32(recvBuf, 4)
		pathLen := runtime.Load32(recvBuf, 8)
		argc := runtime.Load32(recvBuf, 12)
		envc := runtime.Load32(recvBuf, 16)

		// Read the command data (path\0 + args\0...\0 + envs\0...\0)
		if dataLen > recvBufSize-20 {
			runtime.Printf("cc: command data too large: 0x%x\n", dataLen)
			reboot()
		}
		// Read command data into buffer starting at offset 20 (after header)
		readResult = vsockReadFull(sockFd, recvBuf+20, dataLen)
		if readResult < 0 {
			runtime.Printf("cc: failed to read command data (errno=0x%x)\n", 0-readResult)
			reboot()
		}

		// Record: container_cmd_exec (72)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 72)
		}

		// Execute the command
		// Data layout in recvBuf:
		// [0-3]: msgType (ignored now)
		// [4-7]: dataLen
		// [8-11]: pathLen
		// [12-15]: argc
		// [16-19]: envc
		// [20...]: path\0 + args\0... + envs\0...
		exitCode := forkExecWaitFromBuffer(recvBuf+20, pathLen, argc, envc)

		// Record: container_cmd_done (73)
		if timesliceMem > 0 {
			runtime.Store32(timesliceMem, 0, 73)
		}

		// Send CMD_DONE with exit code: [type:4][exit_code:4]
		runtime.Store32(recvBuf, 0, vsockMsgCmdDone)
		runtime.Store32(recvBuf, 4, exitCode)
		writeResult = runtime.Syscall(runtime.SYS_WRITE, sockFd, recvBuf, 8)
		if writeResult < 0 {
			runtime.Printf("cc: failed to send CMD_DONE (errno=0x%x)\n", 0-writeResult)
			reboot()
		}
	}
}

// forkExecWaitFromBuffer forks and executes command from a memory buffer.
// The buffer contains: path\0 + args\0... + envs\0...
// This is a placeholder - the body is replaced at IR level.
func forkExecWaitFromBuffer(bufPtr int64, pathLen int64, argc int64, envc int64) int64 {
	return 0
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
