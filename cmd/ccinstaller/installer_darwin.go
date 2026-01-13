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

	ui.setStatus("Installing new version...")
	ui.setProgress(0.6)

	// Move new version to temporary location first
	tempTarget := target + ".new"
	if err := os.Rename(appBundle, tempTarget); err != nil {
		os.RemoveAll(backupPath)
		return fmt.Errorf("move new version: %w", err)
	}

	ui.setStatus("Removing old version...")
	ui.setProgress(0.7)

	// Atomic swap: remove old, rename new
	if err := os.RemoveAll(target); err != nil {
		os.Rename(tempTarget, appBundle) // restore
		os.RemoveAll(backupPath)
		return fmt.Errorf("remove old version: %w", err)
	}

	if err := os.Rename(tempTarget, target); err != nil {
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

	// Verify code signature of the newly installed app
	if err := verifyCodeSign(target); err != nil {
		ui.setStatus("Signature verification failed, rolling back...")

		// Rollback: remove failed install, then restore backup
		removeErr := os.RemoveAll(target)
		restoreErr := os.Rename(backupPath, target)
		if restoreErr != nil {
			return fmt.Errorf("signature verification failed (%w), rollback failed (remove: %v, restore: %v)", err, removeErr, restoreErr)
		}
		if removeErr != nil {
			// Restore succeeded despite remove failing (target may have been partially removed)
			return fmt.Errorf("signature verification failed (backup restored with warnings): %w", err)
		}
		return fmt.Errorf("signature verification failed (backup restored): %w", err)
	}

	// Remove backup on success (best effort)
	if err := os.RemoveAll(backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to remove backup after successful install: %v\n", err)
	}

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
