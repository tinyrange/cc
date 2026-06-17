package builtin

import (
	"path/filepath"
	"reflect"
	"testing"

	managedguest "j5.nz/cc/internal/managed/guest"
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
