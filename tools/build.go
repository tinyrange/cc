///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
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

type remoteTarget struct {
	Address   string `json:"address"`
	GOOS      string `json:"os"`
	GOARCH    string `json:"arch"`
	TargetDir string `json:"targetDir"`
}

type buildOptions struct {
	Package          string
	OutputName       string
	CgoEnabled       bool
	Build            crossBuild
	RaceEnabled      bool
	EntitlementsPath string
	BuildTests       bool
	Tags             []string
	// Build a bundle on macOS, build as a windows executable on windows, and build as a normal executable on linux
	BuildApp bool
	// LogoPath is the path to a PNG image to use as the application icon (macOS only)
	LogoPath string
}

type buildOutput struct {
	Path string
}

func copyFile(dstPath, srcPath string, perm os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("mkdir dst dir: %w", err)
	}

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("open dst: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	return nil
}

// encodePlist marshals a struct into a minimal XML property list.
//
// Supported field kinds:
// - string => <string>
// - bool   => <true/> / <false/>
// - int/uint and sized variants => <integer>
//
// Fields are encoded in declaration order. Use struct tags: `plist:"CFBundleName"`.
func encodePlist(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("plist: expected struct, got %T", v)
	}
	rt := rv.Type()

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")

	enc := xml.NewEncoder(&buf)
	enc.Indent("", "\t")

	plistStart := xml.StartElement{
		Name: xml.Name{Local: "plist"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}},
	}
	if err := enc.EncodeToken(plistStart); err != nil {
		return nil, fmt.Errorf("plist: encode <plist>: %w", err)
	}
	dictStart := xml.StartElement{Name: xml.Name{Local: "dict"}}
	if err := enc.EncodeToken(dictStart); err != nil {
		return nil, fmt.Errorf("plist: encode <dict>: %w", err)
	}

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		key := f.Tag.Get("plist")
		if key == "" || key == "-" {
			continue
		}

		fv := rv.Field(i)
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		if err := enc.EncodeElement(key, xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return nil, fmt.Errorf("plist: encode key %q: %w", key, err)
		}

		switch fv.Kind() {
		case reflect.String:
			if err := enc.EncodeElement(fv.String(), xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
				return nil, fmt.Errorf("plist: encode string %q: %w", key, err)
			}
		case reflect.Bool:
			elem := "false"
			if fv.Bool() {
				elem = "true"
			}
			start := xml.StartElement{Name: xml.Name{Local: elem}}
			if err := enc.EncodeToken(start); err != nil {
				return nil, fmt.Errorf("plist: encode <%s/> for %q: %w", elem, key, err)
			}
			if err := enc.EncodeToken(start.End()); err != nil {
				return nil, fmt.Errorf("plist: encode </%s> for %q: %w", elem, key, err)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			s := strconv.FormatInt(fv.Int(), 10)
			if err := enc.EncodeElement(s, xml.StartElement{Name: xml.Name{Local: "integer"}}); err != nil {
				return nil, fmt.Errorf("plist: encode integer %q: %w", key, err)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			s := strconv.FormatUint(fv.Uint(), 10)
			if err := enc.EncodeElement(s, xml.StartElement{Name: xml.Name{Local: "integer"}}); err != nil {
				return nil, fmt.Errorf("plist: encode integer %q: %w", key, err)
			}
		default:
			return nil, fmt.Errorf("plist: unsupported kind %s for key %q (field %s)", fv.Kind(), key, f.Name)
		}
	}

	if err := enc.EncodeToken(dictStart.End()); err != nil {
		return nil, fmt.Errorf("plist: encode </dict>: %w", err)
	}
	if err := enc.EncodeToken(plistStart.End()); err != nil {
		return nil, fmt.Errorf("plist: encode </plist>: %w", err)
	}
	if err := enc.Flush(); err != nil {
		return nil, fmt.Errorf("plist: flush: %w", err)
	}

	return buf.Bytes(), nil
}

