package initx

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/rtg"
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

type rtgHelperSpec struct {
	params []string
}

var (
	rtgHelperSource = fmt.Sprintf(`package main
func ClearRunResults(mailbox uintptr) {
	store32(mailbox, %d, 0)
	store32(mailbox, %d, 0)
	store32(mailbox, %d, 0)
	store32(mailbox, %d, 0)
}

func ReportRunResult(stage int64, detail int64) {
	rrFd := syscall(SYS_OPENAT, %d, "/dev/mem", %d, 0)
	if rrFd < 0 {
		goto done
	}

	rrPtr := syscall(SYS_MMAP, 0, %d, %d, %d, rrFd, %d)
	if rrPtr < 0 {
		syscall(SYS_CLOSE, rrFd)
		goto fail
	}

	store32(rrPtr, %d, detail)
	store32(rrPtr, %d, stage)
	syscall(SYS_MUNMAP, rrPtr, %d)
	syscall(SYS_CLOSE, rrFd)
	goto done

fail:
	goto done
done:
	return
}

func RequestSnapshot() {
	snapFd := syscall(SYS_OPENAT, %d, "/dev/mem", %d, 0)
	if snapFd < 0 {
		goto done
	}

	snapPtr := syscall(SYS_MMAP, 0, %d, %d, %d, snapFd, %d)
	if snapPtr < 0 {
		syscall(SYS_CLOSE, snapFd)
		goto fail
	}

	store32(snapPtr, 0, %d)
	syscall(SYS_MUNMAP, snapPtr, %d)
	syscall(SYS_CLOSE, snapFd)
	goto done

fail:
	goto done
done:
	return
}

func Mount(source string, target string, fstype string, flags uintptr, data string, errLabel label, errVar int64) {
	errVar = syscall(SYS_MOUNT, source, target, fstype, flags, data)
	if errVar == %d {
		errVar = 0
	}
	if errVar < 0 {
		gotoLabel(errLabel)
	}
}

func Mkdir(path string, mode int64, errLabel label, errVar int64) {
	errVar = syscall(SYS_MKDIRAT, %d, path, mode)
	if errVar == %d {
		errVar = 0
	}
	if errVar < 0 {
		gotoLabel(errLabel)
	}
}

func Chroot(path string, errLabel label, errVar int64) {
	errVar = syscall(SYS_CHROOT, path)
	if errVar < 0 {
		gotoLabel(errLabel)
	}

	errVar = syscall(SYS_CHDIR, "/")
	if errVar < 0 {
		gotoLabel(errLabel)
	}
}

func SetHostname(name string, nameLen int64, errLabel label, errVar int64) {
	errVar = syscall(SYS_SETHOSTNAME, name, nameLen)
	if errVar < 0 {
		gotoLabel(errLabel)
	}
}
`,
		mailboxRunResultDetailOffset,
		mailboxRunResultStageOffset,
		mailboxStartResultDetailOffset,
		mailboxStartResultStageOffset,
		linux.AT_FDCWD,
		linux.O_RDWR|linux.O_SYNC,
		mailboxMapSize,
		linux.PROT_READ|linux.PROT_WRITE,
		linux.MAP_SHARED,
		snapshotSignalPhysAddr,
		mailboxRunResultDetailOffset,
		mailboxRunResultStageOffset,
		mailboxMapSize,
		linux.AT_FDCWD,
		linux.O_RDWR|linux.O_SYNC,
		mailboxMapSize,
		linux.PROT_READ|linux.PROT_WRITE,
		linux.MAP_SHARED,
		snapshotSignalPhysAddr,
		snapshotRequestValue,
		mailboxMapSize,
		-int64(linux.EBUSY),
		linux.AT_FDCWD,
		-int64(linux.EEXIST),
	)

	rtgHelperSpecs = map[string]rtgHelperSpec{
		"ClearRunResults": {params: []string{"mailbox"}},
		"ReportRunResult": {params: []string{"stage", "detail"}},
		"RequestSnapshot": {params: nil},
		"Mount":           {params: []string{"source", "target", "fstype", "flags", "data", "errLabel", "errVar"}},
		"Mkdir":           {params: []string{"path", "mode", "errLabel", "errVar"}},
		"Chroot":          {params: []string{"path", "errLabel", "errVar"}},
		"SetHostname":     {params: []string{"name", "nameLen", "errLabel", "errVar"}},
	}

	rtgHelpersOnce sync.Once
	rtgHelpers     map[string]ir.Method
	rtgHelpersErr  error
)

