package vm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"time"
)

// BenchmarkPass defines the type of I/O operation being benchmarked
type BenchmarkPass string

const (
	BenchmarkPassRead  BenchmarkPass = "read"
	BenchmarkPassWrite BenchmarkPass = "write"
)

// LatencyMeasurement represents a single latency measurement
type LatencyMeasurement struct {
	Offset   int64         `json:"offset"`
	Size     int64         `json:"size"`
	Latency  time.Duration `json:"latency_ns"`
	Pass     BenchmarkPass `json:"pass"` // "read" or "write"
	RunIndex int           `json:"run_index"`
}

// LatencyStats4 contains 4 statistical measurements for a set of latencies (in nanoseconds)
type LatencyStats4 [4]uint32 // [min, max, avg, stdev]

// CompactMeasurements stores all measurement data in parallel arrays for memory efficiency
type CompactMeasurements struct {
	Offsets    []int64         `json:"offsets"`     // Page offsets
	ReadStats  []LatencyStats4 `json:"read_stats"`  // [min, max, avg, stdev] for read latencies in nanoseconds
	WriteStats []LatencyStats4 `json:"write_stats"` // [min, max, avg, stdev] for write latencies in nanoseconds
	RunIndices []uint16        `json:"run_indices"` // Run index for each measurement (uint16 saves space)
}

// LatencyDataset contains complete benchmarking results with metadata
type LatencyDataset struct {
	Timestamp    time.Time            `json:"timestamp"`
	TotalSize    int64                `json:"total_size"`
	PageSize     uint32               `json:"page_size"`
	TotalRuns    int                  `json:"total_runs"`
	Statistics   LatencyStatistics    `json:"statistics"`
	CompactData  *CompactMeasurements `json:"compact_data,omitempty"` // Compact array format (default)
	Measurements []LatencyMeasurement `json:"measurements,omitempty"` // Legacy object format (only if requested)
}

// LatencyStatistics provides aggregate statistics for the latency measurements
type LatencyStatistics struct {
	ReadLatencies  LatencyStats `json:"read_latencies"`
	WriteLatencies LatencyStats `json:"write_latencies"`
}

// LatencyStats contains statistical measurements for a set of latencies
type LatencyStats struct {
	Mean   time.Duration `json:"mean_ns"`
	Median time.Duration `json:"median_ns"`
	Min    time.Duration `json:"min_ns"`
	Max    time.Duration `json:"max_ns"`
	P95    time.Duration `json:"p95_ns"`
	P99    time.Duration `json:"p99_ns"`
}

// BenchmarkFullCoverage performs full disk latency benchmarking with complete 4KB chunk coverage
func (vm *VirtualMemory) BenchmarkFullCoverage(pass BenchmarkPass, runIndex int) ([]LatencyMeasurement, error) {
	var measurements []LatencyMeasurement

	// Calculate total number of 4KB chunks
	totalChunks := vm.totalSize / int64(vm.pageSize)

	// Create buffers for I/O operations
	testBuffer := make([]byte, vm.pageSize)
	readBuffer := make([]byte, vm.pageSize)

	// Fill test buffer with a simple pattern
	for i := range testBuffer {
		testBuffer[i] = byte(i % 256)
	}

	// Create array of chunk indices for randomization
	chunkIndices := make([]int64, totalChunks)
	for i := range totalChunks {
		chunkIndices[i] = i
	}

	// Randomize the order of chunk access
	rand.Shuffle(int(totalChunks), func(i, j int) {
		chunkIndices[i], chunkIndices[j] = chunkIndices[j], chunkIndices[i]
	})

	// Visit every 4KB chunk in random order
	for _, chunkIndex := range chunkIndices {
		offset := chunkIndex * int64(vm.pageSize)

		var latency time.Duration
		var err error

		start := time.Now()

		switch pass {
		case BenchmarkPassRead:
			_, err = vm.ReadAt(readBuffer, offset)
		case BenchmarkPassWrite:
			_, err = vm.WriteAt(testBuffer, offset)
		default:
			return nil, fmt.Errorf("invalid benchmark pass: %s", pass)
		}

		latency = time.Since(start)

		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("%s error at offset %d: %w", pass, offset, err)
		}

		measurement := LatencyMeasurement{
			Offset:   offset,
			Size:     int64(vm.pageSize),
			Latency:  latency,
			Pass:     pass,
			RunIndex: runIndex,
		}

		measurements = append(measurements, measurement)
	}

	return measurements, nil
}

