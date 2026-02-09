// helper-fstest validates filesystem operations through the IPC protocol.
// It spawns a cc-helper, creates an alpine instance, and exercises every
// filesystem-related IPC message type.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/ipc"
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
	c, err := ipc.SpawnHelper("")
	if err != nil {
		return fmt.Errorf("spawn helper: %w", err)
	}
	defer c.Close()

	if err := createInstance(c, imageRef, cacheDir); err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	fmt.Println("Running filesystem tests...")

	// Test: WriteFile + ReadFile roundtrip
	check("WriteFile+ReadFile", testWriteReadFile(c))

	// Test: Stat
	check("Stat", testStat(c))

	// Test: Lstat
	check("Lstat", testLstat(c))

	// Test: Mkdir + ReadDir
	check("Mkdir+ReadDir", testMkdirReadDir(c))

	// Test: Rename
	check("Rename", testRename(c))

	// Test: Remove
	check("Remove", testRemove(c))

	// Test: Symlink + Readlink
	check("Symlink+Readlink", testSymlinkReadlink(c))

	// Test: Chmod
	check("Chmod", testChmod(c))

	// Test: Chown
	check("Chown", testChown(c))

	// Test: Chtimes
	check("Chtimes", testChtimes(c))

	// Test: File handle ops (Open, Write, Seek, Read, Close)
	check("FileHandleOps", testFileHandleOps(c))

	fmt.Printf("\nResults: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return fmt.Errorf("%d test(s) failed", failed)
	}
	return nil
}

func createInstance(c *ipc.Client, imageRef, cacheDir string) error {
	enc := ipc.NewEncoder()
	enc.Uint8(2) // ref
	enc.String("")
	enc.String(imageRef)
	enc.String(cacheDir)
	ipc.EncodeInstanceOptions(enc, ipc.InstanceOptions{})

	resp, err := c.Call(ipc.MsgInstanceNew, enc.Bytes())
	if err != nil {
		return err
	}
	dec := ipc.NewDecoder(resp)
	code, err := dec.Uint8()
	if err != nil {
		return err
	}
	if code != ipc.ErrCodeOK {
		return fmt.Errorf("error code %d", code)
	}
	return nil
}

// callOK sends a message and checks the error code is 0.
func callOK(c *ipc.Client, msgType uint16, enc *ipc.Encoder) (*ipc.Decoder, error) {
	resp, err := c.Call(msgType, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec := ipc.NewDecoder(resp)
	code, err := dec.Uint8()
	if err != nil {
		return nil, err
	}
	if code != ipc.ErrCodeOK {
		return nil, fmt.Errorf("IPC error code %d", code)
	}
	return dec, nil
}

func testWriteReadFile(c *ipc.Client) error {
	data := []byte("hello from fstest")
	path := "/root/fstest_write_read"

	// WriteFile
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes(data)
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// ReadFile
	enc = ipc.NewEncoder()
	enc.String(path)
	dec, err := callOK(c, ipc.MsgFsReadFile, enc)
	if err != nil {
		return fmt.Errorf("ReadFile: %w", err)
	}
	got, err := dec.Bytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, data) {
		return fmt.Errorf("ReadFile: got %q, want %q", got, data)
	}
	return nil
}

func testStat(c *ipc.Client) error {
	enc := ipc.NewEncoder()
	enc.String("/etc/hostname")
	dec, err := callOK(c, ipc.MsgFsStat, enc)
	if err != nil {
		return fmt.Errorf("Stat: %w", err)
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return err
	}
	if fi.Name != "hostname" {
		return fmt.Errorf("Stat: name=%q, want %q", fi.Name, "hostname")
	}
	if fi.IsDir {
		return fmt.Errorf("Stat: IsDir=true, want false")
	}
	return nil
}

func testLstat(c *ipc.Client) error {
	enc := ipc.NewEncoder()
	enc.String("/etc")
	dec, err := callOK(c, ipc.MsgFsLstat, enc)
	if err != nil {
		return fmt.Errorf("Lstat: %w", err)
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return err
	}
	if fi.Name != "etc" {
		return fmt.Errorf("Lstat: name=%q, want %q", fi.Name, "etc")
	}
	if !fi.IsDir {
		return fmt.Errorf("Lstat: IsDir=false, want true")
	}
	return nil
}

