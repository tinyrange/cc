//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

const (
	benchExitMemorySize = 16 << 20
	benchExitCodeAddr   = 0x200000
	benchRealCodeAddr   = 0x1000
	benchExitStackAddr  = 0x300000
	benchRealStackAddr  = 0x8000
	benchExitPagingAddr = 0x100000
	benchMMIOAddr       = 0x10000000
	benchIOPort         = 0x3f8
)

func BenchmarkKVMExitMMIOWrite(b *testing.B) {
	vm := newExitBenchmarkVM(b, mmioWriteLoop(benchMMIOAddr))
	defer vm.Close()
	benchmarkKVMExits(b, vm, ExitMMIO, func(exit *Exit) {
		if !exit.MMIO.Write || exit.MMIO.Addr != benchMMIOAddr || exit.MMIO.Len != 4 {
			b.Fatalf("MMIO exit = %+v, want 4-byte write to %#x", exit.MMIO, benchMMIOAddr)
		}
	})
}

func BenchmarkKVMExitMMIORead(b *testing.B) {
	vm := newExitBenchmarkVM(b, mmioReadLoop(benchMMIOAddr))
	defer vm.Close()
	benchmarkKVMExits(b, vm, ExitMMIO, func(exit *Exit) {
		if exit.MMIO.Write || exit.MMIO.Addr != benchMMIOAddr || exit.MMIO.Len != 4 {
			b.Fatalf("MMIO exit = %+v, want 4-byte read from %#x", exit.MMIO, benchMMIOAddr)
		}
		vm.CompleteMMIORead(0, exit.MMIO.Len)
	})
}

func BenchmarkKVMExitIOWrite(b *testing.B) {
	vm := newRealModeExitBenchmarkVM(b, ioWriteLoop(benchIOPort))
	defer vm.Close()
	benchmarkKVMExits(b, vm, ExitIO, func(exit *Exit) {
		if !exit.IO.Write || exit.IO.Port != benchIOPort || exit.IO.Size != 1 || exit.IO.Count != 1 {
			b.Fatalf("IO exit = %+v, want 1-byte write to %#x", exit.IO, benchIOPort)
		}
	})
}

func BenchmarkKVMExitIORead(b *testing.B) {
	vm := newRealModeExitBenchmarkVM(b, ioReadLoop(benchIOPort))
	defer vm.Close()
	benchmarkKVMExits(b, vm, ExitIO, func(exit *Exit) {
		if exit.IO.Write || exit.IO.Port != benchIOPort || exit.IO.Size != 1 || exit.IO.Count != 1 {
			b.Fatalf("IO exit = %+v, want 1-byte read from %#x", exit.IO, benchIOPort)
		}
		exit.IO.Data[0] = 0
	})
}

func newExitBenchmarkVM(b *testing.B, code []byte) *VM {
	b.Helper()
	vm, err := NewVM()
	if err != nil {
		if strings.Contains(err.Error(), "/dev/kvm") ||
			strings.Contains(err.Error(), "inappropriate ioctl") ||
			strings.Contains(err.Error(), "invalid argument") ||
			strings.Contains(err.Error(), "permission denied") {
			b.Skip(err)
		}
		b.Fatal(err)
	}
	if _, err := vm.MapAnonymousMemory(benchExitMemorySize, 0); err != nil {
		_ = vm.Close()
		b.Fatalf("MapAnonymousMemory() error = %v", err)
	}
	if err := vm.WriteIPA(benchExitCodeAddr, code); err != nil {
		_ = vm.Close()
		b.Fatalf("WriteIPA(code) error = %v", err)
	}
	if err := vm.SetLongMode(benchExitCodeAddr, 0, benchExitStackAddr, benchExitPagingAddr); err != nil {
		_ = vm.Close()
		b.Fatalf("SetLongMode() error = %v", err)
	}
	return vm
}

