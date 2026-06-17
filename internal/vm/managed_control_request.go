package vm

import (
	"fmt"
	"strings"
)

func checkManagedControlRequest(osName string, caps guestCapabilities, kind string) error {
	display := managedDisplayName(osName)
	unsupported := func(feature string) error {
		return unsupportedManagedFeature(display, caps, feature)
	}
	switch strings.TrimSpace(kind) {
	case "", "exec", "sync":
		return nil
	case "fs_mkdir", "fs_write":
		if caps.CopyIn {
			return nil
		}
		return unsupported("copy into guest")
	case "fs_extract":
		if caps.CopyIn && caps.ArchiveExtract {
			return nil
		}
		if !caps.CopyIn {
			return unsupported("copy into guest")
		}
		return unsupported("archive extraction")
	case "fs_archive":
		if caps.CopyOut {
			return nil
		}
		return unsupported("copy out of guest")
	default:
		return fmt.Errorf("%s runtime does not support managed control request %q", display, kind)
	}
}

func managedDisplayName(osName string) string {
	if strings.TrimSpace(osName) == "" {
		return "managed guest"
	}
	return osName
}