func writeMacOSAppBundle(bundlePath, executablePath, appName, logoPath string) error {
	contentsDir := filepath.Join(bundlePath, "Contents")
	macosDir := filepath.Join(contentsDir, "MacOS")
	resourcesDir := filepath.Join(contentsDir, "Resources")

	if err := os.RemoveAll(bundlePath); err != nil {
		return fmt.Errorf("remove existing bundle: %w", err)
	}
	if err := os.MkdirAll(macosDir, 0755); err != nil {
		return fmt.Errorf("create bundle dirs: %w", err)
	}
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		return fmt.Errorf("create resources dir: %w", err)
	}

	exeName := filepath.Base(executablePath)
	exeDst := filepath.Join(macosDir, exeName)
	if err := copyFile(exeDst, executablePath, 0755); err != nil {
		return fmt.Errorf("copy app executable: %w", err)
	}

	// Copy the logo to Resources if provided
	var iconFileName string
	if logoPath != "" {
		iconFileName = filepath.Base(logoPath)
		iconDst := filepath.Join(resourcesDir, iconFileName)
		if err := copyFile(iconDst, logoPath, 0644); err != nil {
			return fmt.Errorf("copy app icon: %w", err)
		}
	}

	// Minimal Info.plist required for a runnable app bundle.
	// Keep it simple: this is primarily for local development workflows.
	bundleID := "com.tinyrange." + strings.ToLower(appName)

	type infoPlist struct {
		CFBundleDevelopmentRegion     string `plist:"CFBundleDevelopmentRegion"`
		CFBundleExecutable            string `plist:"CFBundleExecutable"`
		CFBundleIdentifier            string `plist:"CFBundleIdentifier"`
		CFBundleInfoDictionaryVersion string `plist:"CFBundleInfoDictionaryVersion"`
		CFBundleName                  string `plist:"CFBundleName"`
		CFBundlePackageType           string `plist:"CFBundlePackageType"`
		CFBundleShortVersionString    string `plist:"CFBundleShortVersionString"`
		CFBundleVersion               string `plist:"CFBundleVersion"`
		NSHighResolutionCapable       bool   `plist:"NSHighResolutionCapable"`
		CFBundleIconFile              string `plist:"CFBundleIconFile"`
	}

	plistData, err := encodePlist(infoPlist{
		CFBundleDevelopmentRegion:     "en",
		CFBundleExecutable:            exeName,
		CFBundleIdentifier:            bundleID,
		CFBundleInfoDictionaryVersion: "6.0",
		CFBundleName:                  appName,
		CFBundlePackageType:           "APPL",
		CFBundleShortVersionString:    "0.0.0",
		CFBundleVersion:               "0",
		NSHighResolutionCapable:       true,
		CFBundleIconFile:              iconFileName,
	})
	if err != nil {
		return fmt.Errorf("encode Info.plist: %w", err)
	}

	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), plistData, 0644); err != nil {
		return fmt.Errorf("write Info.plist: %w", err)
	}

	return nil
}

func goBuild(opts buildOptions) (buildOutput, error) {
	output := filepath.Join("build", opts.Build.OutputName(opts.OutputName))
	macosBundlePath := ""
	if opts.BuildApp && opts.Build.GOOS == "darwin" {
		macosBundlePath = output + ".app"
	}

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

	var args []string
	if opts.BuildTests {
		args = []string{"go", "test", "-c", "-o", output}
	} else {
		args = []string{"go", "build", "-o", output}
	}

	if len(opts.Tags) > 0 {
		args = append(args, "-tags", strings.Join(opts.Tags, " "))
	}

	// if the target is windows and BuildApp is true use the windows subsystem rather than the default console subsystem
	if opts.Build.GOOS == "windows" && opts.BuildApp {
		args = append(args, "-ldflags=-H windowsgui")
	}

	args = append(args, pkg)

	cmd := exec.Command(args[0], args[1:]...)
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

	if opts.BuildApp && opts.Build.GOOS == "darwin" {
		// Turn the output binary into a macOS .app bundle.
		if err := writeMacOSAppBundle(macosBundlePath, output, opts.OutputName, opts.LogoPath); err != nil {
			return buildOutput{}, fmt.Errorf("failed to create macOS app bundle: %w", err)
		}
		return buildOutput{Path: macosBundlePath}, nil
	}

	return buildOutput{Path: output}, nil
}

