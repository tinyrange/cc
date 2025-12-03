package initx

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

var execVarCounter uint64
var helperLabelCounter uint64

const (
	stdinCopyBufferSize = 4096
)

func nextExecVar() asm.Variable {
	// Start from a high number to avoid conflicts with registers (0-32)
	// and other manually assigned variables.
	return asm.Variable(1000 + atomic.AddUint64(&execVarCounter, 1))
}

func nextHelperLabel(prefix string) ir.Label {
	id := atomic.AddUint64(&helperLabelCounter, 1)
	return ir.Label(fmt.Sprintf("__initx_%s_%d", prefix, id))
}

// Mailbox helpers

// ClearRunResults zeros the run/start result slots so the host observes a clean
// state once the payload completes.
func ClearRunResults(mailbox ir.Var) ir.Fragment {
	zero := ir.Var("__rr_zero")
	return ir.Block{
		ir.Assign(zero, ir.Int64(0)),
		ir.Assign(mailbox.MemWithDisp(mailboxRunResultDetailOffset).As32(), zero.As32()),
		ir.Assign(mailbox.MemWithDisp(mailboxRunResultStageOffset).As32(), zero.As32()),
		ir.Assign(mailbox.MemWithDisp(mailboxStartResultDetailOffset).As32(), zero.As32()),
		ir.Assign(mailbox.MemWithDisp(mailboxStartResultStageOffset).As32(), zero.As32()),
	}
}

// ReportRunResult stores the provided detail/stage pair into the mailbox so the
// host can decode the error reason.
func ReportRunResult(stage any, detail any) ir.Fragment {
	failed := nextHelperLabel("mailbox_fail")
	done := nextHelperLabel("mailbox_done")
	fd := ir.Var("__mailbox_fd")
	ptr := ir.Var("__mailbox_ptr")
	stageVar := ir.Var("__mailbox_stage")
	detailVar := ir.Var("__mailbox_detail")
	return ir.Block{
		ir.Assign(stageVar, stage),
		ir.Assign(detailVar, detail),
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/mem",
			ir.Int64(linux.O_RDWR|linux.O_SYNC),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(fd), ir.Goto(done)),
		ir.Assign(ptr, ir.Syscall(
			defs.SYS_MMAP,
			ir.Int64(0),
			ir.Int64(mailboxMapSize),
			ir.Int64(linux.PROT_READ|linux.PROT_WRITE),
			ir.Int64(linux.MAP_SHARED),
			fd,
			ir.Int64(snapshotSignalPhysAddr),
		)),
		ir.If(ir.IsNegative(ptr), ir.Block{
			ir.Syscall(defs.SYS_CLOSE, fd),
			ir.Goto(failed),
		}),
		ir.Assign(ptr.MemWithDisp(mailboxRunResultDetailOffset).As32(), detailVar.As32()),
		ir.Assign(ptr.MemWithDisp(mailboxRunResultStageOffset).As32(), stageVar.As32()),
		ir.Syscall(defs.SYS_MUNMAP, ptr, ir.Int64(mailboxMapSize)),
		ir.Syscall(defs.SYS_CLOSE, fd),
		ir.Goto(done),
		ir.DeclareLabel(failed, ir.Block{}),
		ir.DeclareLabel(done, ir.Block{}),
	}
}

