//go:build !(arm64 && (darwin || linux || windows))

package hv

func mapBenchmarkGuestFile(path string, size int) (benchmarkGuestMapping, error) {
	return nil, errMemoryBenchmarkUnsupported
}