// BuildLatencyDataset creates a latency dataset with compact array format (default) or legacy object format
func BuildLatencyDataset(totalSize int64, pageSize uint32, totalRuns int, measurements []LatencyMeasurement, useCompact bool) *LatencyDataset {
	dataset := &LatencyDataset{
		Timestamp: time.Now(),
		TotalSize: totalSize,
		PageSize:  pageSize,
		TotalRuns: totalRuns,
	}

	// Calculate statistics
	dataset.Statistics = calculateLatencyStatistics(measurements)

	if useCompact {
		dataset.CompactData = buildCompactMeasurements(measurements, totalSize, pageSize)
	} else {
		dataset.Measurements = measurements
	}

	return dataset
}

// calculateLatencyStats4 computes min, max, avg, stdev for a set of latency measurements
func calculateLatencyStats4(measurements []LatencyMeasurement) LatencyStats4 {
	if len(measurements) == 0 {
		return LatencyStats4{0, 0, 0, 0}
	}

	// Convert to nanoseconds for calculations
	latencies := make([]uint64, len(measurements))
	for i, m := range measurements {
		latencies[i] = uint64(m.Latency.Nanoseconds())
	}

	// Find min and max, calculate sum for average
	min := latencies[0]
	max := latencies[0]
	var sum uint64

	for _, lat := range latencies {
		if lat < min {
			min = lat
		}
		if lat > max {
			max = lat
		}
		sum += lat
	}

	avg := sum / uint64(len(latencies))

	// Calculate standard deviation
	var sumSquaredDiffs uint64
	for _, lat := range latencies {
		diff := int64(lat) - int64(avg)
		sumSquaredDiffs += uint64(diff * diff)
	}
	variance := sumSquaredDiffs / uint64(len(latencies))
	stdev := uint64(math.Sqrt(float64(variance)))

	return LatencyStats4{
		uint32(min),
		uint32(max),
		uint32(avg),
		uint32(stdev),
	}
}

// buildCompactMeasurements converts measurements to compact array format for memory efficiency
func buildCompactMeasurements(measurements []LatencyMeasurement, totalSize int64, pageSize uint32) *CompactMeasurements {
	totalPages := int(totalSize / int64(pageSize))

	compact := &CompactMeasurements{
		Offsets:    make([]int64, 0, totalPages),
		ReadStats:  make([]LatencyStats4, 0, totalPages),
		WriteStats: make([]LatencyStats4, 0, totalPages),
		RunIndices: make([]uint16, 0, totalPages),
	}

	// Group measurements by offset to pair read/write operations
	measurementMap := make(map[int64]map[BenchmarkPass][]LatencyMeasurement)

	for _, m := range measurements {
		if measurementMap[m.Offset] == nil {
			measurementMap[m.Offset] = make(map[BenchmarkPass][]LatencyMeasurement)
		}
		measurementMap[m.Offset][m.Pass] = append(measurementMap[m.Offset][m.Pass], m)
	}

	// Build parallel arrays - one entry per page offset
	for offset := int64(0); offset < totalSize; offset += int64(pageSize) {
		passMeasurements := measurementMap[offset]
		if passMeasurements == nil {
			continue // Skip pages with no measurements
		}

		compact.Offsets = append(compact.Offsets, offset)

		// Calculate statistics for read times across all runs
		readStats := LatencyStats4{0, 0, 0, 0}
		if readMeasurements := passMeasurements[BenchmarkPassRead]; len(readMeasurements) > 0 {
			readStats = calculateLatencyStats4(readMeasurements)
		}
		compact.ReadStats = append(compact.ReadStats, readStats)

		// Calculate statistics for write times across all runs
		writeStats := LatencyStats4{0, 0, 0, 0}
		if writeMeasurements := passMeasurements[BenchmarkPassWrite]; len(writeMeasurements) > 0 {
			writeStats = calculateLatencyStats4(writeMeasurements)
		}
		compact.WriteStats = append(compact.WriteStats, writeStats)

		// Use the run index from the first measurement (they should all be similar for averaging)
		var runIndex uint16
		if len(passMeasurements[BenchmarkPassRead]) > 0 {
			runIndex = uint16(passMeasurements[BenchmarkPassRead][0].RunIndex)
		} else if len(passMeasurements[BenchmarkPassWrite]) > 0 {
			runIndex = uint16(passMeasurements[BenchmarkPassWrite][0].RunIndex)
		}
		compact.RunIndices = append(compact.RunIndices, runIndex)
	}

	return compact
}

