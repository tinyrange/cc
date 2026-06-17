//go:build linux && amd64

package vm

import (
	"testing"

	managedguest "j5.nz/cc/internal/managed/guest"
)

func TestBuiltinGuestForImage(t *testing.T) {
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
		profile, ok := builtinGuestForImage(tc.image)
		if ok != tc.want {
			t.Fatalf("builtinGuestForImage(%q) ok = %v, want %v", tc.image, ok, tc.want)
		}
		if !ok {
			continue
		}
		if profile.Canonical != tc.canonical {
			t.Fatalf("builtinGuestForImage(%q) canonical = %q, want %q", tc.image, profile.Canonical, tc.canonical)
		}
	}
}
