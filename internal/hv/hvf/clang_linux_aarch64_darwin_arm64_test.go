//go:build darwin && arm64

package hvf

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var errMissingAArch64LinuxLinker = errors.New("clang aarch64-linux-gnu linker support is unavailable")

type linuxARM64ClangToolchain struct {
	clang string
	env   []string
}

func clangAArch64LinuxToolchain() (linuxARM64ClangToolchain, error) {
	lldPaths := existingPaths(
		lookPathOrEmpty("ld.lld"),
		"/opt/homebrew/opt/lld/bin/ld.lld",
		"/opt/homebrew/opt/llvm/bin/ld.lld",
		"/usr/local/opt/lld/bin/ld.lld",
		"/usr/local/opt/llvm/bin/ld.lld",
	)
	if len(lldPaths) == 0 {
		for _, pattern := range []string{
			"/opt/homebrew/Cellar/lld@*/*/bin/ld.lld",
			"/opt/homebrew/Cellar/llvm*/*/bin/ld.lld",
			"/usr/local/Cellar/lld@*/*/bin/ld.lld",
			"/usr/local/Cellar/llvm*/*/bin/ld.lld",
		} {
			matches, _ := filepath.Glob(pattern)
			lldPaths = append(lldPaths, existingPaths(matches...)...)
		}
	}
	if len(lldPaths) == 0 {
		return linuxARM64ClangToolchain{}, fmt.Errorf("%w: ld.lld not found", errMissingAArch64LinuxLinker)
	}

	clangPaths := existingPaths(
		"/opt/homebrew/opt/llvm/bin/clang",
		"/usr/local/opt/llvm/bin/clang",
		lookPathOrEmpty("clang"),
	)
	if len(clangPaths) == 0 {
		return linuxARM64ClangToolchain{}, fmt.Errorf("%w: clang not found in PATH or common Homebrew locations", errMissingAArch64LinuxLinker)
	}

	for _, clang := range clangPaths {
		for _, lld := range lldPaths {
			env := prependPathEnv(os.Environ(), filepath.Dir(lld))
			cmd := exec.Command(
				clang,
				"-###",
				"-fuse-ld=lld",
				"-target", "aarch64-linux-gnu",
				"-nostdlib",
				"-static",
				"-Wl,-e,_start",
				"-Wl,--build-id=none",
				"-o", os.DevNull,
				"-x", "c",
				os.DevNull,
			)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err == nil {
				return linuxARM64ClangToolchain{clang: clang, env: env}, nil
			} else if strings.Contains(string(out), "invalid linker name") {
				continue
			}
		}
	}

	return linuxARM64ClangToolchain{}, fmt.Errorf("%w: no compatible clang/ld.lld pair found", errMissingAArch64LinuxLinker)
}

func prependPathEnv(env []string, dir string) []string {
	out := append([]string(nil), env...)
	for i, entry := range out {
		if strings.HasPrefix(entry, "PATH=") {
			out[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
			return out
		}
	}
	return append(out, "PATH="+dir)
}

func existingPaths(paths ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func lookPathOrEmpty(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}
