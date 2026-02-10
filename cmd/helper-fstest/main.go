// helper-fstest validates filesystem operations through the helper API.
// It spawns a cc-helper, creates an alpine instance, and exercises every
// filesystem operation via the public cc.Instance interface.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	cc "github.com/tinyrange/cc"
)

func main() {
	image := flag.String("image", "alpine", "OCI image reference")
	cacheDir := flag.String("cache-dir", "", "cache directory for OCI images")
	flag.Parse()

	if err := run(*image, *cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nAll filesystem tests passed.")
}

var (
	passed int
	failed int
)

func check(name string, err error) {
	if err != nil {
		fmt.Printf("  FAIL  %s: %v\n", name, err)
		failed++
	} else {
		fmt.Printf("  PASS  %s\n", name)
		passed++
	}
}

func run(imageRef, cacheDir string) error {
	// Ensure image is cached.
	fmt.Printf("Preparing image %q...\n", imageRef)
	var client cc.OCIClient
	var err error
	if cacheDir != "" {
		cache, err := cc.NewCacheDir(cacheDir)
		if err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
		client, err = cc.NewOCIClientWithCache(cache)
		if err != nil {
			return fmt.Errorf("create OCI client: %w", err)
		}
	} else {
		client, err = cc.NewOCIClient()
		if err != nil {
			return fmt.Errorf("create OCI client: %w", err)
		}
	}

	_, err = client.Pull(context.Background(), imageRef, cc.WithPullPolicy(cc.PullIfNotPresent))
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	// Spawn helper and create instance.
	fmt.Println("Spawning helper...")
	h, err := cc.SpawnHelper()
	if err != nil {
		return fmt.Errorf("spawn helper: %w", err)
	}
	defer h.Close()

	source, err := h.Pull(context.Background(), imageRef)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	inst, err := h.New(source)
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}
	defer inst.Close()

	fmt.Println("Running filesystem tests...")

	// Test: WriteFile + ReadFile roundtrip
	check("WriteFile+ReadFile", testWriteReadFile(inst))

	// Test: Stat
	check("Stat", testStat(inst))

	// Test: Lstat
	check("Lstat", testLstat(inst))

	// Test: Mkdir + ReadDir
	check("Mkdir+ReadDir", testMkdirReadDir(inst))

	// Test: Rename
	check("Rename", testRename(inst))

	// Test: Remove
	check("Remove", testRemove(inst))

	// Test: Symlink + Readlink
	check("Symlink+Readlink", testSymlinkReadlink(inst))

	// Test: Chmod
	check("Chmod", testChmod(inst))

	// Test: Chown
	check("Chown", testChown(inst))

	// Test: Chtimes
	check("Chtimes", testChtimes(inst))

	// Test: File handle ops (Open, Write, Seek, Read, Close)
	check("FileHandleOps", testFileHandleOps(inst))

	fmt.Printf("\nResults: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return fmt.Errorf("%d test(s) failed", failed)
	}
	return nil
}

func testWriteReadFile(inst cc.Instance) error {
	data := []byte("hello from fstest")
	path := "/root/fstest_write_read"

	if err := inst.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	got, err := inst.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ReadFile: %w", err)
	}
	if !bytes.Equal(got, data) {
		return fmt.Errorf("ReadFile: got %q, want %q", got, data)
	}
	return nil
}

func testStat(inst cc.Instance) error {
	fi, err := inst.Stat("/etc/hostname")
	if err != nil {
		return fmt.Errorf("Stat: %w", err)
	}
	if fi.Name() != "hostname" {
		return fmt.Errorf("Stat: name=%q, want %q", fi.Name(), "hostname")
	}
	if fi.IsDir() {
		return fmt.Errorf("Stat: IsDir=true, want false")
	}
	return nil
}

func testLstat(inst cc.Instance) error {
	fi, err := inst.Lstat("/etc")
	if err != nil {
		return fmt.Errorf("Lstat: %w", err)
	}
	if fi.Name() != "etc" {
		return fmt.Errorf("Lstat: name=%q, want %q", fi.Name(), "etc")
	}
	if !fi.IsDir() {
		return fmt.Errorf("Lstat: IsDir=false, want true")
	}
	return nil
}

