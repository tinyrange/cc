//go:build linux && amd64

package vm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"j5.nz/cc/client"
	freebsdrootfs "j5.nz/cc/internal/freebsd/rootfs"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	netbsdrootfs "j5.nz/cc/internal/netbsd/rootfs"
	openbsdrootfs "j5.nz/cc/internal/openbsd/rootfs"
	"j5.nz/cc/internal/vm/builtin"
)

func TestRuntimeBootsTrimmedBSDSourceCachePackageManagers(t *testing.T) {
	cacheDir := os.Getenv("CC_TEST_TRIMMED_BSD_CACHE")
	if cacheDir == "" {
		t.Skip("set CC_TEST_TRIMMED_BSD_CACHE to a trimmed BSD source cache")
	}
	cacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		t.Fatalf("resolve trimmed BSD cache: %v", err)
	}
	if err := Supports(); err != nil {
		t.Skipf("VM runtime unsupported on this host: %v", err)
	}
	tests := []struct {
		name     string
		def      builtin.BSDDefinition
		memoryMB uint64
		command  []string
	}{
		{
			name:     "OpenBSD",
			def:      trimmedOpenBSDDefinition(cacheDir),
			memoryMB: 768,
			command:  []string{"sh", "-c", "set -eu; pkg_info -q >/tmp/pkg-list; cc --version >/tmp/cc-version; test -s /tmp/cc-version"},
		},
		{
			name:     "FreeBSD",
			def:      trimmedFreeBSDDefinition(cacheDir),
			memoryMB: 1024,
			command:  []string{"sh", "-c", "set -eu; pkg -v >/tmp/pkg-version; test -s /tmp/pkg-version; cc --version >/tmp/cc-version; test -s /tmp/cc-version"},
		},
		{
			name:     "NetBSD",
			def:      trimmedNetBSDDefinition(cacheDir),
			memoryMB: 1024,
			command:  []string{"sh", "-c", "set -eu; pkg_info -V >/tmp/pkg-version; test -s /tmp/pkg-version; cc --version >/tmp/cc-version; test -s /tmp/cc-version"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			backend := NewRuntimeBackend(nil, nil, filepath.Join(t.TempDir(), "guestinit")).(*runtimeBackend)
			inst, err := backend.startBSDManagedStream(ctx, client.CreateInstanceRequest{
				ID:       "trimmed-" + tc.def.BootKind,
				Image:    tc.def.Profile.Canonical,
				MemoryMB: tc.memoryMB,
			}, nil, tc.def)
			if err != nil {
				t.Fatalf("boot trimmed %s: %v", tc.name, err)
			}
			defer inst.Close()
			resp, err := inst.Exec(ctx, client.ExecRequest{Command: tc.command, WorkDir: "/tmp"})
			if err != nil {
				t.Fatalf("run %s package manager check: %v", tc.name, err)
			}
			if resp.ExitCode != 0 {
				t.Fatalf("%s package manager check exit=%d output=%q", tc.name, resp.ExitCode, resp.Output)
			}
		})
	}
}

func trimmedOpenBSDDefinition(cacheDir string) builtin.BSDDefinition {
	def := builtin.OpenBSDDefinition("")
	def.CacheDir = cacheDir
	def.BuildArtifact = func(ctx context.Context, _, _ string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
		rt, err := openbsdrootfs.BuildManagedRuntime(ctx, openbsdrootfs.Config{CacheDir: cacheDir, Network: network})
		if err != nil {
			return rootartifact.Artifact{}, err
		}
		return rt.Artifact(), nil
	}
	return def
}

func trimmedFreeBSDDefinition(cacheDir string) builtin.BSDDefinition {
	def := builtin.FreeBSDDefinition("")
	def.CacheDir = cacheDir
	def.BuildArtifact = func(ctx context.Context, _, _ string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
		rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{CacheDir: cacheDir, Network: network})
		if err != nil {
			return rootartifact.Artifact{}, err
		}
		return rt.Artifact(), nil
	}
	return def
}

func trimmedNetBSDDefinition(cacheDir string) builtin.BSDDefinition {
	def := builtin.NetBSDDefinition("")
	def.CacheDir = cacheDir
	def.BuildArtifact = func(ctx context.Context, _, _ string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
		rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{CacheDir: cacheDir, Network: network})
		if err != nil {
			return rootartifact.Artifact{}, err
		}
		return rt.Artifact(), nil
	}
	return def
}
