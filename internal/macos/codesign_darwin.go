//go:build darwin

package macos

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const resignedEnvVar = "CCX3_HYPERVISOR_SIGNED"

const hypervisorEntitlements = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.hypervisor</key>
	<true/>
</dict>
</plist>
`

func EnsureExecutableIsSigned() error {
	if os.Getenv(resignedEnvVar) == "1" {
		return nil
	}
	if hasHypervisorEntitlement() {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if err := signWithHypervisorEntitlement(exePath); err != nil {
		return fmt.Errorf("sign executable: %w", err)
	}

	env := append(os.Environ(), resignedEnvVar+"=1")
	return syscall.Exec(exePath, os.Args, env)
}

func hasHypervisorEntitlement() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}

	cmd := exec.Command("codesign", "-d", "--entitlements", "-", "--xml", exePath)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return bytes.Contains(output, []byte("com.apple.security.hypervisor"))
}

func signWithHypervisorEntitlement(exePath string) error {
	tmpFile, err := os.CreateTemp("", "ccx3-entitlements-*.plist")
	if err != nil {
		return fmt.Errorf("create temp entitlements file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(hypervisorEntitlements); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write entitlements: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close entitlements file: %w", err)
	}

	cmd := exec.Command("codesign", "-f", "-s", "-", "--entitlements", tmpFile.Name(), exePath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(output.String())
		if detail != "" {
			return fmt.Errorf("codesign failed: %w: %s", err, detail)
		}
		return fmt.Errorf("codesign failed: %w", err)
	}
	return nil
}
