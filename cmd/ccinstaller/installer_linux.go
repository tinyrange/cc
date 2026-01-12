//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func install(staging, target string, ui *InstallerUI) error {
	// Find the new executable in staging
	newExe := ""
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read staging dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "download" {
			newExe = filepath.Join(staging, e.Name())
			break
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

	ui.setStatus("Installing new version...")
	ui.setProgress(0.7)

	// Atomic replacement using rename
	// First copy to a temp file next to target, then rename
	tempPath := target + ".new"
	if err := copyFile(newExe, tempPath); err != nil {
		os.Remove(backupPath)
		return fmt.Errorf("copy new version: %w", err)
	}

	// Ensure executable permission
	if err := os.Chmod(tempPath, 0755); err != nil {
		os.Remove(tempPath)
		os.Remove(backupPath)
		return fmt.Errorf("set permissions: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, target); err != nil {
		os.Remove(tempPath)
		os.Remove(backupPath)
		return fmt.Errorf("atomic rename: %w", err)
	}

	// Remove backup on success
	os.Remove(backupPath)

	return nil
}

func launchApp(target string) error {
	cmd := exec.Command(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
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
