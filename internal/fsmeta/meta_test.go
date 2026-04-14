package fsmeta

import (
	"archive/tar"
	"io/fs"
	"testing"
)

func TestNormalizeLinuxModeConvertsGoFileModeValues(t *testing.T) {
	tests := []struct {
		name     string
		stored   uint32
		fallback fs.FileMode
		want     uint32
	}{
		{name: "regular", stored: 0o755, fallback: 0o755, want: linuxSIFREG | 0o755},
		{name: "directory", stored: uint32(fs.ModeDir | 0o755), fallback: fs.ModeDir | 0o755, want: linuxSIFDIR | 0o755},
		{name: "symlink", stored: uint32(fs.ModeSymlink | 0o777), fallback: fs.ModeSymlink | 0o777, want: linuxSIFLNK | 0o777},
		{name: "linux-preserved", stored: linuxSIFCHR | 0o600, fallback: 0, want: linuxSIFCHR | 0o600},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeLinuxMode(tc.stored, tc.fallback); got != tc.want {
				t.Fatalf("NormalizeLinuxMode(%#o, %#o) = %#o, want %#o", tc.stored, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestLinuxModeFromTarHeaderUsesTarFileType(t *testing.T) {
	tests := []struct {
		typeflag byte
		mode     int64
		want     uint32
	}{
		{typeflag: tar.TypeReg, mode: 0o755, want: linuxSIFREG | 0o755},
		{typeflag: tar.TypeDir, mode: 0o755, want: linuxSIFDIR | 0o755},
		{typeflag: tar.TypeSymlink, mode: 0o777, want: linuxSIFLNK | 0o777},
		{typeflag: tar.TypeChar, mode: 0o600, want: linuxSIFCHR | 0o600},
	}
	for _, tc := range tests {
		hdr := &tar.Header{Name: "x", Typeflag: tc.typeflag, Mode: tc.mode}
		if got := LinuxModeFromTarHeader(hdr); got != tc.want {
			t.Fatalf("LinuxModeFromTarHeader(%d, %#o) = %#o, want %#o", tc.typeflag, tc.mode, got, tc.want)
		}
	}
}
