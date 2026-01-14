//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func install(staging, target string, ui *InstallerUI) error {
	// Find the new executable in staging
	newExe := ""
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read staging dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".exe" {
			newExe = filepath.Join(staging, e.Name())
			break
		}
	}
	if newExe == "" {
		// Try looking for a file without extension
		for _, e := range entries {
			if !e.IsDir() && e.Name() != "download" {
				newExe = filepath.Join(staging, e.Name())
				break
			}
		}
	}
	if newExe == "" {
		return fmt.Errorf("no executable found in staging directory")
	}

	ui.setStatus("Creating backup...")
	ui.setProgress(0.4)

	// Create backup
	backupPath := target + ".backup"
	if err := copyFile(target, backupPath); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}

	ui.setStatus("Waiting for application to close...")
	ui.setProgress(0.5)

	// Wait for the file to be unlocked and deletable (app may take time for cleanup/VM shutdown)
	if err := waitForFileDeletable(target, 60*time.Second); err != nil {
		os.Remove(backupPath)
		return fmt.Errorf("wait for file to become deletable: %w", err)
	}

	ui.setStatus("Removing old version...")
	ui.setProgress(0.6)

	// Remove old version
	if err := os.Remove(target); err != nil {
		os.Remove(backupPath)
		return fmt.Errorf("remove old version: %w", err)
	}

	ui.setStatus("Installing new version...")
	ui.setProgress(0.8)

	// Move new version into place
	if err := os.Rename(newExe, target); err != nil {
		// Try copy instead if rename fails (cross-device)
		if err := copyFile(newExe, target); err != nil {
			// Restore backup
			os.Rename(backupPath, target)
			return fmt.Errorf("install new version: %w", err)
		}
	}

	// Remove backup on success
	os.Remove(backupPath)

	return nil
}

func waitForFileDeletable(path string, timeout time.Duration) error {
	start := time.Now()
	for {
		// Try to open the file with read/write access - this is a reliable test for deletability
		// without actually modifying the file
		handle, err := syscall.CreateFile(
			syscall.StringToUTF16Ptr(path),
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0, // No sharing - ensures exclusive access
			nil,
			syscall.OPEN_EXISTING,
			syscall.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil {
			syscall.CloseHandle(handle)
			return nil
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for file to become deletable: %w", err)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func launchApp(target string) error {
	cmd := exec.Command(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd.Start()
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
