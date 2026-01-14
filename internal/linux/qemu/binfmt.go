package qemu

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// BinfmtConfig contains the binfmt-misc registration data for an architecture.
type BinfmtConfig struct {
	// Name is the registration name (e.g., "qemu-aarch64").
	Name string
	// Magic is the ELF magic bytes to match.
	Magic []byte
	// Mask is the mask for matching (0xff means exact match, 0x00 means ignore).
	Mask []byte
	// Interpreter is the path to the QEMU binary.
	Interpreter string
	// Flags are the binfmt-misc flags (typically "OCF").
	// O = preserve-argv0, C = credentials, F = fix-binary (keeps binary in memory)
	Flags string
}

// RegistrationString returns the string to write to /proc/sys/fs/binfmt_misc/register.
// Format: :name:type:offset:magic:mask:interpreter:flags
// Note: When offset is 0, we use :: (empty) instead of :0: for compatibility.
// Note: There is NO trailing colon after flags - the kernel parses until NUL.
func (c BinfmtConfig) RegistrationString() string {
	// Format: :name:M::magic:mask:interpreter:flags (no trailing colon)
	return fmt.Sprintf(":%s:M::%s:%s:%s:%s",
		c.Name,
		hexEscape(c.Magic),
		hexEscape(c.Mask),
		c.Interpreter,
		c.Flags,
	)
}

// hexEscape converts bytes to \xNN format for binfmt-misc.
func hexEscape(data []byte) string {
	result := ""
	for _, b := range data {
		result += fmt.Sprintf("\\x%02x", b)
	}
	return result
}

// binfmtConfigs contains the binfmt-misc registration data for each architecture.
// These are based on the standard QEMU binfmt configurations.
var binfmtConfigs = map[hv.CpuArchitecture]BinfmtConfig{
	hv.ArchitectureARM64: {
		Name: "qemu-aarch64",
		// Magic and mask from QEMU's qemu-binfmt-conf.sh
		Magic: []byte{
			0x7f, 0x45, 0x4c, 0x46, // ELF magic
			0x02,                   // ELF64
			0x01,                   // Little endian
			0x01,                   // ELF version
			0x00,                   // OS/ABI
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Padding
			0x02, 0x00, // Executable type
			0xb7, 0x00, // Machine: aarch64
		},
		Mask: []byte{
			0xff, 0xff, 0xff, 0xff, // ELF magic must match
			0xff,                   // ELF64 must match
			0xfe,                   // Little endian (allow 01 or 03)
			0xfe,                   // ELF version (allow 00 or 01)
			0x00,                   // Ignore OSABI
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // Padding mask from QEMU
			0xfe, 0xff, // Match ET_EXEC or ET_DYN
			0xff, 0xff, // Machine must match
		},
		Interpreter: "/usr/bin/qemu-aarch64-static",
		Flags:       "OC",
	},
	hv.ArchitectureX86_64: {
		Name: "qemu-x86_64",
		// Magic and mask from QEMU's qemu-binfmt-conf.sh
		Magic: []byte{
			0x7f, 0x45, 0x4c, 0x46, // ELF magic
			0x02,                   // ELF64
			0x01,                   // Little endian
			0x01,                   // ELF version
			0x00,                   // OS/ABI
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Padding
			0x02, 0x00, // Executable type
			0x3e, 0x00, // Machine: x86_64
		},
		Mask: []byte{
			0xff, 0xff, 0xff, 0xff, // ELF magic must match
			0xff,                   // ELF64 must match
			0xfe,                   // Little endian (allow 01 or 03)
			0xfe,                   // ELF version (allow 00 or 01)
			0x00,                   // Ignore OSABI
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // Padding mask from QEMU
			0xfe, 0xff, // Match ET_EXEC or ET_DYN
			0xff, 0xff, // Machine must match
		},
		Interpreter: "/usr/bin/qemu-x86_64-static",
		Flags:       "OC",
	},
}

// GetBinfmtConfig returns the binfmt-misc configuration for the given architecture.
func GetBinfmtConfig(arch hv.CpuArchitecture) (BinfmtConfig, bool) {
	cfg, ok := binfmtConfigs[arch]
	return cfg, ok
}
