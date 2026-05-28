package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "build-vsh-single:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	targetGOOS := firstNonEmpty(os.Getenv("CCX3_TARGET_GOOS"), goEnv(root, "GOOS"), runtime.GOOS)
	targetGOARCH := firstNonEmpty(os.Getenv("CCX3_TARGET_GOARCH"), goEnv(root, "GOARCH"), runtime.GOARCH)
	suffix := ""
	if targetGOOS == "windows" {
		suffix = ".exe"
	}

	buildDir := filepath.Join(root, "build", "vsh")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}

	initARM64 := filepath.Join(buildDir, "init-linux-arm64")
	initAMD64 := filepath.Join(buildDir, "init-linux-amd64")
	if err := goBuild(root, map[string]string{"GOOS": "linux", "GOARCH": "arm64"}, nil, initARM64, "./internal/cmd/init"); err != nil {
		return err
	}
	if err := copyFile(initARM64, filepath.Join(root, "internal", "guestinit", "guest-init-linux-arm64"), 0o644); err != nil {
		return err
	}
	if err := goBuild(root, map[string]string{"GOOS": "linux", "GOARCH": "amd64"}, nil, initAMD64, "./internal/cmd/init"); err != nil {
		return err
	}
	if err := copyFile(initAMD64, filepath.Join(root, "internal", "guestinit", "guest-init-linux-amd64"), 0o644); err != nil {
		return err
	}

	output := filepath.Join(buildDir, "vsh-"+targetGOOS+"-"+targetGOARCH+suffix)
	if err := goBuild(root, map[string]string{"GOOS": targetGOOS, "GOARCH": targetGOARCH}, []string{"embed_guestinit"}, output, "./cmd/vsh"); err != nil {
		return err
	}
	if targetGOOS == "darwin" && runtime.GOOS == "darwin" {
		if err := command(root, nil, "codesign", "-f", "-s", "-", "--entitlements", filepath.Join(root, "tools", "entitlements.xml"), output); err != nil {
			return err
		}
	}
	fmt.Println(output)
	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && bytes.Contains(data, []byte("module j5.nz/cc")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find cc repo root from %s", dir)
		}
		dir = parent
	}
}

func goEnv(root, key string) string {
	var out bytes.Buffer
	cmd := exec.Command("go", "env", key)
	cmd.Dir = root
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

func goBuild(root string, env map[string]string, tags []string, output, pkg string) error {
	args := []string{"build"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "-o", output, pkg)
	return command(root, env, "go", args...)
}

func command(root string, env map[string]string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