// RequestSnapshot asks the host to capture a snapshot by writing the dedicated
// doorbell value to the mailbox.
func RequestSnapshot() ir.Fragment {
	failed := nextHelperLabel("snapshot_fail")
	done := nextHelperLabel("snapshot_done")
	fd := ir.Var("__snapshot_fd")
	ptr := ir.Var("__snapshot_ptr")
	val := ir.Var("__snapshot_signal")
	return ir.Block{
		ir.Assign(val, ir.Int64(snapshotRequestValue)),
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/mem",
			ir.Int64(linux.O_RDWR|linux.O_SYNC),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(fd), ir.Goto(done)),
		ir.Assign(ptr, ir.Syscall(
			defs.SYS_MMAP,
			ir.Int64(0),
			ir.Int64(mailboxMapSize),
			ir.Int64(linux.PROT_READ|linux.PROT_WRITE),
			ir.Int64(linux.MAP_SHARED),
			fd,
			ir.Int64(snapshotSignalPhysAddr),
		)),
		ir.If(ir.IsNegative(ptr), ir.Block{
			ir.Syscall(defs.SYS_CLOSE, fd),
			ir.Goto(failed),
		}),
		ir.Assign(ptr.Mem().As32(), val.As32()),
		ir.Syscall(defs.SYS_MUNMAP, ptr, ir.Int64(mailboxMapSize)),
		ir.Syscall(defs.SYS_CLOSE, fd),
		ir.Goto(done),
		ir.DeclareLabel(failed, ir.Block{}),
		ir.DeclareLabel(done, ir.Block{}),
	}
}

// Filesystem

func Mount(
	source, target, fstype string,
	flags uintptr,
	data string,
	errLabel ir.Label,
	errVar ir.Var,
) ir.Fragment {
	return ir.Block{
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MOUNT,
			source,
			target,
			fstype,
			ir.Int64(int64(flags)),
			data,
		)),

		// if the error is EBUSY, ignore
		ir.If(ir.IsEqual(errVar, ir.Int64(-int64(linux.EBUSY))), ir.Assign(errVar, ir.Int64(0))),

		// check for other errors
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
	}
}

func Mkdir(path string, mode uint32, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.Block{
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MKDIRAT,
			ir.Int64(linux.AT_FDCWD),
			path,
			ir.Int64(int64(mode)),
		)),
		// check if errVar == -EEXIST, if so, ignore
		ir.If(
			ir.IsEqual(errVar, ir.Int64(-int64(linux.EEXIST))),
			ir.Assign(errVar, ir.Int64(0)),
		),
		ir.If(
			ir.IsNegative(errVar),
			ir.Goto(errLabel),
		),
	}
}

func Chroot(path string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.Block{
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_CHROOT,
			path,
		)),
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_CHDIR,
			"/",
		)),
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
	}
}

