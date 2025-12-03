package kernel

import (
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

// test getting the kernel config
func TestKernelConfig(t *testing.T) {
	kernel, err := LoadForArchitecture(hv.ArchitectureX86_64)
	if err != nil {
		t.Fatalf("failed to load kernel: %v", err)
	}

	config, err := kernel.GetConfig()
	if err != nil {
		t.Fatalf("failed to get kernel config: %v", err)
	}

	if config["CONFIG_SMP"] != TristateYes {
		t.Errorf("expected CONFIG_SMP to be yes, got %v", config["CONFIG_SMP"])
	}

	if config["CONFIG_NONEXISTENT_OPTION"] != TristateNo {
		t.Errorf("expected CONFIG_NONEXISTENT_OPTION to be no, got %v", config["CONFIG_NONEXISTENT_OPTION"])
	}
}

// test planning module loads
func TestPlanModuleLoad(t *testing.T) {
	for _, arch := range []hv.CpuArchitecture{
		hv.ArchitectureX86_64,
		hv.ArchitectureARM64,
	} {
		t.Run(string(arch), func(t *testing.T) {
			kernel, err := LoadForArchitecture(arch)
			if err != nil {
				t.Fatalf("failed to load kernel: %v", err)
			}

			_, err = kernel.PlanModuleLoad(
				[]string{
					"CONFIG_VIRTIO_MMIO",
					"CONFIG_VIRTIO_BLK",
					"CONFIG_VIRTIO_NET",
					"CONFIG_VIRTIO_CONSOLE",
				},
				map[string]string{
					"CONFIG_VIRTIO_BLK":  "kernel/drivers/block/virtio_blk.ko.gz",
					"CONFIG_VIRTIO_NET":  "kernel/drivers/net/virtio_net.ko.gz",
					"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
				},
			)
			if err != nil {
				t.Fatalf("failed to plan module load: %v", err)
			}
		})
	}
}