func runBuildOutput(output buildOutput, args []string) error {
	// If this is a macOS app bundle, run via `open`.
	if runtime.GOOS == "darwin" && strings.HasSuffix(output.Path, ".app") {
		openArgs := []string{"-n", output.Path}
		if len(args) > 0 {
			openArgs = append(openArgs, "--args")
			openArgs = append(openArgs, args...)
		}
		cmd := exec.Command("open", openArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run app bundle: %w", err)
		}
		return nil
	}

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

func loadRemoteTarget(alias string) (remoteTarget, error) {
	remotesPath := filepath.Join("local", "remotes.json")
	data, err := os.ReadFile(remotesPath)
	if err != nil {
		return remoteTarget{}, fmt.Errorf("failed to read remotes from %s: %w", remotesPath, err)
	}

	var remotes map[string]remoteTarget
	if err := json.Unmarshal(data, &remotes); err != nil {
		return remoteTarget{}, fmt.Errorf("failed to parse remotes file %s: %w", remotesPath, err)
	}

	target, ok := remotes[alias]
	if !ok {
		return remoteTarget{}, fmt.Errorf("remote alias %q not found in %s", alias, remotesPath)
	}

	if target.Address == "" || target.GOOS == "" || target.GOARCH == "" || target.TargetDir == "" {
		return remoteTarget{}, fmt.Errorf("remote alias %q missing required fields (address/os/arch/targetDir)", alias)
	}

	return target, nil
}

func runRemoteCommand(remote remoteTarget, output buildOutput, args []string) error {
	cmdName := filepath.Join(remote.TargetDir, filepath.Base(output.Path))

	cmdArgs := append([]string{"run", "-timeout", "30s", remote.Address, cmdName}, args...)
	cmd := exec.Command("remotectl", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remotectl execution failed: %w", err)
	}

	return nil
}

func pushBuildOutput(remote remoteTarget, output buildOutput) error {
	targetFile := filepath.Join(remote.TargetDir, filepath.Base(output.Path))

	// push the file using remotectl push-file
	cmd := exec.Command(
		"remotectl", "push-file",
		remote.Address,
		output.Path,
		targetFile,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remotectl push failed: %w", err)
	}

	// make the file executable using remotectl run chmod +x
	cmd = exec.Command(
		"remotectl", "run", remote.Address,
		"chmod", "+x", targetFile,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remotectl chmod failed: %w", err)
	}

	return nil
}

// isContextOlderThanOutput checks if all files in the context directory
// are older than the output file. Returns true if the output is newer
// than all context files, false otherwise.
func isContextOlderThanOutput(contextDir, outputPath string) (bool, error) {
	outputInfo, err := os.Stat(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat output file: %w", err)
	}

	outputModTime := outputInfo.ModTime()

	err = filepath.Walk(contextDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories themselves, only check files
		if info.IsDir() {
			return nil
		}

		if info.ModTime().After(outputModTime) {
			return fmt.Errorf("file %s is newer than output", path)
		}

		return nil
	})

	if err != nil {
		// If we found a newer file, return false
		return false, nil
	}

	// All files are older than the output
	return true, nil
}

// clearCCCache clears the cc cache for a given tar file path.
// This ensures that when we rebuild a Docker image, cc will re-extract it.
func clearCCCache(tarPath string) error {
	// Resolve absolute path (same as cc does)
	absPath, err := filepath.Abs(tarPath)
	if err != nil {
		return fmt.Errorf("resolve tar path: %w", err)
	}

	// Handle relative paths starting with "./" (same as cc does)
	if strings.HasPrefix(tarPath, "./") {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		absPath = filepath.Join(wd, tarPath[2:])
		absPath, err = filepath.Abs(absPath)
		if err != nil {
			return fmt.Errorf("resolve tar path: %w", err)
		}
	}

	// Get cache directory (same default as cc uses)
	cacheDir := os.Getenv("CC_CACHE_DIR")
	if cacheDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("get user config dir: %w", err)
		}
		cacheDir = filepath.Join(configDir, "cc", "oci")
	}

	// Sanitize the tar path for use as a filename (same as cc does)
	sanitized := sanitizeForFilename(absPath)
	cachePath := filepath.Join(cacheDir, "images", sanitized)

	// Remove the cache directory if it exists
	if _, err := os.Stat(cachePath); err == nil {
		if err := os.RemoveAll(cachePath); err != nil {
			return fmt.Errorf("remove cache directory: %w", err)
		}
	}

	return nil
}

