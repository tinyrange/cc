package initx

import (
	"reflect"
	"testing"

	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

func TestClearRunResultsRTG(t *testing.T) {
	mailbox := ir.Var("mailbox")
	got := ClearRunResults(mailbox)

	want := ir.Block{
		ir.Assign(mailbox.MemWithDisp(ir.Int64(mailboxRunResultDetailOffset)).As32(), ir.Int64(0)),
		ir.Assign(mailbox.MemWithDisp(ir.Int64(mailboxRunResultStageOffset)).As32(), ir.Int64(0)),
		ir.Assign(mailbox.MemWithDisp(ir.Int64(mailboxStartResultDetailOffset)).As32(), ir.Int64(0)),
		ir.Assign(mailbox.MemWithDisp(ir.Int64(mailboxStartResultStageOffset)).As32(), ir.Int64(0)),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ClearRunResults mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReportRunResultRTG(t *testing.T) {
	stage := ir.Int64(3)
	detail := ir.Int64(4)
	got := ReportRunResult(stage, detail)

	fd := ir.Var("rrFd")
	ptr := ir.Var("rrPtr")
	want := ir.Block{
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(linux.AT_FDCWD),
			"/dev/mem",
			ir.Int64(linux.O_RDWR|linux.O_SYNC),
			ir.Int64(0),
		)),
		ir.If(ir.IsLessThan(fd, ir.Int64(0)), ir.Block{ir.Goto(ir.Label("done"))}),
		ir.Assign(ptr, ir.Syscall(
			defs.SYS_MMAP,
			ir.Int64(0),
			ir.Int64(mailboxMapSize),
			ir.Int64(linux.PROT_READ|linux.PROT_WRITE),
			ir.Int64(linux.MAP_SHARED),
			fd,
			ir.Int64(snapshotSignalPhysAddr),
		)),
		ir.If(ir.IsLessThan(ptr, ir.Int64(0)), ir.Block{
			ir.Syscall(defs.SYS_CLOSE, fd),
			ir.Goto(ir.Label("fail")),
		}),
		ir.Assign(ptr.MemWithDisp(ir.Int64(mailboxRunResultDetailOffset)).As32(), detail),
		ir.Assign(ptr.MemWithDisp(ir.Int64(mailboxRunResultStageOffset)).As32(), stage),
		ir.Syscall(defs.SYS_MUNMAP, ptr, ir.Int64(mailboxMapSize)),
		ir.Syscall(defs.SYS_CLOSE, fd),
		ir.Goto(ir.Label("done")),
		ir.DeclareLabel(ir.Label("fail"), ir.Block{
			ir.Goto(ir.Label("done")),
		}),
		ir.DeclareLabel(ir.Label("done"), nil),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReportRunResult mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMountRTG(t *testing.T) {
	errVar := ir.Var("err")
	errLabel := ir.Label("errLabel")

	got := Mount("src", "dst", "fs", 0x100, "data", errLabel, errVar)

	want := ir.Block{
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MOUNT,
			"src",
			"dst",
			"fs",
			uintptr(0x100),
			"data",
		)),
		ir.If(ir.IsEqual(errVar, ir.Int64(-int64(linux.EBUSY))), ir.Block{
			ir.Assign(errVar, ir.Int64(0)),
		}),
		ir.If(ir.IsLessThan(errVar, ir.Int64(0)), ir.Block{ir.Goto(errLabel)}),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Mount mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
