//go:build darwin && arm64

package hvf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	freebsdarm64 "j5.nz/cc/internal/freebsd/boot/arm64"
	freebsdrootfs "j5.nz/cc/internal/freebsd/rootfs"
	netbsdarm64 "j5.nz/cc/internal/netbsd/boot/arm64"
	netbsdrootfs "j5.nz/cc/internal/netbsd/rootfs"
	"j5.nz/cc/internal/nvme"
	openbsdarm64 "j5.nz/cc/internal/openbsd/boot/arm64"
	openbsdrootfs "j5.nz/cc/internal/openbsd/rootfs"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
)

func TestDarwinHVFBSDGuestsAttachNVMeRoot(t *testing.T) {
	if os.Getenv("CC_TEST_DARWIN_HVF_BSD_NVME") == "" {
		t.Skip("set CC_TEST_DARWIN_HVF_BSD_NVME=1 to run Darwin HVF BSD NVMe boot tests")
	}

	for _, tc := range []struct {
		name       string
		memoryMB   uint64
		memoryBase uint64
		build      func(context.Context, *testing.T) ([]byte, virtio.BlockBackend)
		markers    []string
	}{
		{
			name:       "OpenBSD",
			memoryMB:   768,
			memoryBase: arm64vm.MemoryBase,
			build: func(ctx context.Context, t *testing.T) ([]byte, virtio.BlockBackend) {
				t.Helper()
				rt, err := openbsdrootfs.BuildManagedRuntime(ctx, openbsdrootfs.Config{
					CacheDir: filepath.Join(darwinHVFBSDTestCacheDir(t), "openbsd"),
					Arch:     "arm64",
				})
				if err != nil {
					t.Fatalf("build OpenBSD runtime: %v", err)
				}
				t.Cleanup(func() { _ = rt.Close() })
				return rt.Kernel, rt.Root
			},
			markers: []string{"nvme0 at pci0", "sd0 at scsibus0", "root on sd0a"},
		},
		{
			name:       "FreeBSD",
			memoryMB:   1024,
			memoryBase: arm64vm.MemoryBase,
			build: func(ctx context.Context, t *testing.T) ([]byte, virtio.BlockBackend) {
				t.Helper()
				rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{
					CacheDir: filepath.Join(darwinHVFBSDTestCacheDir(t), "freebsd"),
					Arch:     "arm64",
				})
				if err != nil {
					t.Fatalf("build FreeBSD runtime: %v", err)
				}
				t.Cleanup(func() { _ = rt.Close() })
				return rt.Kernel, rt.Root
			},
			markers: []string{"nvme0:", "nda0 at nvme0", "Trying to mount root from ufs:/dev/nda0"},
		},
		{
			name:       "NetBSD",
			memoryMB:   1024,
			memoryBase: netBSDArm64MemoryBase,
			build: func(ctx context.Context, t *testing.T) ([]byte, virtio.BlockBackend) {
				t.Helper()
				rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{
					CacheDir: filepath.Join(darwinHVFBSDTestCacheDir(t), "netbsd"),
					Arch:     "evbarm-aarch64",
				})
				if err != nil {
					t.Fatalf("build NetBSD runtime: %v", err)
				}
				t.Cleanup(func() { _ = rt.Close() })
				return rt.Kernel, rt.Root
			},
			markers: []string{"nvme0 at pci0", "ld4 at nvme0", "root on ld4a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			kernel, root := tc.build(ctx, t)
			serialOut, err := bootBSDArm64WithHVFNVMeRoot(ctx, tc.name, kernel, root, tc.memoryBase, tc.memoryMB, tc.markers)
			t.Logf("%s serial tail:\n%s", tc.name, tailString(serialOut, 8192))
			if err != nil {
				t.Fatalf("boot %s with NVMe root: %v\nserial:\n%s", tc.name, err, serialOut)
			}
		})
	}
}

func bootBSDArm64WithHVFNVMeRoot(ctx context.Context, guestName string, kernel []byte, root virtio.BlockBackend, memoryBase, memoryMB uint64, markers []string) (string, error) {
	vm, err := NewVMWithOptions(ctx, VMOptions{CPUs: 1})
	if err != nil {
		return "", err
	}
	defer vm.Close()

	memorySize := arm64vm.MemorySizeBytes(memoryMB)
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(memoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		return "", fmt.Errorf("map guest memory: %w", err)
	}
	serialOut := newSerialTranscript()
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialOut)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	if guestName == "NetBSD" {
		rng.LegacyMMIO = true
	}
	rng.Attach(vm, vm)
	ctrl := nvme.NewController(root)
	ctrl.Attach(vm, vm)
	pci := newHVFPCIHost(newHVFNVMePCIDevice(1, arm64vm.NVMeBase, arm64vm.NVMeIRQ, ctrl))
	extraNodes := []fdt.Node{pci.DeviceTreeNode(), rng.DeviceTreeNode()}

	switch guestName {
	case "OpenBSD":
		plan, err := openbsdarm64.PrepareBoot(mem, kernel, openbsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: openbsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: extraNodes,
		})
		if err != nil {
			return serialOut.String(), fmt.Errorf("prepare OpenBSD boot: %w", err)
		}
		if err := setupOpenBSDBootState(vm, plan); err != nil {
			return serialOut.String(), err
		}
	case "FreeBSD":
		plan, err := freebsdarm64.PrepareBoot(mem, kernel, freebsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: freebsdarm64.GICVersionV3,
			Console:    true,
			ExtraNodes: extraNodes,
		})
		if err != nil {
			return serialOut.String(), fmt.Errorf("prepare FreeBSD boot: %w", err)
		}
		if err := setupFreeBSDBootState(vm, plan); err != nil {
			return serialOut.String(), err
		}
	case "NetBSD":
		plan, err := netbsdarm64.PrepareBoot(mem, kernel, netbsdarm64.BootOptions{
			MemoryBase: memoryBase,
			MemorySize: memorySize,
			NumCPUs:    1,
			GICVersion: netbsdarm64.GICVersionV3,
			Console:    true,
			BootArgs:   "root=ld4a",
			ExtraNodes: extraNodes,
		})
		if err != nil {
			return serialOut.String(), fmt.Errorf("prepare NetBSD boot: %w", err)
		}
		if err := setupNetBSDBootState(vm, plan); err != nil {
			return serialOut.String(), err
		}
	default:
		return serialOut.String(), fmt.Errorf("unsupported BSD guest %q", guestName)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runBSDManagedVM(runCtx, vm, guestName, uart, nil, pci, nil, rng, serialOut)
	}()
	defer func() {
		cancel()
		_ = vm.CancelRun()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()

	_, err = serialOut.WaitFor(ctx, 0, func(text string) bool {
		for _, marker := range markers {
			if !strings.Contains(text, marker) {
				return false
			}
		}
		return true
	})
	if err == nil {
		return serialOut.String(), nil
	}
	select {
	case runErr := <-done:
		if runErr != nil {
			return serialOut.String(), runErr
		}
	default:
	}
	return serialOut.String(), err
}

func darwinHVFBSDTestCacheDir(t *testing.T) string {
	t.Helper()
	if cache := strings.TrimSpace(os.Getenv("CC_TEST_DARWIN_HVF_BSD_CACHE")); cache != "" {
		return cache
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "ccx3", "runtime", "hvf-bsd-nvme")
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