// CreateFileFromStdin reads length bytes from stdin and writes them into path.
func CreateFileFromStdin(path string, length int64, mode uint32, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	fd := ir.Var("__stdin_file_fd")
	bufPtr := ir.Var("__stdin_file_buf_ptr")
	chunkSize := ir.Var("__stdin_file_chunk_size")
	chunkRemaining := ir.Var("__stdin_file_chunk_remaining")
	totalRemaining := ir.Var("__stdin_file_total_remaining")
	readPtr := ir.Var("__stdin_file_read_ptr")
	writePtr := ir.Var("__stdin_file_write_ptr")
	writeRemaining := ir.Var("__stdin_file_write_remaining")
	readLoop := nextHelperLabel("stdin_file_read_loop")
	readChunkLoop := nextHelperLabel("stdin_file_read_chunk_loop")
	chunkReady := nextHelperLabel("stdin_file_chunk_ready")
	writeLoop := nextHelperLabel("stdin_file_write_loop")
	writeDone := nextHelperLabel("stdin_file_write_done")
	done := nextHelperLabel("stdin_file_done")

	return ir.Block{
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			path,
			ir.Int64(linux.O_WRONLY|linux.O_CREAT|linux.O_TRUNC),
			ir.Int64(int64(mode)),
		)),
		ir.Assign(errVar, fd),
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
		ir.Assign(totalRemaining, ir.Int64(length)),
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: stdinCopyBufferSize,
			Body: func(slot ir.StackSlot) ir.Fragment {
				return ir.Block{
					ir.Assign(bufPtr, slot.Pointer()),
					ir.Goto(readLoop),
					ir.DeclareLabel(readLoop, ir.Block{
						ir.If(ir.IsZero(totalRemaining), ir.Goto(done)),
						ir.Assign(chunkSize, totalRemaining),
						ir.If(ir.IsGreaterThan(chunkSize, ir.Int64(stdinCopyBufferSize)), ir.Assign(chunkSize, ir.Int64(stdinCopyBufferSize))),
						ir.Assign(chunkRemaining, chunkSize),
						ir.Assign(readPtr, bufPtr),
						ir.Goto(readChunkLoop),
					}),
					ir.DeclareLabel(readChunkLoop, ir.Block{
						ir.If(ir.IsZero(chunkRemaining), ir.Goto(chunkReady)),
						ir.Printf("Reading %x bytes from stdin...\n", chunkRemaining),
						ir.Assign(errVar, ir.Syscall(
							defs.SYS_READ,
							ir.Int64(0),
							readPtr,
							chunkRemaining,
						)),
						ir.If(ir.IsNegative(errVar), ir.Block{
							ir.Syscall(defs.SYS_CLOSE, fd),
							ir.Goto(errLabel),
						}),
						ir.If(ir.IsZero(errVar), ir.Block{
							ir.Assign(errVar, ir.Int64(-int64(linux.EPIPE))),
							ir.Syscall(defs.SYS_CLOSE, fd),
							ir.Goto(errLabel),
						}),
						ir.Assign(readPtr, ir.Op(ir.OpAdd, readPtr, errVar)),
						ir.Assign(chunkRemaining, ir.Op(ir.OpSub, chunkRemaining, errVar)),
						ir.Goto(readChunkLoop),
					}),
					ir.DeclareLabel(chunkReady, ir.Block{
						ir.Assign(writePtr, bufPtr),
						ir.Assign(writeRemaining, chunkSize),
						ir.Goto(writeLoop),
					}),
					ir.DeclareLabel(writeLoop, ir.Block{
						ir.If(ir.IsZero(writeRemaining), ir.Goto(writeDone)),
						ir.Printf("Writing %x bytes to file...\n", writeRemaining),
						ir.Assign(errVar, ir.Syscall(
							defs.SYS_WRITE,
							fd,
							writePtr,
							writeRemaining,
						)),
						ir.If(ir.IsNegative(errVar), ir.Block{
							ir.Syscall(defs.SYS_CLOSE, fd),
							ir.Goto(errLabel),
						}),
						ir.Assign(writePtr, ir.Op(ir.OpAdd, writePtr, errVar)),
						ir.Assign(writeRemaining, ir.Op(ir.OpSub, writeRemaining, errVar)),
						ir.Goto(writeLoop),
					}),
					ir.DeclareLabel(writeDone, ir.Block{
						ir.Assign(totalRemaining, ir.Op(ir.OpSub, totalRemaining, chunkSize)),
						ir.Goto(readLoop),
					}),
					ir.DeclareLabel(done, ir.Block{}),
				}
			},
		}),
		ir.Assign(errVar, ir.Syscall(defs.SYS_CLOSE, fd)),
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
	}
}

// Timekeeping

func SetClock(sec int64, nsec int64, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: 16, // sizeof(struct timespec)
		Body: func(slot ir.StackSlot) ir.Fragment {
			ptr := ir.Var("tsPtr")
			return ir.Block{
				ir.Assign(slot.At(0), ir.Int64(sec)),
				ir.Assign(slot.At(8), ir.Int64(nsec)),
				ir.Assign(ptr, slot.Pointer()),
				ir.Assign(errVar, ir.Syscall(
					defs.SYS_CLOCK_SETTIME,
					ir.Int64(linux.CLOCK_REALTIME),
					ptr,
				)),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			}
		},
	})
}

