package initx

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

type ContainerInitConfig struct {
	Arch          hv.CpuArchitecture
	Cmd           []string
	Env           []string
	WorkDir       string
	EnableNetwork bool
	Exec          bool

	Hostname    string // default: tinyrange
	DNS         string // default: 10.42.0.1
	GuestIP     string // default: 10.42.0.2
	GuestMask   string // default: 255.255.255.0
	GuestIFName string // default: eth0
}

func (c *ContainerInitConfig) applyDefaults() {
	if c.Hostname == "" {
		c.Hostname = "tinyrange"
	}
	if c.DNS == "" {
		c.DNS = "10.42.0.1"
	}
	if c.GuestIP == "" {
		c.GuestIP = "10.42.0.2"
	}
	if c.GuestMask == "" {
		c.GuestMask = "255.255.255.0"
	}
	if c.GuestIFName == "" {
		c.GuestIFName = "eth0"
	}
	if c.WorkDir == "" {
		c.WorkDir = "/"
	}
}

func ipToUint32(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip.To4())
}

// BuildContainerInitProgram builds an init program that mounts the virtiofs root,
// switches root into it (pivot_root with chroot fallback), optionally configures
// networking, and finally execs or fork/exec/waits a target command.
func BuildContainerInitProgram(cfg ContainerInitConfig) (*ir.Program, error) {
	cfg.applyDefaults()

	if cfg.Arch == "" || cfg.Arch == hv.ArchitectureInvalid {
		return nil, fmt.Errorf("initx: container init requires valid architecture")
	}
	if len(cfg.Cmd) == 0 || cfg.Cmd[0] == "" {
		return nil, fmt.Errorf("initx: container init requires non-empty command")
	}

	errLabel := ir.Label("__cc_error")
	errVar := ir.Var("__cc_errno")
	pivotResult := ir.Var("__cc_pivot_result")

	main := ir.Method{
		LogKmsg("cc: running container init program\n"),

		// Create mount points
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/sys", ir.Int64(0o755)),

		// Mount virtiofs
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MOUNT,
			"rootfs",
			"/mnt",
			"virtiofs",
			ir.Int64(0),
			"",
		)),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to mount virtiofs: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Create necessary directories in container
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/sys", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/tmp", ir.Int64(0o1777)),

		// Mount proc/sysfs/devtmpfs
		ir.Syscall(defs.SYS_MOUNT, "proc", "/mnt/proc", "proc", ir.Int64(0), ""),
		ir.Syscall(defs.SYS_MOUNT, "sysfs", "/mnt/sys", "sysfs", ir.Int64(0), ""),
		ir.Syscall(defs.SYS_MOUNT, "devtmpfs", "/mnt/dev", "devtmpfs", ir.Int64(0), ""),

		// Mount /dev/shm (wlroots/xkbcommon use it for shm-backed buffers like keymaps).
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev/shm", ir.Int64(0o1777)),
		ir.Syscall(defs.SYS_MOUNT, "tmpfs", "/mnt/dev/shm", "tmpfs", ir.Int64(0), "mode=1777"),

		LogKmsg("cc: mounted filesystems\n"),

		// Change root to container using pivot_root (fallback to chroot).
		ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/mnt")),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to chdir to /mnt: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// pivot_root(".", "oldroot")
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "oldroot", ir.Int64(0o755)),
		ir.Assign(pivotResult, ir.Syscall(defs.SYS_PIVOT_ROOT, ".", "oldroot")),
		ir.Assign(errVar, pivotResult),
		ir.If(ir.IsNegative(pivotResult), ir.Block{
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHROOT, ".")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),
		ir.If(ir.IsGreaterOrEqual(pivotResult, ir.Int64(0)), ir.Block{
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chdir to new root: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
			ir.Assign(errVar, ir.Syscall(defs.SYS_UMOUNT2, "/oldroot", ir.Int64(linux.MNT_DETACH))),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to unmount oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),

		LogKmsg("cc: changed root to container\n"),

		// Always cleanup oldroot (it exists on both pivot_root and chroot fallback).
		ir.Assign(errVar, ir.Syscall(defs.SYS_UNLINKAT, ir.Int64(linux.AT_FDCWD), "/oldroot", ir.Int64(linux.AT_REMOVEDIR))),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to remove oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Change to working directory.
		ir.Syscall(defs.SYS_CHDIR, cfg.WorkDir),

		// /dev/pts + devpts mount.
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/dev/pts", ir.Int64(0o755)),
		ir.Assign(errVar, ir.Syscall(defs.SYS_MOUNT, "devpts", "/dev/pts", "devpts", ir.Int64(0), "")),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to mount devpts: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		LogKmsg("cc: mounted devpts\n"),

		// Set hostname.
		SetHostname(cfg.Hostname, errLabel, errVar),
		LogKmsg("cc: set hostname to container name\n"),
	}

	// Configure network interface if networking is enabled.
	if cfg.EnableNetwork {
		ip := ipToUint32(cfg.GuestIP)
		gateway := ipToUint32(cfg.DNS)
		mask := ipToUint32(cfg.GuestMask)
		main = append(main,
			ConfigureInterface(cfg.GuestIFName, ip, mask, errLabel, errVar),
			AddDefaultRoute(cfg.GuestIFName, gateway, errLabel, errVar),
			SetResolvConf(cfg.DNS, errLabel, errVar),
			LogKmsg("cc: configured network interface\n"),
		)
	}

	if cfg.Exec {
		main = append(main, ir.Block{
			LogKmsg(fmt.Sprintf("cc: executing command %s\n", cfg.Cmd[0])),
			Exec(cfg.Cmd[0], cfg.Cmd[1:], cfg.Env, errLabel, errVar),
		})
	} else {
		main = append(main,
			ForkExecWait(cfg.Cmd[0], cfg.Cmd[1:], cfg.Env, errLabel, errVar),
		)
	}

	main = append(main,
		// Return child exit code to host.
		ir.Return(errVar),

		// Error handler.
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Printf("cc: fatal error during boot: errno=0x%x\n", errVar),
			func() ir.Fragment {
				switch cfg.Arch {
				case hv.ArchitectureX86_64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_RESTART,
						ir.Int64(0),
					)
				case hv.ArchitectureARM64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_POWER_OFF,
						ir.Int64(0),
					)
				default:
					panic(fmt.Sprintf("unsupported architecture for reboot: %s", cfg.Arch))
				}
			}(),
		}),
	)

	return &ir.Program{
		Methods:    map[string]ir.Method{"main": main},
		Entrypoint: "main",
	}, nil
}
