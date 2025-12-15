//go:build guest

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	_ "github.com/tinyrange/cc/internal/ir/arm64"
	"github.com/tinyrange/cc/internal/linux/defs"
)

// TestFS is the single entry point for the filesystem compliance suite.
// It creates a workspace in basePath and runs exhaustive capability tests.
func testFS(t *testing.T, basePath string) {
	// Structural Sanity Check
	info, err := os.Stat(basePath)
	if err != nil {
		t.Fatalf("basePath %q inaccessible: %v", basePath, err)
	}
	if !info.IsDir() {
		t.Fatalf("basePath %q is not a directory", basePath)
	}

	t.Logf("Starting Filesystem Compliance Suite on: %s", basePath)

	// Workspace Isolation
	workspace := filepath.Join(basePath, fmt.Sprintf("fs_test_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}
	// Strict Cleanup: Ensure no artifacts remain
	t.Cleanup(func() {
		os.RemoveAll(workspace)
	})

	t.Logf("Running FS Compliance Suite in: %s", workspace)

	// Execute Subtests
	t.Run("ExtendedAttributes", func(t *testing.T) { testXattr(t, workspace) })
	t.Run("SparseSupport", func(t *testing.T) { testSparse(t, workspace) })
	t.Run("FileLocking", func(t *testing.T) { testLocking(t, workspace) })
	t.Run("NameLimits", func(t *testing.T) { testNameLimits(t, workspace) })
	t.Run("Atomicity", func(t *testing.T) { testAtomicity(t, workspace) })
	t.Run("RaceConditions", func(t *testing.T) { testRaceConditions(t, workspace) })
	t.Run("VirtioFSWriteRead", func(t *testing.T) { testVirtioFSWriteRead(t, workspace) })
	t.Run("VirtioFSExecutable", func(t *testing.T) { testVirtioFSExecutable(t, workspace) })
	t.Run("Chmod", func(t *testing.T) { testChmod(t, workspace) })
	t.Run("Chown", func(t *testing.T) { testChown(t, workspace) })
}

// --- Extended Attributes (Xattr) ---

func testXattr(t *testing.T, dir string) {
	if runtime.GOOS != "linux" {
		t.Skip("xattr tests currently only implemented for Linux")
	}

	f, err := os.CreateTemp(dir, "xattr_probe")
	if err != nil {
		t.Fatalf("Failed to create probe file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	attrName := "user.test_compliance"
	attrValue := []byte("verified")

	// 1. Probe for Support
	err = setXattr(f.Name(), attrName, attrValue, 0)
	if err == syscall.EOPNOTSUPP || err == syscall.ENOTSUP {
		t.Skipf("Extended attributes not supported on %s", dir)
	}
	if err != nil {
		t.Fatalf("Setxattr failed: %v", err)
	}

	// 2. Verify Read Consistency
	val, err := getXattr(f.Name(), attrName)
	if err != nil {
		t.Fatalf("Getxattr failed after successful Set: %v", err)
	}
	if !bytes.Equal(val, attrValue) {
		t.Errorf("Xattr data corruption. Want %s, Got %s", attrValue, val)
	}

	// 3. Test XATTR_CREATE (Atomic Creation)
	err = setXattr(f.Name(), attrName, []byte("overwrite"), 1) // 1 = XATTR_CREATE
	if err != syscall.EEXIST {
		t.Errorf("XATTR_CREATE on existing attr should return EEXIST, got: %v", err)
	}

	// 4. Test XATTR_REPLACE (Atomic Replacement)
	err = setXattr(f.Name(), "user.missing", []byte("val"), 2) // 2 = XATTR_REPLACE
	if err != syscall.ENODATA {
		t.Errorf("XATTR_REPLACE on missing attr should return ENODATA, got: %v", err)
	}

	// 5. Test Large Attributes (E2BIG)
	// Ext4 usually limits to block size (4k) or less.
	// We verify that the error is explicit, not silent truncation.
	largeVal := make([]byte, 65536) // 64KB
	err = setXattr(f.Name(), "user.huge", largeVal, 0)
	if err != nil && err != syscall.E2BIG && err != syscall.ENOSPC {
		t.Logf("Write huge xattr failed with unexpected error (might be FS specific limit): %v", err)
	} else if err == nil {
		t.Log("Filesystem supports large xattrs (>64KB)")
	}
}

// Low-level wrappers to keep single-file dependency free
func setXattr(path string, attr string, data []byte, flags int) error {
	// syscall.Setxattr signature: (path string, attr string, data []byte, flags int) (err error)
	return syscall.Setxattr(path, attr, data, flags)
}

func getXattr(path string, attr string) ([]byte, error) {
	// Start with reasonable buffer
	dest := make([]byte, 1024)
	sz, err := syscall.Getxattr(path, attr, dest)
	if err == syscall.ERANGE {
		// Buffer too small, query size
		sz, err = syscall.Getxattr(path, attr, nil)
		if err != nil {
			return nil, err
		}
		dest = make([]byte, sz)
		sz, err = syscall.Getxattr(path, attr, dest)
	}
	if err != nil {
		return nil, err
	}
	return dest[:sz], nil
}

// --- Sparse Files ---

func testSparse(t *testing.T, dir string) {
	filePath := filepath.Join(dir, "sparse_test")
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer os.Remove(filePath)
	defer f.Close()

	// 1. Create a 10MB file with data only at ends
	if _, err := f.Write([]byte("H")); err != nil {
		t.Fatal(err)
	}
	// Seek to 10MB
	offset := int64(10 * 1024 * 1024)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("T")); err != nil {
		t.Fatal(err)
	}

	// Sync to ensure metadata hits disk (important for SEEK_HOLE on some FS)
	f.Sync()

	// 2. Check Physical vs Logical size
	var st syscall.Stat_t
	if err := syscall.Stat(filePath, &st); err != nil {
		t.Fatal(err)
	}

	logicalSize := st.Size
	// st.Blocks is 512-byte blocks
	physicalSize := st.Blocks * 512

	t.Logf("Sparse Check: Logical=%d, Physical=%d", logicalSize, physicalSize)

	// If physical size is effectively full, sparse is not supported.
	// We allow some overhead, but 10MB logical should be << 10MB physical
	if int64(physicalSize) >= logicalSize {
		t.Log("Filesystem does not appear to support sparse files (Physical ~= Logical)")
		// Not a failure, but a capability gap.
		return
	}

	// 3. Verify SEEK_HOLE support (Linux 3.1+)
	// SEEK_HOLE constant is generally 4, but let's use the explicit value to be safe.
	const SEEK_HOLE = 4

	// We seek from offset 0. We expect a hole to start very soon (e.g. at 4096 or 0).
	holeOffset, _, errno := syscall.Syscall(syscall.SYS_LSEEK, f.Fd(), 0, uintptr(SEEK_HOLE))
	if errno != 0 {
		// errno is non-zero
		t.Logf("SEEK_HOLE not supported by syscall: %v", syscall.Errno(errno))
		return
	}

	if int64(holeOffset) >= logicalSize {
		t.Logf("SEEK_HOLE returned offset %d, expected < %d (treating as unsupported)", holeOffset, logicalSize)
	} else {
		t.Logf("SEEK_HOLE confirmed hole starting at %d", holeOffset)
	}
}

// --- File Locking ---

func testLocking(t *testing.T, dir string) {
	lockPath := filepath.Join(dir, "lock.test")
	f1, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lockPath)
	defer f1.Close()

	f2, err := os.OpenFile(lockPath, os.O_RDWR, 0666)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	// 1. Exclusive Lock Contention
	// Acquire Ex on f1
	if err := syscall.Flock(int(f1.Fd()), syscall.LOCK_EX); err != nil {
		if err == syscall.ENOLCK || err == syscall.EOPNOTSUPP {
			t.Skipf("Flock not supported: %v", err)
		}
		t.Fatalf("Failed to acquire LOCK_EX: %v", err)
	}

	// Attempt Ex on f2 (Should Fail/Block)
	// We use LOCK_NB to prevent hanging the test suite
	err = syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
		t.Errorf("Lock contention failed: Expected EWOULDBLOCK, got %v", err)
	}

	// 2. Unlock and Re-acquire
	syscall.Flock(int(f1.Fd()), syscall.LOCK_UN)

	err = syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		t.Errorf("Failed to acquire lock after release: %v", err)
	}

	// Clean up for next test
	syscall.Flock(int(f2.Fd()), syscall.LOCK_UN)

	// 3. Shared Lock Logic
	// Acquire SH on f1
	if err := syscall.Flock(int(f1.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	// Acquire SH on f2 (Should Succeed)
	if err := syscall.Flock(int(f2.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Errorf("Shared lock contention failed: %v", err)
	}
}

// --- Name Limits ---

func testNameLimits(t *testing.T, dir string) {
	// Query theoretical limit
	var nameMax int64 = 255 // Default fallback
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err == nil && fs.Namelen > 0 {
		nameMax = fs.Namelen
	}
	t.Logf("Reported NAME_MAX: %d", nameMax)

	// 1. Test Boundary (NAME_MAX) - Should Succeed
	longName := strings.Repeat("a", int(nameMax))
	f, err := os.Create(filepath.Join(dir, longName))
	if err != nil {
		if err == syscall.ENAMETOOLONG {
			t.Errorf("Reported limit %d is incorrect; creation failed", nameMax)
		} else {
			t.Fatalf("Unexpected error creating max-length file: %v", err)
		}
	} else {
		f.Close()
		os.Remove(f.Name())
	}

	// 2. Test Violation (NAME_MAX + 1) - Should Fail
	tooLong := strings.Repeat("a", int(nameMax)+1)
	_, err = os.Create(filepath.Join(dir, tooLong))
	if !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Errorf("Expected ENAMETOOLONG for len %d, got: %v", len(tooLong), err)
	}

	// 3. Discovery: Find actual limit if theoretical failed
	// This helps detect eCryptfs or other overlay overheads
	if t.Failed() {
		for l := int(nameMax); l > 0; l-- {
			name := strings.Repeat("b", l)
			if f, err := os.Create(filepath.Join(dir, name)); err == nil {
				f.Close()
				os.Remove(f.Name())
				t.Logf("Actual empirical NAME_MAX is: %d", l)
				break
			}
		}
	}
}

// --- Atomicity ---

func testAtomicity(t *testing.T, dir string) {
	target := filepath.Join(dir, "atomic_target")
	replacement := filepath.Join(dir, "atomic_replacement")

	// Create initial state
	if err := os.WriteFile(target, []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Reader Routine
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				data, err := os.ReadFile(target)
				if os.IsNotExist(err) {
					t.Error("Atomicity violation: File missing during rename")
					return
				}
				if len(data) == 0 {
					t.Error("Atomicity violation: Partial read (empty)")
					return
				}
			}
		}
	}()

	// Writer Routine (Flip-Flop Rename)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			// Write "B" to replacement
			if err := os.WriteFile(replacement, []byte("B"), 0644); err != nil {
				t.Error(err)
				return
			}
			// Atomic Swap
			if err := os.Rename(replacement, target); err != nil {
				t.Error(err)
				return
			}
		}
		close(stop)
	}()

	wg.Wait()
}