func SyncClockFromPTP(ptpPath string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: 16, // sizeof(struct timespec)
		Body: func(slot ir.StackSlot) ir.Fragment {
			fd := ir.Var("ptpFd")
			ptr := ir.Var("tsPtr")
			clockId := ir.Var("clockId")

			return ir.Block{
				ir.Assign(fd, ir.Syscall(
					defs.SYS_OPENAT,
					ir.Int64(linux.AT_FDCWD),
					ptpPath,
					ir.Int64(linux.O_RDWR|linux.O_CLOEXEC),
					ir.Int64(0),
				)),
				ir.Assign(errVar, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),

				ir.Assign(ptr, slot.Pointer()),

				// Calculate clockId = ((~fd) << 3) | 3
				ir.Assign(clockId, ir.Op(ir.OpSub, ir.Int64(0), fd)),
				ir.Assign(clockId, ir.Op(ir.OpSub, clockId, ir.Int64(1))),
				ir.Assign(clockId, ir.Op(ir.OpShl, clockId, ir.Int64(3))),
				ir.Assign(clockId, ir.Op(ir.OpAdd, clockId, ir.Int64(3))),

				ir.Assign(errVar, ir.Syscall(
					defs.SYS_CLOCK_GETTIME,
					clockId,
					ptr,
				)),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				ir.Assign(errVar, ir.Syscall(
					defs.SYS_CLOCK_SETTIME,
					ir.Int64(linux.CLOCK_REALTIME),
					ptr,
				)),
				ir.Syscall(defs.SYS_CLOSE, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			}
		},
	})
}

// Networking

func ConfigureInterface(ifName string, ip uint32, mask uint32, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: 40, // sizeof(struct ifreq)
		Body: func(slot ir.StackSlot) ir.Fragment {
			fd := ir.Var("netFd")
			ptr := ir.Var("ifreqPtr")

			familyIP := int64(linux.AF_INET) | (int64(ip) << 32)
			familyMask := int64(linux.AF_INET) | (int64(mask) << 32)
			flagsVal := int64(linux.IFF_UP | linux.IFF_RUNNING)

			return ir.Block{
				ir.Assign(fd, ir.Syscall(defs.SYS_SOCKET, ir.Int64(linux.AF_INET), ir.Int64(linux.SOCK_DGRAM), ir.Int64(0))),
				ir.Assign(errVar, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),

				ir.Assign(ptr, slot.Pointer()),

				copyStringToSlot(slot, ifName),

				// Set IP
				ir.Assign(slot.At(16), ir.Int64(familyIP)),
				ir.Assign(errVar, ir.Syscall(defs.SYS_IOCTL, fd, ir.Int64(linux.SIOCSIFADDR), ptr)),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				// Set Mask
				ir.Assign(slot.At(16), ir.Int64(familyMask)),
				ir.Assign(errVar, ir.Syscall(defs.SYS_IOCTL, fd, ir.Int64(linux.SIOCSIFNETMASK), ptr)),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				// Bring up
				ir.Assign(slot.At(16), ir.Int64(flagsVal)),
				ir.Assign(errVar, ir.Syscall(defs.SYS_IOCTL, fd, ir.Int64(linux.SIOCSIFFLAGS), ptr)),
				ir.Syscall(defs.SYS_CLOSE, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			}
		},
	})
}

func copyStringToSlot(slot ir.StackSlot, s string) ir.Fragment {
	var frags []ir.Fragment
	tmp := ir.Var("strTmp")
	for i := 0; i < len(s) && i < 16; i++ {
		frags = append(frags,
			ir.Assign(tmp, ir.Int64(int64(s[i]))),
			ir.Assign(slot.At(i), tmp.As8()),
		)
	}
	for i := len(s); i < 16; i++ {
		frags = append(frags,
			ir.Assign(tmp, ir.Int64(0)),
			ir.Assign(slot.At(i), tmp.As8()),
		)
	}
	return ir.Block(frags)
}

