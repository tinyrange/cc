package guest

import "testing"

func TestBuiltinBSDProfileForImage(t *testing.T) {
	tests := []struct {
		image     string
		canonical string
		want      bool
	}{
		{image: "@openbsd", canonical: OpenBSDImageName, want: true},
		{image: "openbsd", canonical: OpenBSDImageName, want: true},
		{image: "@freebsd", canonical: FreeBSDImageName, want: true},
		{image: "freebsd", canonical: FreeBSDImageName, want: true},
		{image: "alpine", want: false},
		{image: "", want: false},
	}
	for _, tc := range tests {
		profile, ok := BuiltinBSDProfileForImage(tc.image)
		if ok != tc.want {
			t.Fatalf("BuiltinBSDProfileForImage(%q) ok = %v, want %v", tc.image, ok, tc.want)
		}
		if !ok {
			continue
		}
		if profile.Canonical != tc.canonical {
			t.Fatalf("BuiltinBSDProfileForImage(%q) canonical = %q, want %q", tc.image, profile.Canonical, tc.canonical)
		}
	}
}

func TestBSDProfileCapabilities(t *testing.T) {
	openbsd := OpenBSDProfile.Caps
	if !openbsd.PersistentExec || !openbsd.CopyIn || !openbsd.CopyOut || openbsd.TTY || openbsd.ResizeTTY {
		t.Fatalf("OpenBSD capabilities = %+v", openbsd)
	}
	if openbsd.PackageManager != "pkg_add" {
		t.Fatalf("OpenBSD package manager = %q", openbsd.PackageManager)
	}

	freebsd := FreeBSDProfile.Caps
	if !freebsd.PersistentExec || !freebsd.CopyIn || !freebsd.CopyOut || !freebsd.TTY || !freebsd.ResizeTTY {
		t.Fatalf("FreeBSD capabilities = %+v", freebsd)
	}
	if freebsd.PackageManager != "pkg" {
		t.Fatalf("FreeBSD package manager = %q", freebsd.PackageManager)
	}
	if freebsd.DynamicShares || freebsd.PortForward || freebsd.AlternateImageExec {
		t.Fatalf("FreeBSD unsupported capabilities should be false: %+v", freebsd)
	}
	if !openbsd.RootSnapshot || openbsd.ImageSnapshot {
		t.Fatalf("OpenBSD snapshot capabilities = %+v", openbsd)
	}
	if !freebsd.RootSnapshot || freebsd.ImageSnapshot {
		t.Fatalf("FreeBSD snapshot capabilities = %+v", freebsd)
	}
}
