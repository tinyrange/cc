package vm

import (
	"strings"
	"testing"
)

func TestCheckManagedControlRequestCapabilities(t *testing.T) {
	denyCases := []struct {
		name string
		kind string
		caps guestCapabilities
		want string
	}{
		{name: "mkdir needs copy in", kind: "fs_mkdir", want: "copy into guest"},
		{name: "write needs copy in", kind: "fs_write", want: "copy into guest"},
		{name: "extract needs copy in first", kind: "fs_extract", want: "copy into guest"},
		{name: "extract needs archive", kind: "fs_extract", caps: guestCapabilities{CopyIn: true}, want: "archive extraction"},
		{name: "archive needs copy out", kind: "fs_archive", want: "copy out of guest"},
		{name: "unknown kind", kind: "unknown", caps: guestCapabilities{CopyIn: true, CopyOut: true, ArchiveExtract: true}, want: `does not support managed control request "unknown"`},
	}
	for _, tc := range denyCases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkManagedControlRequest("TestOS", tc.caps, tc.kind)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}

	allowCaps := guestCapabilities{CopyIn: true, CopyOut: true, ArchiveExtract: true}
	for _, kind := range []string{"", "exec", "sync", "fs_mkdir", "fs_write", "fs_extract", "fs_archive"} {
		if err := checkManagedControlRequest("TestOS", allowCaps, kind); err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
	}
}
