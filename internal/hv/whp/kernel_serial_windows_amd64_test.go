//go:build windows && amd64

package whp

import (
	"context"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/linux/initramfs"
	"j5.nz/cc/internal/vmruntime"
)

func TestKernelBootFirstSerialByte(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), whpBootTestTimeout(t))
	defer cancel()
	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	serial, err := BootKernelToSerialWithTimeout(kernelFile, 256, true, whpBootTestTimeout(t))
	if err != nil {
		t.Fatalf("BootKernelToSerialWithTimeout() error = %v\nserial:\n%s", err, serial)
	}
	if serial == "" {
		t.Fatal("BootKernelToSerialWithTimeout() produced no serial output")
	}
	t.Logf("first serial output: %q", serial)
}

func TestInitramfsBootReadyMarker(t *testing.T) {
	if os.Getenv("CCX3_WHP_BOOT") == "" {
		t.Skip("set CCX3_WHP_BOOT=1 to run the windows amd64 WHP boot probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), whpBootTestTimeout(t))
	defer cancel()
	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	initrd, err := initramfs.Build([]initramfs.File{
		{Path: "/dev", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/dev/console", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 5, DevMinor: 1},
		{Path: "/init", Mode: 0o755, Data: tinyAMD64InitELF(vmruntime.InstanceReadyMarker + "\n"), Type: initramfs.TypeRegular},
	})
	if err != nil {
		t.Fatalf("build tiny initramfs: %v", err)
	}
	serial, err := BootInitramfsToMarker(ctx, kernelFile, initrd, 256, true, vmruntime.InstanceReadyMarker)
	if err != nil {
		t.Fatalf("BootInitramfsToMarker() error = %v\nserial:\n%s", err, serial)
	}
	if !strings.Contains(serial, vmruntime.InstanceReadyMarker) {
		t.Fatalf("serial missing ready marker %q:\n%s", vmruntime.InstanceReadyMarker, serial)
	}
}

func tinyAMD64InitELF(message string) []byte {
	const (
		base      = uint64(0x400000)
		headerLen = 64 + 56
	)
	path := []byte("/dev/console\x00")
	msg := []byte(message)
	code := []byte{
		0xb8, 0x01, 0x01, 0x00, 0x00, // mov eax, SYS_openat
		0xbf, 0x9c, 0xff, 0xff, 0xff, // mov edi, AT_FDCWD
		0x48, 0x8d, 0x35, 0, 0, 0, 0, // lea rsi, [rip+path]
		0xba, 0x02, 0x00, 0x00, 0x00, // mov edx, O_RDWR
		0x45, 0x31, 0xd2, // xor r10d, r10d
		0x0f, 0x05, // syscall
		0x48, 0x89, 0xc7, // mov rdi, rax
		0xb8, 0x01, 0x00, 0x00, 0x00, // mov eax, SYS_write
		0x48, 0x8d, 0x35, 0, 0, 0, 0, // lea rsi, [rip+msg]
		0xba, byte(len(msg)), byte(len(msg) >> 8), byte(len(msg) >> 16), byte(len(msg) >> 24), // mov edx, len
		0x0f, 0x05, // syscall
		0xf3, 0x90, // pause
		0xeb, 0xfc, // jmp pause
	}
	pathOffset := headerLen + len(code)
	msgOffset := pathOffset + len(path)
	binary.LittleEndian.PutUint32(code[13:17], uint32(pathOffset-(headerLen+17)))
	binary.LittleEndian.PutUint32(code[38:42], uint32(msgOffset-(headerLen+42)))

	filesz := uint64(headerLen + len(code) + len(path) + len(msg))
	elf := make([]byte, filesz)
	copy(elf[0:16], []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:18], 2)    // ET_EXEC
	binary.LittleEndian.PutUint16(elf[18:20], 0x3e) // EM_X86_64
	binary.LittleEndian.PutUint32(elf[20:24], 1)    // EV_CURRENT
	binary.LittleEndian.PutUint64(elf[24:32], base+headerLen)
	binary.LittleEndian.PutUint64(elf[32:40], 64) // program header offset
	binary.LittleEndian.PutUint16(elf[52:54], 64)
	binary.LittleEndian.PutUint16(elf[54:56], 56)
	binary.LittleEndian.PutUint16(elf[56:58], 1)

	ph := elf[64 : 64+56]
	binary.LittleEndian.PutUint32(ph[0:4], 1) // PT_LOAD
	binary.LittleEndian.PutUint32(ph[4:8], 5) // PF_R | PF_X
	binary.LittleEndian.PutUint64(ph[16:24], base)
	binary.LittleEndian.PutUint64(ph[24:32], base)
	binary.LittleEndian.PutUint64(ph[32:40], filesz)
	binary.LittleEndian.PutUint64(ph[40:48], filesz)
	binary.LittleEndian.PutUint64(ph[48:56], 0x1000)

	copy(elf[headerLen:], code)
	copy(elf[pathOffset:], path)
	copy(elf[msgOffset:], msg)
	return elf
}