func testMkdirReadDir(inst cc.Instance) error {
	dir := "/root/fstest_dir"

	if err := inst.Mkdir(dir, 0755); err != nil {
		return fmt.Errorf("Mkdir: %w", err)
	}

	if err := inst.WriteFile(dir+"/child.txt", []byte("child"), 0644); err != nil {
		return fmt.Errorf("WriteFile in dir: %w", err)
	}

	entries, err := inst.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("ReadDir: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("ReadDir: empty directory")
	}
	found := false
	for _, e := range entries {
		if e.Name() == "child.txt" {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("ReadDir: child.txt not found")
	}
	return nil
}

func testRename(inst cc.Instance) error {
	src := "/root/fstest_rename_src"
	dst := "/root/fstest_rename_dst"

	if err := inst.WriteFile(src, []byte("rename me"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	if err := inst.Rename(src, dst); err != nil {
		return fmt.Errorf("Rename: %w", err)
	}

	if _, err := inst.Stat(dst); err != nil {
		return fmt.Errorf("Stat after rename: %w", err)
	}
	return nil
}

func testRemove(inst cc.Instance) error {
	path := "/root/fstest_remove"

	if err := inst.WriteFile(path, []byte("remove me"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	if err := inst.Remove(path); err != nil {
		return fmt.Errorf("Remove: %w", err)
	}

	if _, err := inst.Stat(path); err == nil {
		return fmt.Errorf("Stat after Remove: expected error, got nil")
	}
	return nil
}

func testSymlinkReadlink(inst cc.Instance) error {
	target := "/root/fstest_symlink_target"
	link := "/root/fstest_symlink_link"

	if err := inst.WriteFile(target, []byte("target"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	if err := inst.Symlink(target, link); err != nil {
		return fmt.Errorf("Symlink: %w", err)
	}

	got, err := inst.Readlink(link)
	if err != nil {
		return fmt.Errorf("Readlink: %w", err)
	}
	if got != target {
		return fmt.Errorf("Readlink: got %q, want %q", got, target)
	}
	return nil
}

func testChmod(inst cc.Instance) error {
	path := "/root/fstest_chmod"

	if err := inst.WriteFile(path, []byte("chmod test"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	if err := inst.Chmod(path, 0755); err != nil {
		return fmt.Errorf("Chmod: %w", err)
	}

	fi, err := inst.Stat(path)
	if err != nil {
		return fmt.Errorf("Stat after Chmod: %w", err)
	}
	if fi.Mode().Perm() != 0755 {
		return fmt.Errorf("Chmod: mode=%o, want 0755", fi.Mode().Perm())
	}
	return nil
}

func testChown(inst cc.Instance) error {
	path := "/root/fstest_chown"

	if err := inst.WriteFile(path, []byte("chown test"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	if err := inst.Chown(path, 0, 0); err != nil {
		return fmt.Errorf("Chown: %w", err)
	}
	return nil
}

func testChtimes(inst cc.Instance) error {
	path := "/root/fstest_chtimes"

	if err := inst.WriteFile(path, []byte("chtimes test"), 0644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	t := time.Unix(1000000, 0)
	if err := inst.Chtimes(path, t, t); err != nil {
		return fmt.Errorf("Chtimes: %w", err)
	}

	fi, err := inst.Stat(path)
	if err != nil {
		return fmt.Errorf("Stat after Chtimes: %w", err)
	}
	if fi.ModTime().Unix() != 1000000 {
		return fmt.Errorf("Chtimes: ModTime=%d, want 1000000", fi.ModTime().Unix())
	}
	return nil
}

func testFileHandleOps(inst cc.Instance) error {
	path := "/root/fstest_handle_ops"

	f, err := inst.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("OpenFile: %w", err)
	}

	writeData := []byte("file handle test data")
	written, err := f.Write(writeData)
	if err != nil {
		f.Close()
		return fmt.Errorf("Write: %w", err)
	}
	if written != len(writeData) {
		f.Close()
		return fmt.Errorf("Write: wrote %d, want %d", written, len(writeData))
	}

	offset, err := f.Seek(0, io.SeekStart)
	if err != nil {
		f.Close()
		return fmt.Errorf("Seek: %w", err)
	}
	if offset != 0 {
		f.Close()
		return fmt.Errorf("Seek: offset=%d, want 0", offset)
	}

	readData := make([]byte, len(writeData))
	n, err := f.Read(readData)
	if err != nil {
		f.Close()
		return fmt.Errorf("Read: %w", err)
	}
	readData = readData[:n]
	if !bytes.Equal(readData, writeData) {
		f.Close()
		return fmt.Errorf("Read: got %q, want %q", readData, writeData)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("Close: %w", err)
	}

	return nil
}
