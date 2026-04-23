package amd64vm

import (
	"fmt"
	"strings"

	bootamd64 "j5.nz/cc/internal/linux/boot/amd64"
)

const (
	MemoryBase        = 0
	DefaultMemorySize = 512 << 20

	COM1Base = 0x3f8
	COM1IRQ  = 4
)

type BootConfig struct {
	MemoryMB uint64
	Dmesg    bool
}

func MemorySizeBytes(memoryMB uint64) uint64 {
	if memoryMB == 0 {
		return DefaultMemorySize
	}
	return memoryMB << 20
}

func BootCommandLine(dmesg bool) string {
	args := []string{
		"nokaslr",
		"panic=-1",
		"rdinit=/init",
	}
	if dmesg {
		args = append([]string{
			"console=ttyS0,115200n8",
			fmt.Sprintf("earlycon=uart8250,io,0x%x,115200n8", COM1Base),
			"keep_bootcon",
			"loglevel=8",
		}, args...)
	}
	return strings.Join(args, " ")
}

func PrepareBoot(memory []byte, kernel []byte, initrd []byte, cfg BootConfig) (*bootamd64.BootPlan, error) {
	return bootamd64.PrepareBoot(memory, kernel, bootamd64.BootOptions{
		MemoryBase: MemoryBase,
		MemorySize: MemorySizeBytes(cfg.MemoryMB),
		Initrd:     initrd,
		Cmdline:    BootCommandLine(cfg.Dmesg),
	})
}
