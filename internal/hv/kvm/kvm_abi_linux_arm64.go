//go:build linux && arm64

package kvm

const (
	kvmArmVcpuInitFeatureWords = 7
	kvmArmVcpuFeaturePsci02    = 2
)

type kvmVcpuInit struct {
	Target   uint32
	Features [kvmArmVcpuInitFeatureWords]uint32
}

type kvmOneReg struct {
	id   uint64
	addr uint64
}

type kvmIRQLevel struct {
	IRQOrStatus uint32
	Level       uint32
}