// calculateLatencyStatistics computes aggregate statistics for the latency measurements
func calculateLatencyStatistics(measurements []LatencyMeasurement) LatencyStatistics {
	var readLatencies, writeLatencies []time.Duration

	// Separate measurements by pass type
	for _, m := range measurements {
		switch m.Pass {
		case BenchmarkPassRead:
			readLatencies = append(readLatencies, m.Latency)
		case BenchmarkPassWrite:
			writeLatencies = append(writeLatencies, m.Latency)
		}
	}

	return LatencyStatistics{
		ReadLatencies:  calculateStats(readLatencies),
		WriteLatencies: calculateStats(writeLatencies),
	}
}

// calculateStats computes statistical measures for a slice of durations
func calculateStats(durations []time.Duration) LatencyStats {
	if len(durations) == 0 {
		return LatencyStats{}
	}

	// Calculate mean and find min/max in single pass (O(n))
	var sum time.Duration
	min := durations[0]
	max := durations[0]

	for _, d := range durations {
		sum += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}

	stats := LatencyStats{
		Min:  min,
		Max:  max,
		Mean: sum / time.Duration(len(durations)),
	}

	// Only sort if we need percentiles (O(n log n) but much faster than bubble sort)
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Median
	n := len(sorted)
	if n%2 == 0 {
		stats.Median = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		stats.Median = sorted[n/2]
	}

	// Percentiles (more accurate calculation)
	p95Index := int(float64(n-1) * 0.95)
	stats.P95 = sorted[p95Index]

	p99Index := int(float64(n-1) * 0.99)
	stats.P99 = sorted[p99Index]

	return stats
}

// SaveLatencyDataset saves the latency dataset to a JSON file
func SaveLatencyDataset(dataset *LatencyDataset, filename string) error {
	file, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal latency dataset: %w", err)
	}

	return writeFile(filename, file, 0644)
}

// LoadLatencyDataset loads a latency dataset from a JSON file
func LoadLatencyDataset(filename string) (*LatencyDataset, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read latency file: %w", err)
	}

	var dataset LatencyDataset
	if err := json.Unmarshal(data, &dataset); err != nil {
		return nil, fmt.Errorf("failed to unmarshal latency dataset: %w", err)
	}

	return &dataset, nil
}

// Helper functions for file I/O (to avoid importing os in the vm package)
var (
	writeFile = func(filename string, data []byte, perm int) error {
		// This will be set by the importing package
		return fmt.Errorf("writeFile function not set")
	}
	readFile = func(filename string) ([]byte, error) {
		// This will be set by the importing package
		return nil, fmt.Errorf("readFile function not set")
	}
)

// SetFileIOFunctions sets the file I/O functions for the benchmarking module
func SetFileIOFunctions(write func(string, []byte, int) error, read func(string) ([]byte, error)) {
	writeFile = write
	readFile = read
}
