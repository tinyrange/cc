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
	runtime.Printf("cc: running container init program\n")

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

	runtime.Printf("cc: mounted filesystems\n")

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

	runtime.Printf("cc: changed root to container\n")

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

	runtime.Printf("cc: mounted devpts\n")

	// === Phase 7: System configuration ===
	// These helper functions are placeholders - their bodies are replaced at IR level

	setHostname()
	runtime.Printf("cc: set hostname\n")

	configureLoopback()
	runtime.Printf("cc: configured loopback interface\n")

	setHostsFile()
	runtime.Printf("cc: configured /etc/hosts\n")

	// === Phase 8: Network configuration (conditional) ===
	if runtime.Ifdef("network") {
		configureInterface()
		addDefaultRoute()
		setResolvConf()
		runtime.Printf("cc: configured network interface\n")
	}

	// === Phase 9: Change to working directory ===
	changeWorkDir()

	// === Phase 10: Execute command ===
	if runtime.Ifdef("exec") {
		runtime.Printf("cc: executing command\n")
		execCommand()
	} else {
		forkExecWait()
	}

	return 0
}

// Placeholder helper functions - bodies are replaced at IR level with actual implementations.
// These functions exist to provide call sites that the injection mechanism can replace.

func setHostname() int64         { return 0 }
func configureLoopback() int64   { return 0 }
func setHostsFile() int64        { return 0 }
func configureInterface() int64  { return 0 }
func addDefaultRoute() int64     { return 0 }
func setResolvConf() int64       { return 0 }
func changeWorkDir() int64       { return 0 }
func execCommand() int64         { return 0 }
func forkExecWait() int64        { return 0 }

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
