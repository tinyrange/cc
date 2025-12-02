///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const PACKAGE_NAME = "github.com/tinyrange/cc"

type crossBuild struct {
	GOOS   string
	GOARCH string
}

func (cb crossBuild) IsNative() bool {
	return cb.GOOS == runtime.GOOS && cb.GOARCH == runtime.GOARCH
}

func (cb crossBuild) OutputName(name string) string {
	suffix := ""
	if cb.GOOS == "windows" {
		suffix = ".exe"
	}

	if cb.IsNative() {
		return fmt.Sprintf("%s%s", name, suffix)
	} else {
		return fmt.Sprintf("%s_%s_%s%s", name, cb.GOOS, cb.GOARCH, suffix)
	}
}

var hostBuild = crossBuild{
	GOOS:   runtime.GOOS,
	GOARCH: runtime.GOARCH,
}

var crossBuilds = []crossBuild{
	{"linux", "amd64"},
	{"windows", "amd64"},
	{"linux", "arm64"},
	{"darwin", "arm64"},
	{"windows", "arm64"},
}

type buildOptions struct {
	Package          string
	OutputName       string
	CgoEnabled       bool
	Build            crossBuild
	RaceEnabled      bool
	EntitlementsPath string
}

type buildOutput struct {
	Path string
}

func goBuild(opts buildOptions) (buildOutput, error) {
	output := filepath.Join("build", opts.Build.OutputName(opts.OutputName))

	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return buildOutput{}, fmt.Errorf("failed to create build directory: %w", err)
	}

	pkg := PACKAGE_NAME + "/" + opts.Package

	env := os.Environ()
	env = append(env, "GOOS="+opts.Build.GOOS)
	env = append(env, "GOARCH="+opts.Build.GOARCH)
	if opts.CgoEnabled || opts.RaceEnabled {
		env = append(env, "CGO_ENABLED=1")
	} else {
		env = append(env, "CGO_ENABLED=0")
	}

	if opts.RaceEnabled {
		env = append(env, "GOFLAGS=-race")
	}

	cmd := exec.Command("go", "build", "-o", output, pkg)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return buildOutput{}, fmt.Errorf("go build failed: %w", err)
	}

	// if entitlements path is set and building for darwin/arm64, codesign the output
	if opts.EntitlementsPath != "" && opts.Build.GOOS == "darwin" && opts.Build.GOARCH == "arm64" {
		// build internal/cmd/codesign first
		codesignOut, err := goBuild(buildOptions{
			Package:    "internal/cmd/codesign",
			OutputName: "codesign",
			Build:      hostBuild,
		})
		if err != nil {
			return buildOutput{}, fmt.Errorf("failed to build codesign tool: %w", err)
		}

		cmd := exec.Command(codesignOut.Path,
			"-entitlements", opts.EntitlementsPath,
			"-bin", output,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return buildOutput{}, fmt.Errorf("failed to codesign output: %w", err)
		}
	}

	return buildOutput{Path: output}, nil
}

func runBuildOutput(output buildOutput, args []string) error {
	cmd := exec.Command(output.Path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run build output: %w", err)
	}

	return nil
}

func getOrCreateHostID() (string, error) {
	idFile := filepath.Join("local", "hostid")
	if data, err := os.ReadFile(idFile); err == nil {
		return string(data), nil
	}

	// Just 4 random bytes hex-encoded
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate host ID: %w", err)
	}

	hostID := fmt.Sprintf("%x", b)

	if err := os.MkdirAll("local", 0755); err != nil {
		return "", fmt.Errorf("failed to create local directory: %w", err)
	}

	if err := os.WriteFile(idFile, []byte(hostID), 0644); err != nil {
		return "", fmt.Errorf("failed to write host ID file: %w", err)
	}

	return hostID, nil
}

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	race := fs.Bool("race", false, "build with race detector enabled")
	quest := fs.Bool("quest", false, "build and execute the bringup quest")
	cross := fs.Bool("cross", false, "attempt to cross compile the quest for all supported platforms")
	test := fs.String("test", "", "run go tests in the specified package")
	bench := fs.Bool("bench", false, "run all benchmarks and output results to benchmarks/<hostid>/<date>.json")
	codesign := fs.Bool("codesign", false, "build the macos codesign tool")
	oci := fs.Bool("oci", false, "build and execute the OCI image tool")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if *codesign {
		out, err := goBuild(buildOptions{
			Package:    "internal/cmd/codesign",
			OutputName: "codesign",
			Build:      hostBuild,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build codesign: %v\n", err)
			os.Exit(1)
		}

		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}

		return
	}

	if *oci {
		out, err := goBuild(buildOptions{
			Package:    "cmd/oci",
			OutputName: "oci",
			Build:      hostBuild,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build oci tool: %v\n", err)
			os.Exit(1)
		}

		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}

		return
	}

	if *quest {
		out, err := goBuild(buildOptions{
			Package:          "cmd/quest",
			OutputName:       "quest",
			Build:            hostBuild,
			RaceEnabled:      *race,
			EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build quest: %v\n", err)
			os.Exit(1)
		}

		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}

		return
	}

	if *cross {
		for _, cb := range crossBuilds {
			fmt.Printf("Building for %s/%s...\n", cb.GOOS, cb.GOARCH)
			_, err := goBuild(buildOptions{
				Package:          "cmd/quest",
				OutputName:       "quest",
				Build:            cb,
				EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to build for %s/%s: %v\n", cb.GOOS, cb.GOARCH, err)
				os.Exit(1)
			}
		}

		return
	}

	if *test != "" {
		cmd := exec.Command("go", "test", *test)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}

		return
	}

	if *bench {
		// go test -bench=. -run=none -benchmem -json > bench.json
		cmd := exec.Command("go", "test", "-bench=.", "-run=none", "-benchmem", "-json", "./...")
		hostId, err := getOrCreateHostID()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get host ID: %v\n", err)
			os.Exit(1)
		}

		benchDir := filepath.Join("benchmarks", hostId)
		if err := os.MkdirAll(benchDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create benchmark directory: %v\n", err)
			os.Exit(1)
		}

		// benchmark_YYYYMMDD_HHMMSS.json (date in UTC)
		benchFile := filepath.Join(benchDir,
			fmt.Sprintf("benchmark_%s.njson", time.Now().UTC().Format("20060102_150405")),
		)
		f, err := os.Create(benchFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create benchmark file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		// if git is available, get the commit hash
		commitHash := "unknown"
		if cmd := exec.Command("git", "rev-parse", "HEAD"); cmd != nil {
			if out, err := cmd.Output(); err == nil {
				commitHash = string(out)
				commitHash = strings.TrimSpace(commitHash)
			}
		}

		// write a first line with the go version, GOOS, GOARCH
		if err := json.NewEncoder(f).Encode(struct {
			GoVersion string `json:"go_version"`
			GOOS      string `json:"go_os"`
			GOARCH    string `json:"go_arch"`
			Commit    string `json:"commit"`
		}{
			GoVersion: runtime.Version(),
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			Commit:    commitHash,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write benchmark metadata: %v\n", err)
			os.Exit(1)
		}

		cmd.Stdout = f
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to run benchmarks: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("benchmarks written to %s\n", benchFile)
		return
	}

	fmt.Fprintf(os.Stderr, "main build not implemented\n")
	os.Exit(1)
}
