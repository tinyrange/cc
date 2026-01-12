//go:build darwin

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func install(staging, target string, ui *InstallerUI) error {
	// On macOS, target should be the .app bundle path
	if !strings.HasSuffix(target, ".app") {
		return fmt.Errorf("target must be a .app bundle, got: %s", target)
	}

	// Find the .app in staging
	appBundle := ""
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read staging dir: %w", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".app") {
			appBundle = filepath.Join(staging, e.Name())
			break
		}
	}
	if appBundle == "" {
		return fmt.Errorf("no .app bundle found in staging directory")
	}

	ui.setStatus("Creating backup...")
	ui.setProgress(0.4)

	// Create backup
	backupPath := target + ".backup"
	if err := copyDir(target, backupPath); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}

	ui.setStatus("Removing old version...")
	ui.setProgress(0.5)

	// Remove old version
	if err := os.RemoveAll(target); err != nil {
		// Try to restore backup
		os.Rename(backupPath, target)
		return fmt.Errorf("remove old version: %w", err)
	}

	ui.setStatus("Installing new version...")
	ui.setProgress(0.7)

	// Move new version into place
	if err := os.Rename(appBundle, target); err != nil {
		// Restore backup
		os.Rename(backupPath, target)
		return fmt.Errorf("install new version: %w", err)
	}

	ui.setStatus("Removing quarantine...")
	ui.setProgress(0.8)

	// Remove quarantine attribute (in case downloaded from internet)
	exec.Command("xattr", "-rd", "com.apple.quarantine", target).Run()

	ui.setStatus("Verifying signature...")
	ui.setProgress(0.9)

	// Verify code signature
	if err := verifyCodeSign(target); err != nil {
		// Restore backup
		os.RemoveAll(target)
		os.Rename(backupPath, target)
		return fmt.Errorf("signature verification failed: %w", err)
	}

	// Remove backup on success
	os.RemoveAll(backupPath)

	return nil
}

func verifyCodeSign(appPath string) error {
	cmd := exec.Command("codesign", "--verify", "--deep", "--strict", appPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codesign verification: %w", err)
	}
	return nil
}

func launchApp(target string) error {
	return exec.Command("open", "-n", target).Start()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, dstPath)
		}

		return copyFile(path, dstPath)
	})
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
