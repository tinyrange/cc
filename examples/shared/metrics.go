package shared

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects Prometheus-compatible metrics.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
	histos   map[string]*Histogram
}

// NewMetrics creates a new metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]*atomic.Int64),
		gauges:   make(map[string]*atomic.Int64),
		histos:   make(map[string]*Histogram),
	}
}

// Counter increments a counter metric.
func (m *Metrics) Counter(name string, delta int64) {
	m.mu.RLock()
	c, ok := m.counters[name]
	m.mu.RUnlock()

	if !ok {
		m.mu.Lock()
		if c, ok = m.counters[name]; !ok {
			c = &atomic.Int64{}
			m.counters[name] = c
		}
		m.mu.Unlock()
	}
	c.Add(delta)
}

// Gauge sets a gauge metric.
func (m *Metrics) Gauge(name string, value int64) {
	m.mu.RLock()
	g, ok := m.gauges[name]
	m.mu.RUnlock()

	if !ok {
		m.mu.Lock()
		if g, ok = m.gauges[name]; !ok {
			g = &atomic.Int64{}
			m.gauges[name] = g
		}
		m.mu.Unlock()
	}
	g.Store(value)
}

// Histogram records a histogram observation.
func (m *Metrics) Histogram(name string, value float64) {
	m.mu.RLock()
	h, ok := m.histos[name]
	m.mu.RUnlock()

	if !ok {
		m.mu.Lock()
		if h, ok = m.histos[name]; !ok {
			h = NewHistogram()
			m.histos[name] = h
		}
		m.mu.Unlock()
	}
	h.Observe(value)
}

// Timer returns a function that records duration when called.
func (m *Metrics) Timer(name string) func() {
	start := time.Now()
	return func() {
		m.Histogram(name, time.Since(start).Seconds())
	}
}

// Handler returns an HTTP handler for /metrics endpoint.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		var lines []string

		m.mu.RLock()
		for name, c := range m.counters {
			lines = append(lines, fmt.Sprintf("%s %d", name, c.Load()))
		}
		for name, g := range m.gauges {
			lines = append(lines, fmt.Sprintf("%s %d", name, g.Load()))
		}
		for name, h := range m.histos {
			sum, count := h.Stats()
			lines = append(lines, fmt.Sprintf("%s_sum %f", name, sum))
			lines = append(lines, fmt.Sprintf("%s_count %d", name, count))
		}
		m.mu.RUnlock()

		sort.Strings(lines)
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
	}
}

// Histogram is a simple histogram implementation.
type Histogram struct {
	mu    sync.Mutex
	sum   float64
	count int64
}

// NewHistogram creates a new histogram.
func NewHistogram() *Histogram {
	return &Histogram{}
}

// Observe records a value.
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	h.sum += value
	h.count++
	h.mu.Unlock()
}

// Stats returns sum and count.
func (h *Histogram) Stats() (float64, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum, h.count
}
