package hv

// Snapshot file format constants
const (
	SnapshotMagic   uint32 = 0x534e4150 // "SNAP"
	SnapshotVersion uint32 = 1
)

// Architecture encoding for snapshot files
const (
	SnapshotArchInvalid uint32 = 0
	SnapshotArchX86_64  uint32 = 1
	SnapshotArchARM64   uint32 = 2
	SnapshotArchRISCV64 uint32 = 3
)

// ArchToSnapshotArch converts a CpuArchitecture to its snapshot file encoding.
func ArchToSnapshotArch(arch CpuArchitecture) uint32 {
	switch arch {
	case ArchitectureX86_64:
		return SnapshotArchX86_64
	case ArchitectureARM64:
		return SnapshotArchARM64
	case ArchitectureRISCV64:
		return SnapshotArchRISCV64
	default:
		return SnapshotArchInvalid
	}
}

// SnapshotArchToArch converts a snapshot file architecture encoding to CpuArchitecture.
func SnapshotArchToArch(arch uint32) CpuArchitecture {
	switch arch {
	case SnapshotArchX86_64:
		return ArchitectureX86_64
	case SnapshotArchARM64:
		return ArchitectureARM64
	case SnapshotArchRISCV64:
		return ArchitectureRISCV64
	default:
		return ArchitectureInvalid
	}
}
