//go:build windows

package update

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// createWindowsShortcut creates a .lnk shortcut in the Start Menu using OLE/COM.
func createWindowsShortcut(appPath string) error {
	// Validate path before using
	if err := validatePathForScript(appPath); err != nil {
		return fmt.Errorf("invalid app path: %w", err)
	}

	// Get the Start Menu Programs directory
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return fmt.Errorf("APPDATA environment variable not set")
	}

	startMenuDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	shortcutPath := filepath.Join(startMenuDir, "CrumbleCracker.lnk")
	workingDir := filepath.Dir(appPath)

	// Use the executable itself as the icon source (Windows embeds icons in .exe files)
	return createShortcut(
		shortcutPath,
		appPath,
		"",
		workingDir,
		"CrumbleCracker - Run Linux containers",
		appPath,
		0,
	)
}

// createShortcut creates a Windows .lnk shortcut file using OLE/COM.
func createShortcut(shortcutPath, targetPath, args, workingDir, description, iconPath string, iconIndex int) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED|ole.COINIT_SPEED_OVER_MEMORY); err != nil {
		// CoInitializeEx may return S_FALSE (0x00000001) if COM was already initialized,
		// which go-ole treats as an error. Check if it's actually an error.
		if oleErr, ok := err.(*ole.OleError); ok && oleErr.Code() == 0x00000001 {
			// S_FALSE - COM already initialized, that's fine
		} else {
			return fmt.Errorf("CoInitializeEx: %w", err)
		}
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return fmt.Errorf("create WScript.Shell: %w", err)
	}
	defer unknown.Release()

	wshell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("query IDispatch: %w", err)
	}
	defer wshell.Release()

	shortcut, err := oleutil.CallMethod(wshell, "CreateShortcut", shortcutPath)
	if err != nil {
		return fmt.Errorf("CreateShortcut: %w", err)
	}
	dispatch := shortcut.ToIDispatch()
	defer dispatch.Release()

	if _, err := oleutil.PutProperty(dispatch, "TargetPath", targetPath); err != nil {
		return fmt.Errorf("set TargetPath: %w", err)
	}
	if _, err := oleutil.PutProperty(dispatch, "Arguments", args); err != nil {
		return fmt.Errorf("set Arguments: %w", err)
	}
	if _, err := oleutil.PutProperty(dispatch, "WorkingDirectory", workingDir); err != nil {
		return fmt.Errorf("set WorkingDirectory: %w", err)
	}
	if _, err := oleutil.PutProperty(dispatch, "Description", description); err != nil {
		return fmt.Errorf("set Description: %w", err)
	}

	// Icon: path to .ico, .exe, or .dll, with index for multi-icon files
	if iconPath != "" {
		iconLocation := fmt.Sprintf("%s,%d", iconPath, iconIndex)
		if _, err := oleutil.PutProperty(dispatch, "IconLocation", iconLocation); err != nil {
			return fmt.Errorf("set IconLocation: %w", err)
		}
	}

	if _, err := oleutil.CallMethod(dispatch, "Save"); err != nil {
		return fmt.Errorf("save shortcut: %w", err)
	}

	return nil
}
