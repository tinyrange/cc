//go:build darwin

package api

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const alreadySignedEnvVar = "CC_HYPERVISOR_SIGNED"

// hypervisorEntitlements is the plist with the hypervisor entitlement.
const hypervisorEntitlements = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.hypervisor</key>
	<true/>
</dict>
</plist>
`

// EnsureExecutableIsSigned checks if the current executable is signed with
// the hypervisor entitlement. If not, it signs the executable and re-executes
// itself. This is useful for test binaries that need hypervisor access.
//
// Call this at the start of main() or TestMain(). If signing and re-exec
// succeed, this function does not return. If already signed, it returns nil.
//
// Example usage in TestMain:
//
//	func TestMain(m *testing.M) {
//	    if err := api.EnsureExecutableIsSigned(); err != nil {
//	        log.Fatalf("Failed to sign executable: %v", err)
//	    }
//	    os.Exit(m.Run())
//	}
func EnsureExecutableIsSigned() error {
	// Check if we've already re-exec'd
	if os.Getenv(alreadySignedEnvVar) == "1" {
		return nil
	}

	// Check if already signed with hypervisor entitlement
	if hasHypervisorEntitlement() {
		return nil
	}

	// Get the current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Sign the executable with hypervisor entitlement
	if err := signWithHypervisorEntitlement(exePath); err != nil {
		return fmt.Errorf("sign executable: %w", err)
	}

	// Re-exec with marker to prevent infinite loop
	env := append(os.Environ(), alreadySignedEnvVar+"=1")

	// Use syscall.Exec to replace the current process
	return syscall.Exec(exePath, os.Args, env)
}

// hasHypervisorEntitlement checks if the current executable is signed with
// the com.apple.security.hypervisor entitlement.
func hasHypervisorEntitlement() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}

	// Use codesign to check entitlements
	cmd := exec.Command("codesign", "-d", "--entitlements", "-", "--xml", exePath)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Check if the output contains the hypervisor entitlement
	return bytes.Contains(output, []byte("com.apple.security.hypervisor"))
}

// signWithHypervisorEntitlement signs the executable with the hypervisor entitlement.
func signWithHypervisorEntitlement(exePath string) error {
	// Create a temporary file for the entitlements plist
	tmpFile, err := os.CreateTemp("", "entitlements-*.plist")
	if err != nil {
		return fmt.Errorf("create temp entitlements file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(hypervisorEntitlements); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write entitlements: %w", err)
	}
	tmpFile.Close()

	// Sign with codesign command
	// -f: force (replace existing signature)
	// -s -: ad-hoc signing (no identity)
	// --entitlements: embed the entitlements
	cmd := exec.Command("codesign", "-f", "-s", "-", "--entitlements", tmpFile.Name(), exePath)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codesign failed: %w", err)
	}

	return nil
}