func AddDefaultRoute(gateway uint32, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	// Build netlink message
	nlMsg := make([]byte, 36)

	// nlmsghdr
	binary.LittleEndian.PutUint32(nlMsg[0:4], 36)                                                                      // len
	binary.LittleEndian.PutUint16(nlMsg[4:6], linux.RTM_NEWROUTE)                                                      // type
	binary.LittleEndian.PutUint16(nlMsg[6:8], linux.NLM_F_REQUEST|linux.NLM_F_CREATE|linux.NLM_F_EXCL|linux.NLM_F_ACK) // flags
	binary.LittleEndian.PutUint32(nlMsg[8:12], 0)                                                                      // seq
	binary.LittleEndian.PutUint32(nlMsg[12:16], 0)                                                                     // pid

	// rtmsg
	nlMsg[16] = linux.AF_INET           // family
	nlMsg[17] = 0                       // dst_len
	nlMsg[18] = 0                       // src_len
	nlMsg[19] = 0                       // tos
	nlMsg[20] = linux.RT_TABLE_MAIN     // table
	nlMsg[21] = linux.RTPROT_BOOT       // protocol
	nlMsg[22] = linux.RT_SCOPE_UNIVERSE // scope
	nlMsg[23] = linux.RTN_UNICAST       // type

	// rtattr RTA_GATEWAY
	binary.LittleEndian.PutUint16(nlMsg[28:30], 8)                 // len
	binary.LittleEndian.PutUint16(nlMsg[30:32], linux.RTA_GATEWAY) // type
	binary.LittleEndian.PutUint32(nlMsg[32:36], gateway)           // val

	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: 36,
		Body: func(slot ir.StackSlot) ir.Fragment {
			fd := ir.Var("nlFd")
			tmp := ir.Var("nlTmp")
			ptr := ir.Var("nlPtr")

			// Copy message to stack using 32-bit writes
			var copyFrags []ir.Fragment
			for i := 0; i < len(nlMsg); i += 4 {
				val := binary.LittleEndian.Uint32(nlMsg[i : i+4])
				copyFrags = append(copyFrags,
					ir.Assign(tmp, ir.Int64(int64(val))),
					ir.Assign(slot.At(i), tmp.As32()),
				)
			}

			return ir.Block{
				ir.Assign(fd, ir.Syscall(defs.SYS_SOCKET, ir.Int64(linux.AF_NETLINK), ir.Int64(linux.SOCK_RAW), ir.Int64(linux.NETLINK_ROUTE))),
				ir.Assign(errVar, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),

				ir.Assign(ptr, slot.Pointer()),
				ir.Block(copyFrags),
				ir.Assign(errVar, ir.Syscall(defs.SYS_SENDTO, fd, ptr, ir.Int64(36), ir.Int64(0), ir.Int64(0), ir.Int64(0))),
				ir.Syscall(defs.SYS_CLOSE, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			}
		},
	})
}

// Execution

func Exec(path string, argv []string, envp []string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	// We need to construct argv and envp arrays on stack.
	// argv = [path, arg1, ..., NULL]
	// envp = [env1, ..., NULL]

	// We need to allocate space for pointers.
	// Size = (len(argv) + 1 + len(envp) + 1) * 8

	argvLen := len(argv) + 2
	envpLen := len(envp) + 1
	totalSize := int64((argvLen + envpLen) * 8)

	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: totalSize,
		Body: func(slot ir.StackSlot) ir.Fragment {
			var frags []ir.Fragment

			// Helper to load string constant and store pointer in slot
			storeStrPtr := func(s string, slotIdx int) {
				ptrVar := ir.Var(s + "_ptr") // Unique-ish name
				// We need to use LoadConstantBytesConfig to get the pointer.
				// We use a unique asm.Variable for the target.
				target := nextExecVar()

				frags = append(frags, ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
					Target:        target,
					Data:          []byte(s),
					ZeroTerminate: true,
					Pointer:       ptrVar,
				}))

				frags = append(frags, ir.Assign(slot.At(slotIdx*8), ptrVar))
			}

			// argv[0] = path
			storeStrPtr(path, 0)

			for i, arg := range argv {
				storeStrPtr(arg, i+1)
			}
			// argv[last] = NULL
			frags = append(frags, ir.Assign(slot.At(argvLen*8-8), ir.Int64(0)))

			// envp
			envpStart := argvLen
			for i, env := range envp {
				storeStrPtr(env, envpStart+i)
			}
			// envp[last] = NULL
			frags = append(frags, ir.Assign(slot.At((argvLen+envpLen)*8-8), ir.Int64(0)))

			argvPtr := ir.Var("argvPtr")
			envpPtr := ir.Var("envpPtr")

			frags = append(frags,
				ir.Assign(argvPtr, slot.Pointer()),
				ir.Assign(envpPtr, slot.PointerWithDisp(argvLen*8)),
				ir.Assign(errVar, ir.Syscall(
					defs.SYS_EXECVE,
					path,
					argvPtr,
					envpPtr,
				)),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			)

			return ir.Block(frags)
		},
	})
}