func loadRtgHelpers() error {
	rtgHelpersOnce.Do(func() {
		prog, err := rtg.CompileProgram(rtgHelperSource)
		if err != nil {
			rtgHelpersErr = err
			return
		}
		rtgHelpers = prog.Methods
	})
	return rtgHelpersErr
}

func instantiateHelper(name string, bindings map[string]any) ir.Block {
	if err := loadRtgHelpers(); err != nil {
		panic(fmt.Sprintf("initx: compile rtg helper %s: %v", name, err))
	}

	spec, ok := rtgHelperSpecs[name]
	if !ok {
		panic(fmt.Sprintf("initx: unknown rtg helper %s", name))
	}
	for _, param := range spec.params {
		if _, ok := bindings[param]; !ok {
			panic(fmt.Sprintf("initx: helper %s missing binding for %s", name, param))
		}
	}

	method, ok := rtgHelpers[name]
	if !ok {
		panic(fmt.Sprintf("initx: helper %s missing compiled method", name))
	}

	return rewriteHelper(method, bindings)
}

func rewriteHelper(method ir.Method, bindings map[string]any) ir.Block {
	var out ir.Block
	for _, frag := range method {
		if _, ok := frag.(ir.DeclareParam); ok {
			continue
		}
		out = append(out, rewriteFragment(frag, bindings))
	}
	return out
}

func rewriteFragment(frag ir.Fragment, bindings map[string]any) ir.Fragment {
	switch v := frag.(type) {
	case ir.Block:
		var rewritten ir.Block
		for _, inner := range v {
			rewritten = append(rewritten, rewriteFragment(inner, bindings))
		}
		return rewritten
	case ir.AssignFragment:
		return ir.Assign(rewriteFragment(v.Dst, bindings), rewriteFragment(v.Src, bindings))
	case ir.SyscallFragment:
		args := make([]ir.Fragment, len(v.Args))
		for i, arg := range v.Args {
			args[i] = rewriteFragment(arg, bindings)
		}
		return ir.SyscallFragment{Num: v.Num, Args: args}
	case ir.IfFragment:
		thenFrag := rewriteFragment(v.Then, bindings)
		var elseFrag ir.Fragment
		if v.Otherwise != nil {
			elseFrag = rewriteFragment(v.Otherwise, bindings)
		}
		return ir.IfFragment{
			Cond:      rewriteCondition(v.Cond, bindings),
			Then:      thenFrag,
			Otherwise: elseFrag,
		}
	case ir.CompareCondition:
		return ir.CompareCondition{
			Kind:  v.Kind,
			Left:  rewriteFragment(v.Left, bindings),
			Right: rewriteFragment(v.Right, bindings),
		}
	case ir.IsNegativeCondition:
		return ir.IsNegativeCondition{Value: rewriteFragment(v.Value, bindings)}
	case ir.IsZeroCondition:
		return ir.IsZeroCondition{Value: rewriteFragment(v.Value, bindings)}
	case ir.OpFragment:
		return ir.OpFragment{Kind: v.Kind, Left: rewriteFragment(v.Left, bindings), Right: rewriteFragment(v.Right, bindings)}
	case ir.GotoFragment:
		return ir.Goto(rewriteFragment(v.Label, bindings))
	case ir.LabelFragment:
		return ir.LabelFragment{Label: v.Label, Block: rewriteFragment(v.Block, bindings).(ir.Block)}
	case ir.MemVar:
		base := v.Base
		if repl, ok := bindings[string(v.Base)]; ok {
			if bVar, ok := repl.(ir.Var); ok {
				base = bVar
			} else {
				panic(fmt.Sprintf("initx: mem base %s requires ir.Var binding", v.Base))
			}
		}
		var disp ir.Fragment
		if v.Disp != nil {
			disp = rewriteFragment(v.Disp, bindings)
		}
		return ir.MemVar{Base: base, Disp: disp, Width: v.Width}
	case ir.Var:
		if repl, ok := bindings[string(v)]; ok {
			if replVar, ok := repl.(ir.Var); ok {
				return replVar
			}
			return repl
		}
		return v
	case ir.Label:
		if repl, ok := bindings[string(v)]; ok {
			if lbl, ok := repl.(ir.Label); ok {
				return lbl
			}
			return repl
		}
		return v
	case ir.Int64, ir.Int32, ir.Int16, ir.Int8:
		return v
	case string:
		return v
	case ir.ReturnFragment:
		return ir.ReturnFragment{Value: rewriteFragment(v.Value, bindings)}
	case ir.PrintfFragment:
		args := make([]ir.Fragment, len(v.Args))
		for i, arg := range v.Args {
			args[i] = rewriteFragment(arg, bindings)
		}
		return ir.PrintfFragment{Format: v.Format, Args: args}
	default:
		return frag
	}
}