func newRealModeExitBenchmarkVM(b *testing.B, code []byte) *VM {
	b.Helper()
	vm, err := NewVM()
	if err != nil {
		if strings.Contains(err.Error(), "/dev/kvm") ||
			strings.Contains(err.Error(), "inappropriate ioctl") ||
			strings.Contains(err.Error(), "invalid argument") ||
			strings.Contains(err.Error(), "permission denied") {
			b.Skip(err)
		}
		b.Fatal(err)
	}
	if _, err := vm.MapAnonymousMemory(benchExitMemorySize, 0); err != nil {
		_ = vm.Close()
		b.Fatalf("MapAnonymousMemory() error = %v", err)
	}
	if err := vm.WriteIPA(benchRealCodeAddr, code); err != nil {
		_ = vm.Close()
		b.Fatalf("WriteIPA(code) error = %v", err)
	}
	sregs, err := getSRegs(vm.vcpufd)
	if err != nil {
		_ = vm.Close()
		b.Fatalf("getSRegs() error = %v", err)
	}
	codeSegment := kvmSegment{Base: 0, Limit: 0xffff, Type: 11, Present: 1, S: 1}
	dataSegment := kvmSegment{Base: 0, Limit: 0xffff, Type: 3, Present: 1, S: 1}
	sregs.Cs = codeSegment
	sregs.Ds = dataSegment
	sregs.Es = dataSegment
	sregs.Fs = dataSegment
	sregs.Gs = dataSegment
	sregs.Ss = dataSegment
	sregs.Cr0 &^= 1
	sregs.Cr4 = 0
	sregs.Efer = 0
	if err := setSRegs(vm.vcpufd, &sregs); err != nil {
		_ = vm.Close()
		b.Fatalf("setSRegs() error = %v", err)
	}
	if err := setRegs(vm.vcpufd, &kvmRegs{
		Rip:    benchRealCodeAddr,
		Rsp:    benchRealStackAddr,
		Rflags: 0x2,
	}); err != nil {
		_ = vm.Close()
		b.Fatalf("setRegs() error = %v", err)
	}
	return vm
}

func benchmarkKVMExits(b *testing.B, vm *VM, want ExitReason, handle func(*Exit)) {
	b.Helper()
	b.ReportAllocs()
	var exit Exit
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.Run(&exit); err != nil {
			b.Fatalf("Run() error = %v", err)
		}
		if exit.Reason != want {
			pc, _ := vm.GetPC()
			b.Fatalf("exit reason = %+v at pc=%#x, want %v", exit, pc, want)
		}
		handle(&exit)
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "exits/s")
}

func mmioWriteLoop(addr uint64) []byte {
	code := []byte{
		0x48, 0xbb, 0, 0, 0, 0, 0, 0, 0, 0, // movabs rbx, imm64
		0x89, 0x03, // mov dword ptr [rbx], eax
		0xeb, 0xfc, // jmp -4
	}
	binary.LittleEndian.PutUint64(code[2:10], addr)
	return code
}

func mmioReadLoop(addr uint64) []byte {
	code := []byte{
		0x48, 0xbb, 0, 0, 0, 0, 0, 0, 0, 0, // movabs rbx, imm64
		0x8b, 0x03, // mov eax, dword ptr [rbx]
		0xeb, 0xfc, // jmp -4
	}
	binary.LittleEndian.PutUint64(code[2:10], addr)
	return code
}

func ioWriteLoop(port uint16) []byte {
	if port > 0xffff {
		panic(fmt.Sprintf("port out of range: %#x", port))
	}
	code := []byte{
		0xba, 0, 0, // mov dx, imm16
		0xee,       // out dx, al
		0xeb, 0xfd, // jmp -3
	}
	binary.LittleEndian.PutUint16(code[1:3], port)
	return code
}

func ioReadLoop(port uint16) []byte {
	if port > 0xffff {
		panic(fmt.Sprintf("port out of range: %#x", port))
	}
	code := []byte{
		0xba, 0, 0, // mov dx, imm16
		0xec,       // in al, dx
		0xeb, 0xfd, // jmp -3
	}
	binary.LittleEndian.PutUint16(code[1:3], port)
	return code
}
