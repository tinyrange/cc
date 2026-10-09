//go:build linux && amd64

package guestinit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Run the upload with real syscalls, denying hardlink creation in a subprocess.
// This models a filesystem that supports RENAME_NOREPLACE but not hardlinks;
// it catches an os.Link regression without depending on a mounted test VM.
func TestSandboxRenameWithoutHardlinks(t *testing.T) {
	for _, mode := range []string{"deny-hardlinks", "unsupported-rename"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestSandboxRenameSyscallChild$")
			cmd.Env = append(os.Environ(), "CC_TEST_SA2_RENAME_MODE="+mode, "CC_TEST_SA2_RENAME_DIR="+t.TempDir())
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("syscall regression %s: %v\n%s", mode, err, output)
			}
		})
	}
}

func TestSandboxRenameSyscallChild(t *testing.T) {
	mode := os.Getenv("CC_TEST_SA2_RENAME_MODE")
	if mode == "" {
		return
	}
	dir := os.Getenv("CC_TEST_SA2_RENAME_DIR")
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	filter := []unix.SockFilter{{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}}
	deny := func(number uint32, errno uint32) {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | errno})
	}
	deny(unix.SYS_LINK, uint32(unix.EPERM))
	deny(unix.SYS_LINKAT, uint32(unix.EPERM))
	if mode == "unsupported-rename" {
		deny(unix.SYS_RENAMEAT2, uint32(unix.ENOSYS))
	}
	filter = append(filter, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	// TSYNC applies the filter to all Go threads; a goroutine migration cannot
	// accidentally bypass the denied syscall and make this regression pass.
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 {
		t.Fatalf("install syscall filter: %v", errno)
	}
	probe := filepath.Join(dir, "hardlink-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(probe, probe+"-link"); !errors.Is(err, unix.EPERM) {
		t.Fatalf("hardlink denial not active: %v", err)
	}
	destination := filepath.Join(dir, "uploaded")
	data := []byte{0, 255, 10, 3, 0}
	err := sandboxFilePut(context.Background(), destination, false, bytes.NewReader(data))
	if mode == "unsupported-rename" {
		if !errors.Is(err, unix.ENOSYS) {
			t.Fatalf("unsupported no-replace must fail closed: %v", err)
		}
		if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unsupported rename installed destination: %v", err)
		}
	} else {
		if err != nil {
			t.Fatalf("upload depended on hardlinks: %v", err)
		}
		got, err := os.ReadFile(destination)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("binary upload: %x %v", got, err)
		}
	}
	stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
	if len(stages) != 0 {
		t.Fatal(fmt.Sprintf("install leaked staging files: %v", stages))
	}
}