func ForkExecWait(path string, argv []string, envp []string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	pid := ir.Var("pid")

	return ir.Block{
		ir.Assign(pid, ir.Syscall(defs.SYS_FORK)),
		ir.If(ir.IsNegative(pid), ir.Block{
			ir.Assign(errVar, pid),
			ir.Goto(errLabel),
		}),
		ir.If(ir.IsZero(pid), ir.Block{
			// Child
			Exec(path, argv, envp, errLabel, errVar),
			ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
		}),
		// Parent
		ir.WithStackSlot(ir.StackSlotConfig{
			Size: 8,
			Body: func(slot ir.StackSlot) ir.Fragment {
				ptr := ir.Var("statusPtr")
				return ir.Block{
					ir.Assign(ptr, slot.Pointer()),
					ir.Assign(errVar, ir.Syscall(defs.SYS_WAIT4, pid, ptr, ir.Int64(0), ir.Int64(0))),
					ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
				}
			},
		}),
	}
}

// SpawnExecutable runs path with the provided argv/envp and waits for completion.
func SpawnExecutable(path string, argv []string, envp []string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ForkExecWait(path, argv, envp, errLabel, errVar)
}

// Profiling & Misc

func SetupProfiling(basePtr ir.Var, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	// Map 2 pages: Page (0xf0001000) + Notify (0xf0002000)
	// Size = 0x2000
	// Phys = 0xf0001000

	return ir.WithStackSlot(ir.StackSlotConfig{
		Size: 8, // scratch for fd
		Body: func(slot ir.StackSlot) ir.Fragment {
			fd := ir.Var("memFd")
			return ir.Block{
				ir.Assign(fd, ir.Syscall(
					defs.SYS_OPENAT,
					ir.Int64(linux.AT_FDCWD),
					"/dev/mem",
					ir.Int64(linux.O_RDWR|linux.O_SYNC),
					ir.Int64(0),
				)),
				ir.Assign(errVar, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),

				ir.Assign(basePtr, ir.Syscall(
					defs.SYS_MMAP,
					ir.Int64(0),      // addr
					ir.Int64(0x2000), // size
					ir.Int64(linux.PROT_READ|linux.PROT_WRITE),
					ir.Int64(linux.MAP_SHARED),
					fd,
					ir.Int64(0xf0001000), // phys
				)),
				ir.Assign(errVar, basePtr), // mmap returns addr or -errno

				ir.Syscall(defs.SYS_CLOSE, fd),

				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
			}
		},
	})
}

func SendProfilingEvent(basePtr ir.Var, name string) ir.Fragment {
	// Copy name to basePtr (Page)
	// Write 1 to basePtr + 0x1000 (Notify)

	var frags []ir.Fragment
	tmp := ir.Var("profTmp")

	// Copy string
	for i := 0; i < len(name); i++ {
		frags = append(frags,
			ir.Assign(tmp, ir.Int64(int64(name[i]))),
			ir.Assign(basePtr.MemWithDisp(i).As8(), tmp.As8()),
		)
	}
	// Null terminate
	frags = append(frags,
		ir.Assign(tmp, ir.Int64(0)),
		ir.Assign(basePtr.MemWithDisp(len(name)).As8(), tmp.As8()),
	)

	// Notify
	frags = append(frags,
		ir.Assign(tmp, ir.Int64(1)),
		ir.Assign(basePtr.MemWithDisp(0x1000), tmp),
	)

	return ir.Block(frags)
}

func SetHostname(name string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return ir.Block{
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_SETHOSTNAME,
			name,
			ir.Int64(int64(len(name))),
		)),
		ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),
	}
}
