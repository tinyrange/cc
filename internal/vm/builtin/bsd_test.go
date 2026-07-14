package builtin

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/rootartifact"
)

func TestGuestForImageRecognizesBuiltinBSDProfiles(t *testing.T) {
	tests := []struct {
		image     string
		canonical string
		want      bool
	}{
		{image: "@openbsd", canonical: managedguest.OpenBSDImageName, want: true},
		{image: "openbsd", canonical: managedguest.OpenBSDImageName, want: true},
		{image: "@freebsd", canonical: managedguest.FreeBSDImageName, want: true},
		{image: "freebsd", canonical: managedguest.FreeBSDImageName, want: true},
		{image: "@netbsd", canonical: managedguest.NetBSDImageName, want: true},
		{image: "netbsd", canonical: managedguest.NetBSDImageName, want: true},
		{image: "alpine", want: false},
		{image: "", want: false},
	}
	for _, tc := range tests {
		profile, ok := GuestForImage(tc.image)
		if ok != tc.want {
			t.Fatalf("GuestForImage(%q) ok = %v, want %v", tc.image, ok, tc.want)
		}
		if !ok {
			continue
		}
		if profile.Canonical != tc.canonical {
			t.Fatalf("GuestForImage(%q) canonical = %q, want %q", tc.image, profile.Canonical, tc.canonical)
		}
	}
}

func TestBSDDefinitionsDescribeManagedGuests(t *testing.T) {
	cache := filepath.Join("tmp", "guestinit", "cc-guestinit")
	tests := []struct {
		name       string
		def        BSDDefinition
		canonical  string
		bootKind   string
		hostname   string
		iface      string
		cacheLeaf  string
		packageMgr string
	}{
		{
			name:       "openbsd",
			def:        OpenBSDDefinition(cache),
			canonical:  managedguest.OpenBSDImageName,
			bootKind:   "openbsd",
			hostname:   "cc-openbsd",
			iface:      "vio0",
			cacheLeaf:  "openbsd",
			packageMgr: "pkg_add",
		},
		{
			name:       "freebsd",
			def:        FreeBSDDefinition(cache),
			canonical:  managedguest.FreeBSDImageName,
			bootKind:   "freebsd",
			hostname:   "cc-freebsd",
			iface:      "vtnet0",
			cacheLeaf:  "freebsd",
			packageMgr: "pkg",
		},
		{
			name:       "netbsd",
			def:        NetBSDDefinition(cache),
			canonical:  managedguest.NetBSDImageName,
			bootKind:   "netbsd",
			hostname:   "cc-netbsd",
			iface:      "vioif0",
			cacheLeaf:  "netbsd",
			packageMgr: "pkg_add",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.def.Profile.Canonical != tc.canonical {
				t.Fatalf("canonical = %q, want %q", tc.def.Profile.Canonical, tc.canonical)
			}
			if tc.def.BootKind != tc.bootKind {
				t.Fatalf("boot kind = %q, want %q", tc.def.BootKind, tc.bootKind)
			}
			if tc.def.Hostname != tc.hostname {
				t.Fatalf("hostname = %q, want %q", tc.def.Hostname, tc.hostname)
			}
			if tc.def.Interface != tc.iface {
				t.Fatalf("interface = %q, want %q", tc.def.Interface, tc.iface)
			}
			if got := filepath.Base(tc.def.CacheDir); got != tc.cacheLeaf {
				t.Fatalf("cache dir leaf = %q, want %q", got, tc.cacheLeaf)
			}
			if tc.def.Profile.Caps.PackageManager != tc.packageMgr {
				t.Fatalf("package manager = %q, want %q", tc.def.Profile.Caps.PackageManager, tc.packageMgr)
			}
			if tc.def.BuildArtifact == nil {
				t.Fatalf("BuildArtifact is nil")
			}
		})
	}
}

func TestEffectiveExecEnvAppliesBSDDefaults(t *testing.T) {
	got := EffectiveExecEnv([]string{"PATH=/custom/bin", "EXTRA=1"}, []string{"TERM=vt220"}, false)
	want := []string{"PATH=/custom/bin", "HOME=/root", "TERM=vt220", "EXTRA=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged env = %#v, want %#v", got, want)
	}

	got = EffectiveExecEnv([]string{"EXTRA=1"}, []string{"TERM=vt220"}, true)
	want = []string{"PATH=/bin:/sbin:/usr/bin:/usr/sbin", "HOME=/root", "TERM=vt220"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement env = %#v, want %#v", got, want)
	}
}

func TestBSDArtifactWithCustomKernel(t *testing.T) {
	kernelPath := filepath.Join(t.TempDir(), "bsd-kernel")
	wantKernel := []byte("custom-bsd-kernel")
	if err := os.WriteFile(kernelPath, wantKernel, 0o644); err != nil {
		t.Fatalf("write custom kernel: %v", err)
	}
	artifact, err := bsdArtifactWithCustomKernel(rootartifact.Artifact{Kernel: []byte("packaged")}, customKernelFilePrefix+kernelPath)
	if err != nil {
		t.Fatalf("apply custom kernel: %v", err)
	}
	if !bytes.Equal(artifact.Kernel, wantKernel) {
		t.Fatalf("artifact kernel = %q, want custom kernel", artifact.Kernel)
	}
}

func TestBSDArtifactWithDefaultKernelKeepsPackagedKernel(t *testing.T) {
	packaged := []byte("packaged")
	artifact, err := bsdArtifactWithCustomKernel(rootartifact.Artifact{Kernel: packaged}, "default")
	if err != nil {
		t.Fatalf("apply default kernel: %v", err)
	}
	if !bytes.Equal(artifact.Kernel, packaged) {
		t.Fatalf("artifact kernel = %q, want packaged kernel", artifact.Kernel)
	}
}
