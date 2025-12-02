package initx

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

type BuilderConfig struct {
	Arch hv.CpuArchitecture
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

func Build(cfg BuilderConfig) (*ir.Program, error) {
	main := ir.Method{
		ir.Printf("Hello, World\n"),

		// ensure /dev exists and expose /dev/mem
		ir.Syscall(
			defs.SYS_MKDIRAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev",
			ir.Int64(0o755),
		),
		ir.Syscall(
			defs.SYS_MKNODAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/mem",
			ir.Int64(devMemMode),
			ir.Int64(int64(devMemDeviceID)),
		),

		// ensure devtmpfs is mounted so block devices like /dev/vda exist before runtime payloads run
		ir.Assign(ir.Var("devMountErr"), ir.Syscall(
			defs.SYS_MOUNT,
			"devtmpfs",
			"/dev",
			"devtmpfs",
			ir.Int64(0),
			"",
		)),
		ir.If(ir.IsNegative(ir.Var("devMountErr")), ir.Block{
			ir.If(ir.IsNotEqual(
				ir.Var("devMountErr"),
				ir.Int64(-int64(linux.EBUSY)),
			), ir.Block{
				ir.Printf("failed to mount devtmpfs on /dev: 0x%x\n",
					ir.Op(ir.OpSub, ir.Int64(0), ir.Var("devMountErr")),
				),
				ir.Syscall(defs.SYS_REBOOT,
					linux.LINUX_REBOOT_MAGIC1,
					linux.LINUX_REBOOT_MAGIC2,
					linux.LINUX_REBOOT_CMD_RESTART,
				),
			}),
		}),

		// open /dev/mem
		openFile(ir.Var("memFd"), "/dev/mem", linux.O_RDWR|linux.O_SYNC),

		// map mailbox region
		mmapFile(
			ir.Var("mailboxMem"),
			ir.Var("memFd"),
			ir.Int64(0x1000),
			linux.PROT_READ|linux.PROT_WRITE,
			linux.MAP_SHARED,
			ir.Int64(0xf000_0000),
		),

		// map config region
		mmapFile(
			ir.Var("configMem"),
			ir.Var("memFd"),
			ir.Int64(4*1024*1024),
			linux.PROT_READ|linux.PROT_WRITE,
			linux.MAP_SHARED,
			ir.Int64(0xf000_3000),
		),

		// map anon a 4MB region r/w/x for copying binaries into
		mmapFile(
			ir.Var("anonMem"),
			ir.Int64(-1),
			ir.Int64(4*1024*1024),
			linux.PROT_READ|linux.PROT_WRITE|linux.PROT_EXEC,
			linux.MAP_PRIVATE|linux.MAP_ANONYMOUS,
			ir.Int64(0),
		),

		ir.DeclareLabel("loop", ir.Block{
			// read uint32(configMem[0]) and compare to 0xcafebabe. If not equal print a error (magic value not found) and power off.
			ir.If(ir.IsEqual(
				ir.Var("configMem").Mem().As32(),
				ir.Int64(0xcafebabe),
			), ir.Block{
				ir.Assign(ir.Var("codeLen"), ir.Var("configMem").MemWithDisp(4).As32()),
				ir.Assign(ir.Var("relocCount"), ir.Var("configMem").MemWithDisp(8).As32()),
				ir.Assign(ir.Var("relocBytes"), ir.Op(ir.OpShl, ir.Var("relocCount"), int64(2))),
				ir.Assign(ir.Var("codeOffset"), ir.Op(ir.OpAdd, int64(16), ir.Var("relocBytes"))),

				// copy binary payload from configMem+codeOffset to anonMem
				ir.Assign(ir.Var("copySrc"), ir.Op(ir.OpAdd, ir.Var("configMem"), ir.Var("codeOffset"))),
				ir.Assign(ir.Var("copyDst"), ir.Var("anonMem")),
				ir.Assign(ir.Var("remaining"), ir.Var("codeLen")),
				ir.DeclareLabel("copy_loop", ir.Block{
					ir.If(ir.IsZero(ir.Var("remaining")), ir.Block{
						ir.Goto(ir.Label("copy_done")),
					}),
					ir.Assign(ir.Var("copyDst").Mem(), ir.Var("copySrc").Mem()),
					ir.Assign(ir.Var("copyDst"), ir.Op(ir.OpAdd, ir.Var("copyDst"), int64(1))),
					ir.Assign(ir.Var("copySrc"), ir.Op(ir.OpAdd, ir.Var("copySrc"), int64(1))),
					ir.Assign(ir.Var("remaining"), ir.Op(ir.OpSub, ir.Var("remaining"), int64(1))),
					ir.Goto(ir.Label("copy_loop")),
				}),
				ir.DeclareLabel("copy_done", ir.Block{}),

				// apply relocations
				ir.Assign(ir.Var("relocPtr"), ir.Op(ir.OpAdd, ir.Var("configMem"), int64(16))),
				ir.Assign(ir.Var("relocIndex"), ir.Int64(0)),
				ir.DeclareLabel("reloc_loop", ir.Block{
					ir.If(ir.IsGreaterOrEqual(ir.Var("relocIndex"), ir.Var("relocCount")), ir.Block{
						ir.Goto(ir.Label("reloc_done")),
					}),
					ir.Assign(ir.Var("relocEntryPtr"), ir.Op(ir.OpAdd,
						ir.Var("relocPtr"),
						ir.Op(ir.OpShl, ir.Var("relocIndex"), int64(2)),
					)),
					ir.Assign(ir.Var("relocOffset"), ir.Var("relocEntryPtr").Mem().As32()),
					ir.Assign(ir.Var("patchPtr"), ir.Op(ir.OpAdd, ir.Var("anonMem"), ir.Var("relocOffset"))),
					ir.Assign(ir.Var("patchValue"), ir.Var("patchPtr").Mem()),
					ir.Assign(ir.Var("patchValue"), ir.Op(ir.OpAdd, ir.Var("patchValue"), ir.Var("anonMem"))),
					ir.Assign(ir.Var("patchPtr").Mem(), ir.Var("patchValue")),
					ir.Assign(ir.Var("relocIndex"), ir.Op(ir.OpAdd, ir.Var("relocIndex"), int64(1))),
					ir.Goto(ir.Label("reloc_loop")),
				}),
				ir.DeclareLabel("reloc_done", ir.Block{}),

				// call anonMem
				ir.Call(ir.Var("anonMem")),

				// signal completion to the host without clobbering run results
				ir.Assign(ir.Var("tmp32"), ir.Int64(0x444f4e45)),
				ir.Assign(ir.Var("mailboxMem").MemWithDisp(0).As32(), ir.Var("tmp32").As32()),

				ir.Goto(ir.Label("loop")),
			}, ir.Block{
				// magic value not found, print error and power off
				ir.Printf("Magic value not found in config region: %x\n", ir.Var("configMem").Mem().As32()),
				ir.Syscall(defs.SYS_REBOOT,
					linux.LINUX_REBOOT_MAGIC1,
					linux.LINUX_REBOOT_MAGIC2,
					linux.LINUX_REBOOT_CMD_RESTART,
				),
			}),
			ir.Goto(ir.Label("loop")),
		}),
		ir.Goto(ir.Label("loop")),
	}

	return &ir.Program{
		Methods: map[string]ir.Method{
			"main": main,
		},
		Entrypoint: "main",
	}, nil
}
