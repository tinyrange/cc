//go:build ignore

// Package main is the RTG source for the container init program.
// This file is compiled by the RTG compiler at build time, not the Go compiler.
// The //go:build ignore tag excludes it from normal compilation.
// It imports the rtg/runtime stub package for IDE completion and type checking.
package main

import "github.com/tinyrange/cc/internal/rtg/runtime"

// main is the entrypoint for the container init program.
// Helper function bodies are replaced at IR level with actual implementations.
// Ifdef flags control which code paths are included:
//   - "network": include network configuration (ConfigureInterface, AddDefaultRoute, SetResolvConf)
//   - "exec": use exec instead of fork/exec/wait
func main() int64 {
	runtime.LogKmsg("cc: running container init program\n")

	// === Phase 1: Create mount points ===
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/proc", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/sys", 0o755)

	// === Phase 2: Mount virtiofs ===
	virtiofsMountErr := runtime.Syscall(runtime.SYS_MOUNT, "rootfs", "/mnt", "virtiofs", 0, "")
	if virtiofsMountErr < 0 {
		runtime.Printf("cc: failed to mount virtiofs: errno=0x%x\n", 0-virtiofsMountErr)
		reboot()
	}

	// === Phase 3: Create directories in container ===
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/proc", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/sys", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/dev", 0o755)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/tmp", 0o1777)

	// === Phase 4: Mount filesystems ===
	runtime.Syscall(runtime.SYS_MOUNT, "proc", "/mnt/proc", "proc", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "sysfs", "/mnt/sys", "sysfs", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "devtmpfs", "/mnt/dev", "devtmpfs", 0, "")
	runtime.Syscall(runtime.SYS_MOUNT, "tmpfs", "/mnt/tmp", "tmpfs", 0, "mode=1777")

	// Mount /dev/shm (wlroots/xkbcommon use it for shm-backed buffers like keymaps)
	runtime.Syscall(runtime.SYS_MKDIRAT, runtime.AT_FDCWD, "/mnt/dev/shm", 0o1777)
	runtime.Syscall(runtime.SYS_MOUNT, "tmpfs", "/mnt/dev/shm", "tmpfs", 0, "mode=1777")

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

	runtime.LogKmsg("cc: mounted devpts\n")

	// === Phase 6.5: QEMU Emulation Setup (conditional) ===
	if runtime.Ifdef("qemu_emulation") {
		setupQEMUEmulation()
		runtime.LogKmsg("cc: configured QEMU user emulation\n")
	}

	// === Phase 7: System configuration ===
	// These helper functions are placeholders - their bodies are replaced at IR level

	setHostname()
	runtime.LogKmsg("cc: set hostname\n")

	configureLoopback()
	runtime.LogKmsg("cc: configured loopback interface\n")

	setHostsFile()
	runtime.LogKmsg("cc: configured /etc/hosts\n")

	// === Phase 8: Network configuration (conditional) ===
	if runtime.Ifdef("network") {
		configureInterface()
		addDefaultRoute()
		setResolvConf()
		runtime.LogKmsg("cc: configured network interface\n")
	}

	// === Phase 9: Change to working directory ===
	changeWorkDir()

	// === Phase 9.5: Drop privileges if configured ===
	if runtime.Ifdef("drop_privileges") {
		dropPrivileges()
		runtime.LogKmsg("cc: dropped privileges\n")
	}

	// === Phase 10: Execute command ===
	if runtime.Ifdef("exec") {
		runtime.LogKmsg("cc: executing command\n")
		execCommand()
	} else {
		forkExecWait()
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
func addDefaultRoute() int64     { return 0 }

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
func execCommand() int64      { return 0 }
func forkExecWait() int64     { return 0 }
func dropPrivileges() int64   { return 0 }
func setupQEMUEmulation() int64 { return 0 }

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
