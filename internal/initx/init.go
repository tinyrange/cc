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

func rebootOnError(val ir.Var, msg string) ir.Block {
	return ir.Block{
		ir.If(ir.IsNegative(val), ir.Block{
			ir.Printf(msg, ir.Op(ir.OpSub, ir.Int64(0), val)),
			ir.Syscall(
				defs.SYS_REBOOT,
				linux.LINUX_REBOOT_MAGIC1,
				linux.LINUX_REBOOT_MAGIC2,
				linux.LINUX_REBOOT_CMD_RESTART,
				ir.Int64(0),
			),
		}),
	}
}

func logKmsg(msg string) ir.Block {
	fd := ir.Var("__initx_kmsg_fd")
	done := nextHelperLabel("kmsg_done")
	return ir.Block{
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/kmsg",
			ir.Int64(linux.O_WRONLY),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(fd), ir.Goto(done)),
		ir.Syscall(defs.SYS_WRITE, fd, msg, ir.Int64(int64(len(msg)))),
		ir.Syscall(defs.SYS_CLOSE, fd),
		ir.DeclareLabel(done, ir.Block{}),
	}
}

func Build(cfg BuilderConfig) (*ir.Program, error) {
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
			logKmsg(fmt.Sprintf("initx: loading module %s\n", mod.Name)),
			ir.Assign(resultVar, ir.Syscall(
				defs.SYS_INIT_MODULE,
				dataPtr,
				dataLen,
				moduleParamArg,
			)),
			rebootOnError(resultVar, fmt.Sprintf("initx: failed to load module %s (errno=0x%%x)\n", mod.Name)),
			logKmsg(fmt.Sprintf("initx: module %s ready\n", mod.Name)),
		})
	}

	main := ir.Method{
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
					ir.Int64(0),
				),
			}),
		}),

		logKmsg("initx: dev tmpfs ready\n"),

		preloads,

		// once the preloads are done assume we can open /dev/console and replace stdin/stdout/stderr
		ir.Assign(ir.Var("consoleFd"), ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/console",
			ir.Int64(linux.O_RDWR),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(ir.Var("consoleFd")), ir.Block{
			ir.Printf("initx: failed to open /dev/console (errno=0x%x)\n",
				ir.Op(ir.OpSub, ir.Int64(0), ir.Var("consoleFd")),
			),
			ir.Syscall(defs.SYS_REBOOT,
				linux.LINUX_REBOOT_MAGIC1,
				linux.LINUX_REBOOT_MAGIC2,
				linux.LINUX_REBOOT_CMD_RESTART,
				ir.Int64(0),
			),
		}),
		// create a new session to be able to own the console
		ir.Assign(ir.Var("setsidResult"), ir.Syscall(
			defs.SYS_SETSID,
		)),
		ir.If(ir.IsNegative(ir.Var("setsidResult")), ir.Block{
			ir.Printf("initx: failed to create session (errno=0x%x)\n",
				ir.Op(ir.OpSub, ir.Int64(0), ir.Var("setsidResult")),
			),
			ir.Syscall(defs.SYS_REBOOT,
				linux.LINUX_REBOOT_MAGIC1,
				linux.LINUX_REBOOT_MAGIC2,
				linux.LINUX_REBOOT_CMD_RESTART,
				ir.Int64(0),
			),
		}),
		// ensure the console is our controlling terminal so interactive shells behave
		ir.Assign(ir.Var("ttyResult"), ir.Syscall(
			defs.SYS_IOCTL,
			ir.Var("consoleFd"),
			ir.Int64(linux.TIOCSCTTY),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(ir.Var("ttyResult")), ir.Block{
			// TIOCSCTTY returns -EPERM if we already have a controlling terminal; treat
			// that case as success to avoid rebooting when the console is already set.
			ir.If(ir.IsNotEqual(ir.Var("ttyResult"), ir.Int64(-int64(linux.EPERM))), ir.Block{
				ir.Printf("initx: failed to set controlling TTY (errno=0x%x)\n",
					ir.Op(ir.OpSub, ir.Int64(0), ir.Var("ttyResult")),
				),
				ir.Syscall(defs.SYS_REBOOT,
					linux.LINUX_REBOOT_MAGIC1,
					linux.LINUX_REBOOT_MAGIC2,
					linux.LINUX_REBOOT_CMD_RESTART,
					ir.Int64(0),
				),
			}),
		}),
		// dup2 consoleFd to stdin (0), stdout (1) and stderr (2)
		ir.Syscall(defs.SYS_DUP3, ir.Var("consoleFd"), ir.Int64(0), ir.Int64(0)),
		ir.Syscall(defs.SYS_DUP3, ir.Var("consoleFd"), ir.Int64(1), ir.Int64(0)),
		ir.Syscall(defs.SYS_DUP3, ir.Var("consoleFd"), ir.Int64(2), ir.Int64(0)),
		logKmsg("initx: console ready\n"),

		// open /dev/mem
		openFile(ir.Var("memFd"), "/dev/mem", linux.O_RDWR|linux.O_SYNC),
		rebootOnError(ir.Var("memFd"), "initx: failed to open /dev/mem (errno=0x%x)\n"),
		logKmsg("initx: opened /dev/mem\n"),

		// map mailbox region
		mmapFile(
			ir.Var("mailboxMem"),
			ir.Var("memFd"),
			ir.Int64(0x1000),
			linux.PROT_READ|linux.PROT_WRITE,
			linux.MAP_SHARED,
			ir.Int64(0xf000_0000),
		),
		rebootOnError(ir.Var("mailboxMem"), "initx: failed to map mailbox region (errno=0x%x)\n"),
		logKmsg("initx: mapped mailbox\n"),

		// map config region
		mmapFile(
			ir.Var("configMem"),
			ir.Var("memFd"),
			ir.Int64(4*1024*1024),
			linux.PROT_READ|linux.PROT_WRITE,
			linux.MAP_SHARED,
			ir.Int64(0xf000_3000),
		),
		rebootOnError(ir.Var("configMem"), "initx: failed to map config region (errno=0x%x)\n"),
		logKmsg("initx: mapped config region\n"),

		// map anon a 4MB region r/w/x for copying binaries into
		mmapFile(
			ir.Var("anonMem"),
			ir.Int64(-1),
			ir.Int64(4*1024*1024),
			linux.PROT_READ|linux.PROT_WRITE|linux.PROT_EXEC,
			linux.MAP_PRIVATE|linux.MAP_ANONYMOUS,
			ir.Int64(0),
		),
		rebootOnError(ir.Var("anonMem"), "initx: failed to map anonymous payload region (errno=0x%x)\n"),
		logKmsg("initx: mapped anon payload region\n"),

		ir.DeclareLabel("loop", ir.Block{
			logKmsg("initx: entering main loop\n"),
			// read uint32(configMem[0]) and compare to config header magic. If not equal print a error (magic value not found) and power off.
			ir.If(ir.IsEqual(
				ir.Var("configMem").Mem().As32(),
				ir.Int64(configHeaderMagicValue),
			), ir.Block{
				ir.Assign(ir.Var("codeLen"), ir.Var("configMem").MemWithDisp(4).As32()),
				ir.Assign(ir.Var("relocCount"), ir.Var("configMem").MemWithDisp(8).As32()),
				ir.Assign(ir.Var("relocBytes"), ir.Op(ir.OpShl, ir.Var("relocCount"), int64(2))),
				ir.Assign(ir.Var("codeOffset"), ir.Op(ir.OpAdd, ir.Int64(configHeaderSize), ir.Var("relocBytes"))),

				logKmsg("initx: loading payload\n"),

				// copy binary payload from configMem+codeOffset to anonMem
				ir.Assign(ir.Var("copySrc"), ir.Op(ir.OpAdd, ir.Var("configMem"), ir.Var("codeOffset"))),
				ir.Assign(ir.Var("copyDst"), ir.Var("anonMem")),
				ir.Assign(ir.Var("remaining"), ir.Var("codeLen")),

				// copy 4 bytes at a time while possible
				ir.DeclareLabel("copy_qword_loop", ir.Block{
					ir.If(ir.IsLessThan(ir.Var("remaining"), ir.Int64(4)), ir.Block{
						ir.Goto(ir.Label("copy_qword_done")),
					}),
					ir.Assign(ir.Var("copyDst").Mem().As32(), ir.Var("copySrc").Mem().As32()),
					ir.Assign(ir.Var("copyDst"), ir.Op(ir.OpAdd, ir.Var("copyDst"), int64(4))),
					ir.Assign(ir.Var("copySrc"), ir.Op(ir.OpAdd, ir.Var("copySrc"), int64(4))),
					ir.Assign(ir.Var("remaining"), ir.Op(ir.OpSub, ir.Var("remaining"), int64(4))),
					ir.Goto(ir.Label("copy_qword_loop")),
				}),
				ir.DeclareLabel("copy_qword_done", ir.Block{}),

				// copy any leftover tail bytes
				ir.DeclareLabel("copy_tail_loop", ir.Block{
					ir.If(ir.IsZero(ir.Var("remaining")), ir.Block{
						ir.Goto(ir.Label("copy_done")),
					}),
					ir.Assign(ir.Var("copyDst").Mem().As8(), ir.Var("copySrc").Mem().As8()),
					ir.Assign(ir.Var("copyDst"), ir.Op(ir.OpAdd, ir.Var("copyDst"), int64(1))),
					ir.Assign(ir.Var("copySrc"), ir.Op(ir.OpAdd, ir.Var("copySrc"), int64(1))),
					ir.Assign(ir.Var("remaining"), ir.Op(ir.OpSub, ir.Var("remaining"), int64(1))),
					ir.Goto(ir.Label("copy_tail_loop")),
				}),
				ir.DeclareLabel("copy_done", ir.Block{}),

				logKmsg("initx: applying relocations\n"),

				// apply relocations
				ir.Assign(ir.Var("relocPtr"), ir.Op(ir.OpAdd, ir.Var("configMem"), ir.Int64(configHeaderSize))),
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

				logKmsg("initx: jumping to payload\n"),

				// call anonMem
				ir.Call(ir.Var("anonMem")),

				logKmsg("initx: payload returned, yielding to host\n"),

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
					ir.Int64(0),
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