// --- Race Conditions ---

func testRaceConditions(t *testing.T, dir string) {
	raceDir := filepath.Join(dir, "race_test")
	if err := os.Mkdir(raceDir, 0755); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Creator
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 500; i++ {
			fname := filepath.Join(raceDir, fmt.Sprintf("%d", i))
			f, _ := os.Create(fname)
			if f != nil {
				f.Close()
			}
		}
	}()

	// Deleter
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 500; i++ {
			fname := filepath.Join(raceDir, fmt.Sprintf("%d", i))
			os.Remove(fname) // Ignore errors, file might not exist yet
		}
	}()

	// Lister (The Victim)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 50; i++ {
			// This syscall (getdents) is prone to races
			entries, err := os.ReadDir(raceDir)
			if err != nil {
				// While semantics vary, ReadDir generally shouldn't fail fatally
				// just because a file disappeared during iteration.
				// However, if the FS is fragile, this might error.
				t.Logf("ReadDir encountered error (warning): %v", err)
			}
			_ = entries
		}
	}()

	close(start)
	wg.Wait()
}

// --- VirtioFS Write/Read Test ---

func testVirtioFSWriteRead(t *testing.T, dir string) {
	filePath := filepath.Join(dir, "virtiofs_write_read_test")
	testData := []byte("virtiofs test data: Hello, World!")

	// Write data to file
	if err := os.WriteFile(filePath, testData, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	defer os.Remove(filePath)

	// Read data back
	readData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Validate contents
	if !bytes.Equal(readData, testData) {
		t.Errorf("Data corruption detected. Want %q, Got %q", testData, readData)
	}

	// Verify file size
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	if info.Size() != int64(len(testData)) {
		t.Errorf("File size mismatch. Want %d, Got %d", len(testData), info.Size())
	}
}

// --- VirtioFS Executable Test ---

func testVirtioFSExecutable(t *testing.T, dir string) {
	// Detect architecture
	var arch hv.CpuArchitecture
	switch runtime.GOARCH {
	case "amd64":
		arch = hv.ArchitectureX86_64
	case "arm64":
		arch = hv.ArchitectureARM64
	default:
		t.Skipf("Unsupported architecture: %s", runtime.GOARCH)
	}

	// Create a simple IR program that exits with code 42
	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Syscall(defs.SYS_EXIT, ir.Int64(42)),
			},
		},
	}

	// Build the standalone program
	asmProg, err := ir.BuildStandaloneProgramForArch(arch, prog)
	if err != nil {
		t.Fatalf("BuildStandaloneProgramForArch: %v", err)
	}

	// Generate ELF executable
	var elfBytes []byte
	switch arch {
	case hv.ArchitectureX86_64:
		elfBytes, err = amd64.StandaloneELF(asmProg)
	case hv.ArchitectureARM64:
		elfBytes, err = arm64.StandaloneELF(asmProg)
	default:
		t.Fatalf("Unsupported architecture: %v", arch)
	}
	if err != nil {
		t.Fatalf("StandaloneELF: %v", err)
	}

	// Write executable to file
	exePath := filepath.Join(dir, "virtiofs_executable_test")
	if err := os.WriteFile(exePath, elfBytes, 0755); err != nil {
		t.Fatalf("Failed to write executable: %v", err)
	}
	defer os.Remove(exePath)

	// Verify file is executable
	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatalf("Failed to stat executable: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("File is not executable. Mode: %o", info.Mode())
	}

	// Execute the program
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, exePath)
	err = cmd.Run()

	// Check exit code (should be 42)
	if exitError, ok := err.(*exec.ExitError); ok {
		status := exitError.Sys().(syscall.WaitStatus)
		exitCode := status.ExitStatus()
		if exitCode != 42 {
			t.Errorf("Executable exited with wrong code. Want 42, Got %d", exitCode)
		}
	} else if err != nil {
		t.Fatalf("Failed to execute program: %v", err)
	} else {
		// If err is nil, the program exited successfully with code 0
		// This shouldn't happen since we exit with 42
		t.Error("Executable exited with code 0, but expected 42")
	}
}