// sanitizeForFilename sanitizes a string for use as a filename.
// This matches the implementation in internal/oci/client.go
func sanitizeForFilename(value string) string {
	value = strings.TrimPrefix(value, "/")
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '/', '\\', ':', '?', '*', '"', '<', '>', '|', ' ':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "root"
	}
	return b.String()
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
	kernel := fs.Bool("kernel", false, "build and execute the kernel tool")
	bringup := fs.Bool("bringup", false, "build and execute the bringup tool inside a linux VM")
	bringupGPU := fs.Bool("bringup-gpu", false, "build and execute the GPU bringup tool with graphics support")
	remote := fs.String("remote", "", "run quest/bringup on remote host alias from local/remotes.json")
	run := fs.Bool("run", false, "run the built cc tool after building")
	runtest := fs.String("runtest", "", "build a Dockerfile in tests/<name>/Dockerfile and run it using cc (Linux only)")
	dbgTool := fs.Bool("dbg-tool", false, "build and run the debug tool")
	tsTool := fs.Bool("ts-tool", false, "build and run the timeslice tool")
	app := fs.Bool("app", false, "build and run ccapp")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	var remoteTargetConfig *remoteTarget
	if *remote != "" {
		if !*quest && !*bringup && !*bringupGPU {
			fmt.Fprintf(os.Stderr, "-remote can only be used with -quest, -bringup, or -bringup-gpu\n")
			os.Exit(1)
		}
		if *cross {
			fmt.Fprintf(os.Stderr, "-remote cannot be combined with -cross\n")
			os.Exit(1)
		}
		target, err := loadRemoteTarget(*remote)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load remote alias: %v\n", err)
			os.Exit(1)
		}
		remoteTargetConfig = &target
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

	if *dbgTool {
		out, err := goBuild(buildOptions{
			Package:    "cmd/debug",
			OutputName: "debug",
			Build:      hostBuild,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build debug tool: %v\n", err)
			os.Exit(1)
		}

		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}

		return
	}

	if *tsTool {
		out, err := goBuild(buildOptions{
			Package:    "cmd/timeslice",
			OutputName: "timeslice",
			Build:      hostBuild,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build timeslice tool: %v\n", err)
			os.Exit(1)
		}
		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}
		return
	}

	if *oci {
		out, err := goBuild(buildOptions{
			Package:    "internal/cmd/oci",
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

	if *kernel {
		out, err := goBuild(buildOptions{
			Package:    "internal/cmd/kernel",
			OutputName: "kernel",
			Build:      hostBuild,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build kernel tool: %v\n", err)
			os.Exit(1)
		}

		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}

		return
	}

	if *quest {
		buildTarget := hostBuild
		if remoteTargetConfig != nil {
			buildTarget = crossBuild{
				GOOS:   remoteTargetConfig.GOOS,
				GOARCH: remoteTargetConfig.GOARCH,
			}
		}

		out, err := goBuild(buildOptions{
			Package:          "internal/cmd/quest",
			OutputName:       "quest",
			Build:            buildTarget,
			RaceEnabled:      *race,
			EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build quest: %v\n", err)
			os.Exit(1)
		}

		if remoteTargetConfig != nil {
			if err := pushBuildOutput(*remoteTargetConfig, out); err != nil {
				fmt.Fprintf(os.Stderr, "failed to push quest to remote: %v\n", err)
				os.Exit(1)
			}

			if err := runRemoteCommand(*remoteTargetConfig, out, fs.Args()); err != nil {
				fmt.Fprintf(os.Stderr, "failed to run quest remotely: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := runBuildOutput(out, fs.Args()); err != nil {
				os.Exit(1)
			}
		}

		return
	}

	if *bringup {
		bringupBuild := crossBuild{GOOS: "linux", GOARCH: hostBuild.GOARCH}
		questBuild := hostBuild

		if remoteTargetConfig != nil {
			bringupBuild = crossBuild{
				GOOS:   "linux",
				GOARCH: remoteTargetConfig.GOARCH,
			}
			questBuild = crossBuild{
				GOOS:   remoteTargetConfig.GOOS,
				GOARCH: remoteTargetConfig.GOARCH,
			}
		}

		bringupOut, err := goBuild(buildOptions{
			Package:    "internal/cmd/bringup",
			OutputName: "bringup",
			CgoEnabled: false,
			Build:      bringupBuild,
			BuildTests: true,
			Tags:       []string{"guest"},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build bringup tool: %v\n", err)
			os.Exit(1)
		}

		out, err := goBuild(buildOptions{
			Package:          "internal/cmd/quest",
			OutputName:       "quest",
			Build:            questBuild,
			RaceEnabled:      *race,
			EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build bringup quest: %v\n", err)
			os.Exit(1)
		}

		bringupExecPath := bringupOut.Path

		if remoteTargetConfig != nil {
			bringupExecPath = filepath.Join(remoteTargetConfig.TargetDir, filepath.Base(bringupOut.Path))

			if err := pushBuildOutput(*remoteTargetConfig, bringupOut); err != nil {
				fmt.Fprintf(os.Stderr, "failed to push bringup to remote: %v\n", err)
				os.Exit(1)
			}

			if err := pushBuildOutput(*remoteTargetConfig, out); err != nil {
				fmt.Fprintf(os.Stderr, "failed to push bringup quest to remote: %v\n", err)
				os.Exit(1)
			}
		}

		args := append([]string{"-exec", bringupExecPath}, fs.Args()...)
		if remoteTargetConfig != nil {
			if err := runRemoteCommand(*remoteTargetConfig, out, args); err != nil {
				fmt.Fprintf(os.Stderr, "failed to run bringup quest remotely: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := runBuildOutput(out, args); err != nil {
				os.Exit(1)
			}
		}

		return
	}

	if *bringupGPU {
		// GPU bringup: builds a special guest test that uses framebuffer and input
		bringupBuild := crossBuild{GOOS: "linux", GOARCH: hostBuild.GOARCH}
		questBuild := hostBuild

		if remoteTargetConfig != nil {
			bringupBuild = crossBuild{
				GOOS:   "linux",
				GOARCH: remoteTargetConfig.GOARCH,
			}
			questBuild = crossBuild{
				GOOS:   remoteTargetConfig.GOOS,
				GOARCH: remoteTargetConfig.GOARCH,
			}
		}

		// Build the GPU bringup test binary (runs inside guest)
		bringupOut, err := goBuild(buildOptions{
			Package:    "internal/cmd/bringup-gpu",
			OutputName: "bringup-gpu",
			CgoEnabled: false,
			Build:      bringupBuild,
			BuildTests: true,
			Tags:       []string{"guest"},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build bringup-gpu tool: %v\n", err)
			os.Exit(1)
		}

		// Build quest (host-side VM runner)
		out, err := goBuild(buildOptions{
			Package:          "internal/cmd/quest",
			OutputName:       "quest",
			Build:            questBuild,
			RaceEnabled:      *race,
			EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build quest for bringup-gpu: %v\n", err)
			os.Exit(1)
		}

		bringupExecPath := bringupOut.Path

		if remoteTargetConfig != nil {
			bringupExecPath = filepath.Join(remoteTargetConfig.TargetDir, filepath.Base(bringupOut.Path))

			if err := pushBuildOutput(*remoteTargetConfig, bringupOut); err != nil {
				fmt.Fprintf(os.Stderr, "failed to push bringup-gpu to remote: %v\n", err)
				os.Exit(1)
			}

			if err := pushBuildOutput(*remoteTargetConfig, out); err != nil {
				fmt.Fprintf(os.Stderr, "failed to push quest to remote: %v\n", err)
				os.Exit(1)
			}
		}

		// Run quest with -exec for the bringup-gpu binary and -gpu flag to enable GPU
		args := append([]string{"-exec", bringupExecPath, "-gpu"}, fs.Args()...)
		if remoteTargetConfig != nil {
			if err := runRemoteCommand(*remoteTargetConfig, out, args); err != nil {
				fmt.Fprintf(os.Stderr, "failed to run bringup-gpu quest remotely: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := runBuildOutput(out, args); err != nil {
				os.Exit(1)
			}
		}

		return
	}

	if *cross {
		for _, cb := range crossBuilds {
			fmt.Printf("Building for %s/%s...\n", cb.GOOS, cb.GOARCH)
			_, err := goBuild(buildOptions{
				Package:          "internal/cmd/quest",
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

	if *runtest != "" {
		testName := *runtest
		dockerfilePath := filepath.Join("tests", testName, "Dockerfile")
		if _, err := os.Stat(dockerfilePath); err != nil {
			fmt.Fprintf(os.Stderr, "Dockerfile not found: %s\n", dockerfilePath)
			os.Exit(1)
		}

		// Build cc first
		ccOut, err := goBuild(buildOptions{
			Package:          "cmd/cc",
			OutputName:       "cc",
			Build:            hostBuild,
			EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build cc: %v\n", err)
			os.Exit(1)
		}

		// Check if we can skip building the Docker image
		buildDir := filepath.Join("tests", testName)
		tarPath := filepath.Join("build", fmt.Sprintf("test-%s.tar", testName))
		if err := os.MkdirAll(filepath.Dir(tarPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create build directory: %v\n", err)
			os.Exit(1)
		}

		hasDocker := false
		if _, err := exec.LookPath("docker"); err == nil {
			hasDocker = true
		}

		shouldRebuild := true
		if isOlder, err := isContextOlderThanOutput(buildDir, tarPath); err != nil {
			fmt.Fprintf(os.Stderr, "failed to check cache: %v\n", err)
			os.Exit(1)
		} else if isOlder || !hasDocker {
			fmt.Printf("Context is older than output tar file, using cached build...\n")
			shouldRebuild = false
		}

		if shouldRebuild {
			if !hasDocker {
				fmt.Fprintf(os.Stderr, "docker is not installed, cannot build Docker image\n")
				os.Exit(1)
			}
			// Clear cc cache for this tar file so it will be re-extracted
			if err := clearCCCache(tarPath); err != nil {
				// Log but don't fail - cache clearing is best effort
				fmt.Fprintf(os.Stderr, "warning: failed to clear cc cache: %v\n", err)
			}

			// Build Docker image
			imageTag := fmt.Sprintf("cc-test-%s:latest", testName)

			fmt.Printf("Building Docker image from %s...\n", dockerfilePath)
			buildCmd := exec.Command("docker", "build",
				"-t", imageTag,
				"-f", dockerfilePath,
				buildDir,
			)
			buildCmd.Stdout = os.Stdout
			buildCmd.Stderr = os.Stderr
			if err := buildCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to build Docker image: %v\n", err)
				os.Exit(1)
			}

			// Save Docker image as tar archive
			fmt.Printf("Saving Docker image to %s...\n", tarPath)
			saveCmd := exec.Command("docker", "save", "-o", tarPath, imageTag)
			saveCmd.Stdout = os.Stdout
			saveCmd.Stderr = os.Stderr
			if err := saveCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to save Docker image: %v\n", err)
				os.Exit(1)
			}
		}

		// Run cc with the tar file
		// Use relative path so cc can load it
		relativeTarPath := filepath.Join(".", "build", fmt.Sprintf("test-%s.tar", testName))
		if !filepath.IsAbs(relativeTarPath) {
			relativeTarPath = strings.Join([]string{".", relativeTarPath}, string(filepath.Separator))
		}
		ccArgs := append(fs.Args(), relativeTarPath)
		fmt.Printf("Running cc with image %s and args %s...\n", relativeTarPath, strings.Join(ccArgs, " "))
		if err := runBuildOutput(ccOut, ccArgs); err != nil {
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

	// build cmd/cc by default
	out, err := goBuild(buildOptions{
		Package:          "cmd/cc",
		OutputName:       "cc",
		Build:            hostBuild,
		EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build cc: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("built %s\n", out.Path)

	// also build cmd/ccapp
	ccappOut, err := goBuild(buildOptions{
		Package:          "cmd/ccapp",
		OutputName:       "ccapp",
		Build:            hostBuild,
		EntitlementsPath: filepath.Join("tools", "entitlements.xml"),
		BuildApp:         true,
		LogoPath:         filepath.Join("internal", "assets", "logo-color-black.png"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build ccapp: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("built %s\n", ccappOut.Path)

	if *run {
		fmt.Printf("running %s %s\n", out.Path, strings.Join(fs.Args(), " "))
		if err := runBuildOutput(out, fs.Args()); err != nil {
			os.Exit(1)
		}
	} else if *app {
		switch runtime.GOOS {
		case "darwin":
			if err := runBuildOutput(ccappOut, fs.Args()); err != nil {
				os.Exit(1)
			}
		case "windows":
			// On Windows, ccapp is built as a GUI app when BuildApp is true.
			if err := runBuildOutput(ccappOut, fs.Args()); err != nil {
				os.Exit(1)
			}
		case "linux":
			if err := runBuildOutput(ccappOut, fs.Args()); err != nil {
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "unsupported OS: %s\n", runtime.GOOS)
			os.Exit(1)
		}
	}
}