func testMkdirReadDir(c *ipc.Client) error {
	dir := "/root/fstest_dir"

	// Mkdir
	enc := ipc.NewEncoder()
	enc.String(dir)
	enc.Uint32(uint32(fs.FileMode(0755)))
	if _, err := callOK(c, ipc.MsgFsMkdir, enc); err != nil {
		return fmt.Errorf("Mkdir: %w", err)
	}

	// Write a file inside
	enc = ipc.NewEncoder()
	enc.String(dir + "/child.txt")
	enc.WriteBytes([]byte("child"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile in dir: %w", err)
	}

	// ReadDir
	enc = ipc.NewEncoder()
	enc.String(dir)
	dec, err := callOK(c, ipc.MsgFsReadDir, enc)
	if err != nil {
		return fmt.Errorf("ReadDir: %w", err)
	}
	count, err := dec.Uint32()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("ReadDir: empty directory")
	}
	found := false
	for i := uint32(0); i < count; i++ {
		de, err := ipc.DecodeDirEntry(dec)
		if err != nil {
			return err
		}
		if de.Name == "child.txt" {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("ReadDir: child.txt not found")
	}
	return nil
}

func testRename(c *ipc.Client) error {
	src := "/root/fstest_rename_src"
	dst := "/root/fstest_rename_dst"

	// Create source file
	enc := ipc.NewEncoder()
	enc.String(src)
	enc.WriteBytes([]byte("rename me"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Rename
	enc = ipc.NewEncoder()
	enc.String(src)
	enc.String(dst)
	if _, err := callOK(c, ipc.MsgFsRename, enc); err != nil {
		return fmt.Errorf("Rename: %w", err)
	}

	// Verify destination exists
	enc = ipc.NewEncoder()
	enc.String(dst)
	if _, err := callOK(c, ipc.MsgFsStat, enc); err != nil {
		return fmt.Errorf("Stat after rename: %w", err)
	}
	return nil
}

func testRemove(c *ipc.Client) error {
	path := "/root/fstest_remove"

	// Create file
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes([]byte("remove me"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Remove
	enc = ipc.NewEncoder()
	enc.String(path)
	if _, err := callOK(c, ipc.MsgFsRemove, enc); err != nil {
		return fmt.Errorf("Remove: %w", err)
	}

	// Verify gone (Stat should fail)
	enc = ipc.NewEncoder()
	enc.String(path)
	_, err := c.Call(ipc.MsgFsStat, enc.Bytes())
	if err == nil {
		return fmt.Errorf("Stat after Remove: expected error, got nil")
	}
	return nil
}

func testSymlinkReadlink(c *ipc.Client) error {
	target := "/root/fstest_symlink_target"
	link := "/root/fstest_symlink_link"

	// Create target
	enc := ipc.NewEncoder()
	enc.String(target)
	enc.WriteBytes([]byte("target"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Symlink
	enc = ipc.NewEncoder()
	enc.String(target)
	enc.String(link)
	if _, err := callOK(c, ipc.MsgFsSymlink, enc); err != nil {
		return fmt.Errorf("Symlink: %w", err)
	}

	// Readlink
	enc = ipc.NewEncoder()
	enc.String(link)
	dec, err := callOK(c, ipc.MsgFsReadlink, enc)
	if err != nil {
		return fmt.Errorf("Readlink: %w", err)
	}
	got, err := dec.String()
	if err != nil {
		return err
	}
	if got != target {
		return fmt.Errorf("Readlink: got %q, want %q", got, target)
	}
	return nil
}

func testChmod(c *ipc.Client) error {
	path := "/root/fstest_chmod"

	// Create file
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes([]byte("chmod test"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Chmod
	enc = ipc.NewEncoder()
	enc.String(path)
	enc.Uint32(uint32(fs.FileMode(0755)))
	if _, err := callOK(c, ipc.MsgFsChmod, enc); err != nil {
		return fmt.Errorf("Chmod: %w", err)
	}

	// Verify
	enc = ipc.NewEncoder()
	enc.String(path)
	dec, err := callOK(c, ipc.MsgFsStat, enc)
	if err != nil {
		return fmt.Errorf("Stat after Chmod: %w", err)
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return err
	}
	if fi.Mode.Perm() != 0755 {
		return fmt.Errorf("Chmod: mode=%o, want 0755", fi.Mode.Perm())
	}
	return nil
}

func testChown(c *ipc.Client) error {
	path := "/root/fstest_chown"

	// Create file
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes([]byte("chown test"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Chown (set to root:root)
	enc = ipc.NewEncoder()
	enc.String(path)
	enc.Int32(0) // uid
	enc.Int32(0) // gid
	if _, err := callOK(c, ipc.MsgFsChown, enc); err != nil {
		return fmt.Errorf("Chown: %w", err)
	}
	return nil
}

func testChtimes(c *ipc.Client) error {
	path := "/root/fstest_chtimes"

	// Create file
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes([]byte("chtimes test"))
	enc.Uint32(uint32(fs.FileMode(0644)))
	if _, err := callOK(c, ipc.MsgFsWriteFile, enc); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}

	// Chtimes (set to Unix epoch 1000000)
	enc = ipc.NewEncoder()
	enc.String(path)
	enc.Int64(1000000) // atime
	enc.Int64(1000000) // mtime
	if _, err := callOK(c, ipc.MsgFsChtimes, enc); err != nil {
		return fmt.Errorf("Chtimes: %w", err)
	}

	// Verify
	enc = ipc.NewEncoder()
	enc.String(path)
	dec, err := callOK(c, ipc.MsgFsStat, enc)
	if err != nil {
		return fmt.Errorf("Stat after Chtimes: %w", err)
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return err
	}
	if fi.ModTime != 1000000 {
		return fmt.Errorf("Chtimes: ModTime=%d, want 1000000", fi.ModTime)
	}
	return nil
}

func testFileHandleOps(c *ipc.Client) error {
	path := "/root/fstest_handle_ops"

	// OpenFile (create+write)
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Int32(int32(os.O_CREATE | os.O_RDWR | os.O_TRUNC))
	enc.Uint32(uint32(fs.FileMode(0644)))
	dec, err := callOK(c, ipc.MsgFsOpenFile, enc)
	if err != nil {
		return fmt.Errorf("OpenFile: %w", err)
	}
	handle, err := dec.Uint64()
	if err != nil {
		return err
	}

	// FileWrite
	writeData := []byte("file handle test data")
	enc = ipc.NewEncoder()
	enc.Uint64(handle)
	enc.WriteBytes(writeData)
	dec, err = callOK(c, ipc.MsgFileWrite, enc)
	if err != nil {
		return fmt.Errorf("FileWrite: %w", err)
	}
	written, err := dec.Uint32()
	if err != nil {
		return err
	}
	if int(written) != len(writeData) {
		return fmt.Errorf("FileWrite: wrote %d, want %d", written, len(writeData))
	}

	// FileSeek back to start
	enc = ipc.NewEncoder()
	enc.Uint64(handle)
	enc.Int64(0)
	enc.Int32(0) // io.SeekStart
	dec, err = callOK(c, ipc.MsgFileSeek, enc)
	if err != nil {
		return fmt.Errorf("FileSeek: %w", err)
	}
	offset, err := dec.Int64()
	if err != nil {
		return err
	}
	if offset != 0 {
		return fmt.Errorf("FileSeek: offset=%d, want 0", offset)
	}

	// FileRead
	enc = ipc.NewEncoder()
	enc.Uint64(handle)
	enc.Uint32(uint32(len(writeData)))
	dec, err = callOK(c, ipc.MsgFileRead, enc)
	if err != nil {
		return fmt.Errorf("FileRead: %w", err)
	}
	readData, err := dec.Bytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(readData, writeData) {
		return fmt.Errorf("FileRead: got %q, want %q", readData, writeData)
	}

	// FileClose
	enc = ipc.NewEncoder()
	enc.Uint64(handle)
	if _, err := callOK(c, ipc.MsgFileClose, enc); err != nil {
		return fmt.Errorf("FileClose: %w", err)
	}

	return nil
}
