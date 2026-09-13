package x86state

// CPUIDEntry describes one architectural CPUID leaf and subleaf.
type CPUIDEntry struct {
	Function, Index, Flags uint32
	Eax, Ebx, Ecx, Edx     uint32
}
