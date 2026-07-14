package sidecar

import (
	"os"
	"os/exec"
	"strings"
)

type LaunchOptions struct {
	DisableEnv    string
	ControlEnv    string
	ModeEnv       string
	TLSConfigPath string
}

func LaunchCommand(exe, cacheDir, controlSocket string, env []string, opts LaunchOptions) *exec.Cmd {
	args := launchArgs(opts.ModeEnv)
	args = append(args, "-worker", "-cache-dir", cacheDir)
	if tlsConfigPath := strings.TrimSpace(opts.TLSConfigPath); tlsConfigPath != "" {
		args = append(args, "-worker-tls-config", tlsConfigPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), opts.DisableEnv+"=1", opts.ControlEnv+"="+controlSocket)
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

func launchArgs(modeEnv string) []string {
	switch strings.TrimSpace(os.Getenv(modeEnv)) {
	case "vmsh-internal":
		return nil
	default:
		return nil
	}
}
