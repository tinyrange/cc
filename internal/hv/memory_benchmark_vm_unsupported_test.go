//go:build !(arm64 && (linux || darwin || windows))

package hv

func newAnonymousMemoryBenchmarkVM(memorySize uint64) (*memoryBenchmarkVM, error) {
	return nil, errMemoryBenchmarkUnsupported
}

func newMappedMemoryBenchmarkVM(mem []byte) (*memoryBenchmarkVM, error) {
	return nil, errMemoryBenchmarkUnsupported
}
