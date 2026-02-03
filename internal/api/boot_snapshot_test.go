package api

import (
	"testing"
	"time"
)

// Test CacheDir.BootSnapshotPath()
func TestCacheDirBootSnapshotPath(t *testing.T) {
	cacheDir := t.TempDir()
	cache, err := NewCacheDir(cacheDir)
	if err != nil {
		t.Fatalf("NewCacheDir: %v", err)
	}

	bootSnapPath := cache.BootSnapshotPath()
	if bootSnapPath == "" {
		t.Error("BootSnapshotPath returned empty string")
	}

	// Should be a subdirectory of the cache dir
	expected := cacheDir + "/boot-snapshots"
	if bootSnapPath != expected {
		t.Errorf("BootSnapshotPath = %q, want %q", bootSnapPath, expected)
	}
}

// Test option parsing for boot snapshot options
func TestBootSnapshotOptionParsing(t *testing.T) {
	tests := []struct {
		name            string
		opts            []Option
		hasCache        bool
		wantEnabled     bool
		wantDescription string
	}{
		{
			name:            "no options - no cache",
			opts:            []Option{},
			hasCache:        false,
			wantEnabled:     false,
			wantDescription: "should be disabled without cache",
		},
		{
			name:            "cache only - disabled by default",
			opts:            []Option{},
			hasCache:        true,
			wantEnabled:     false,
			wantDescription: "should be disabled by default (until vsock reconnection is implemented)",
		},
		{
			name:            "explicit enable with cache",
			opts:            []Option{&bootSnapshotTestOption{enabled: true}},
			hasCache:        true,
			wantEnabled:     true,
			wantDescription: "should be enabled when explicitly set",
		},
		{
			name:            "explicit disable with cache",
			opts:            []Option{&bootSnapshotTestOption{enabled: false}},
			hasCache:        true,
			wantEnabled:     false,
			wantDescription: "should be disabled when explicitly set to false",
		},
		{
			name:            "explicit enable without cache",
			opts:            []Option{&bootSnapshotTestOption{enabled: true}},
			hasCache:        false,
			wantEnabled:     true,
			wantDescription: "explicit enable should work even without cache",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts

			// Add cache option if needed
			if tt.hasCache {
				cacheDir := t.TempDir()
				cache, err := NewCacheDir(cacheDir)
				if err != nil {
					t.Fatalf("NewCacheDir: %v", err)
				}
				opts = append(opts, &cacheTestOption{cache: cache})
			}

			cfg := parseInstanceOptions(opts)

			if cfg.bootSnapshotEnabled != tt.wantEnabled {
				t.Errorf("%s: bootSnapshotEnabled = %v, want %v",
					tt.wantDescription, cfg.bootSnapshotEnabled, tt.wantEnabled)
			}
		})
	}
}

// Test option types for testing
type bootSnapshotTestOption struct{ enabled bool }

func (*bootSnapshotTestOption) IsOption()           {}
func (o *bootSnapshotTestOption) BootSnapshot() bool { return o.enabled }

type cacheTestOption struct{ cache CacheDir }

func (*cacheTestOption) IsOption()            {}
func (o *cacheTestOption) Cache() CacheDir { return o.cache }

// Test BootStats type
func TestBootStatsType(t *testing.T) {
	stats := &BootStats{
		ColdBoot:            true,
		SnapshotRestoreTime: 0,
		KernelBootTime:      100 * time.Millisecond,
		ContainerInitTime:   50 * time.Millisecond,
		TotalBootTime:       150 * time.Millisecond,
	}

	if !stats.ColdBoot {
		t.Error("ColdBoot should be true")
	}
	if stats.SnapshotRestoreTime != 0 {
		t.Error("SnapshotRestoreTime should be 0 for cold boot")
	}
	if stats.KernelBootTime != 100*time.Millisecond {
		t.Errorf("KernelBootTime = %v, want 100ms", stats.KernelBootTime)
	}
	if stats.ContainerInitTime != 50*time.Millisecond {
		t.Errorf("ContainerInitTime = %v, want 50ms", stats.ContainerInitTime)
	}
	if stats.TotalBootTime != 150*time.Millisecond {
		t.Errorf("TotalBootTime = %v, want 150ms", stats.TotalBootTime)
	}

	// Test warm boot stats
	warmStats := &BootStats{
		ColdBoot:            false,
		SnapshotRestoreTime: 20 * time.Millisecond,
		KernelBootTime:      0,
		ContainerInitTime:   50 * time.Millisecond,
		TotalBootTime:       70 * time.Millisecond,
	}

	if warmStats.ColdBoot {
		t.Error("ColdBoot should be false for warm boot")
	}
	if warmStats.SnapshotRestoreTime != 20*time.Millisecond {
		t.Errorf("SnapshotRestoreTime = %v, want 20ms", warmStats.SnapshotRestoreTime)
	}
	if warmStats.KernelBootTime != 0 {
		t.Error("KernelBootTime should be 0 for warm boot")
	}
}