func rewriteCondition(cond ir.Condition, bindings map[string]any) ir.Condition {
	switch c := cond.(type) {
	case ir.CompareCondition:
		return rewriteFragment(c, bindings).(ir.CompareCondition)
	case ir.IsNegativeCondition:
		return rewriteFragment(c, bindings).(ir.IsNegativeCondition)
	case ir.IsZeroCondition:
		return rewriteFragment(c, bindings).(ir.IsZeroCondition)
	default:
		return cond
	}
}

// Mailbox helpers

// ClearRunResults zeros the run/start result slots so the host observes a clean
// state once the payload completes.
func ClearRunResults(mailbox ir.Var) ir.Fragment {
	return instantiateHelper("ClearRunResults", map[string]any{
		"mailbox": mailbox,
	})
}

// ReportRunResult stores the provided detail/stage pair into the mailbox so the
// host can decode the error reason.
func ReportRunResult(stage any, detail any) ir.Fragment {
	return instantiateHelper("ReportRunResult", map[string]any{
		"stage":  stage,
		"detail": detail,
	})
}

// RequestSnapshot asks the host to capture a snapshot by writing the dedicated
// doorbell value to the mailbox.
func RequestSnapshot() ir.Fragment {
	return instantiateHelper("RequestSnapshot", nil)
}

// Filesystem

func Mount(
	source, target, fstype string,
	flags uintptr,
	data string,
	errLabel ir.Label,
	errVar ir.Var,
) ir.Fragment {
	return instantiateHelper("Mount", map[string]any{
		"source":   source,
		"target":   target,
		"fstype":   fstype,
		"flags":    flags,
		"data":     data,
		"errLabel": errLabel,
		"errVar":   errVar,
	})
}

func Mkdir(path string, mode uint32, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return instantiateHelper("Mkdir", map[string]any{
		"path":     path,
		"mode":     int64(mode),
		"errLabel": errLabel,
		"errVar":   errVar,
	})
}

func Chroot(path string, errLabel ir.Label, errVar ir.Var) ir.Fragment {
	return instantiateHelper("Chroot", map[string]any{
		"path":     path,
		"errLabel": errLabel,
		"errVar":   errVar,
	})
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
	status := ir.Var("waitStatus")
	signal := ir.Var("waitSignal")
	exitCode := ir.Var("waitExitCode")

	return ir.Block{
		ir.Assign(pid, ir.Syscall(defs.SYS_CLONE, ir.Int64(defs.SIGCHLD), 0, 0, 0, 0)),
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
					ir.Assign(status, ptr.Mem().As32()),
					ir.Assign(signal, ir.Op(ir.OpAnd, status, ir.Int64(0x7f))),
					ir.Assign(exitCode, ir.Op(
						ir.OpAnd,
						ir.Op(ir.OpShr, status, ir.Int64(8)),
						ir.Int64(0xff),
					)),
					ir.If(ir.IsNotEqual(signal, ir.Int64(0)), ir.Block{
						ir.Assign(exitCode, ir.Op(ir.OpAdd, signal, ir.Int64(128))),
					}),
					ir.Assign(errVar, exitCode),
				}
			},
		}),
	}
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
	return instantiateHelper("SetHostname", map[string]any{
		"name":     name,
		"nameLen":  int64(len(name)),
		"errLabel": errLabel,
		"errVar":   errVar,
	})
}
