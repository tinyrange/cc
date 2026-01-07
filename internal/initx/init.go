package initx

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
)

type Module struct {
	Name string
	Data []byte
}

type BuilderConfig struct {
	Arch hv.CpuArchitecture

	PreloadModules []kernel.Module
}

const (
	devMemMode             = linux.S_IFCHR | 0o600
	snapshotRequestValue   = 0xdeadbeef
	snapshotSignalPhysAddr = 0xf0000000
	mailboxMapSize         = 0x1000
)

const (
	mailboxRunResultDetailOffset   = 8
	mailboxRunResultStageOffset    = 12
	mailboxStartResultDetailOffset = 16
	mailboxStartResultStageOffset  = 20

	// Config region time fields (set by host for guest clock initialization)
	configTimeSecField  = 24
	configTimeNsecField = 32
)

var (
	devMemDeviceID = linux.Mkdev(1, 1)
)

func openFile(dest ir.Var, filename string, flags int) ir.Block {
	return ir.Block{
		ir.Assign(dest, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			filename,
			ir.Int64(flags),
			ir.Int64(0),
		)),
	}
}

func mmapFile(dest ir.Var, fd any, length any, prot int, flags int, offset any) ir.Block {
	return ir.Block{
		ir.Assign(dest, ir.Syscall(
			defs.SYS_MMAP,
			ir.Int64(0),
			length,
			ir.Int64(prot),
			ir.Int64(flags),
			fd,
			offset,
		)),
	}
}

func rebootOnError(arch hv.CpuArchitecture, val ir.Var, msg string) ir.Block {
	return ir.Block{
		ir.If(ir.IsNegative(val), ir.Block{
			ir.Printf(msg, ir.Op(ir.OpSub, ir.Int64(0), val)),
			func() ir.Fragment {
				switch arch {
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
					panic(fmt.Sprintf("unsupported architecture for reboot: %s", arch))
				}
			}(),
		}),
	}
}

// Build builds the init program using the RTG compiler.
// This is the main entry point for building init programs.
func Build(cfg BuilderConfig) (*ir.Program, error) {
	return BuildFromRTG(cfg)
}