// --- Chmod Test ---

func testChmod(t *testing.T, dir string) {
	filePath := filepath.Join(dir, "chmod_test")
	
	// Create a file with non-executable permissions
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer os.Remove(filePath)

	// Verify initial permissions (should not be executable)
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	initialMode := info.Mode()
	if initialMode&0111 != 0 {
		t.Logf("File already has executable bits set: %o", initialMode)
	}

	// Chmod to make it executable (0755)
	if err := os.Chmod(filePath, 0755); err != nil {
		t.Fatalf("Failed to chmod file: %v", err)
	}

	// Verify permissions changed
	info, err = os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to stat file after chmod: %v", err)
	}
	newMode := info.Mode()
	if newMode&0111 == 0 {
		t.Errorf("File is not executable after chmod. Mode: %o", newMode)
	}
	if newMode&0755 != 0755 {
		t.Errorf("Unexpected file mode. Want 0755, Got %o", newMode&0777)
	}

	t.Logf("Chmod successful: %o -> %o", initialMode&0777, newMode&0777)
}

// --- Chown Test ---

func testChown(t *testing.T, dir string) {
	filePath := filepath.Join(dir, "chown_test")
	
	// Create a file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	defer os.Remove(filePath)

	// Get current UID and GID
	var st syscall.Stat_t
	if err := syscall.Stat(filePath, &st); err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	currentUID := int(st.Uid)
	currentGID := int(st.Gid)

	t.Logf("Current file ownership: UID=%d, GID=%d", currentUID, currentGID)

	// Try to chown to current user (should succeed)
	// This tests that chown works even when not changing ownership
	if err := os.Chown(filePath, currentUID, currentGID); err != nil {
		// Chown might fail on some filesystems (e.g., if not root)
		// But chowning to the same owner should generally work
		if err == syscall.EPERM || err == syscall.EACCES {
			t.Skipf("Chown requires elevated privileges: %v", err)
		}
		t.Fatalf("Failed to chown file to current user: %v", err)
	}

	// Verify ownership (should be unchanged, but chown should have succeeded)
	var st2 syscall.Stat_t
	if err := syscall.Stat(filePath, &st2); err != nil {
		t.Fatalf("Failed to stat file after chown: %v", err)
	}
	if int(st2.Uid) != currentUID {
		t.Errorf("UID mismatch after chown. Want %d, Got %d", currentUID, st2.Uid)
	}
	if int(st2.Gid) != currentGID {
		t.Errorf("GID mismatch after chown. Want %d, Got %d", currentGID, st2.Gid)
	}

	t.Logf("Chown successful: UID=%d, GID=%d", st2.Uid, st2.Gid)
}
