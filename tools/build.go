///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const PACKAGE_NAME = "github.com/tinyrange/cc"

// ============================================================================
// Data Structures
// ============================================================================

// Command represents a single command in a target
type Command struct {
	Line     string   // Original line for error messages
	LineNum  int      // Line number for error messages
	Platform string   // "" (any), "darwin", "linux", "windows"
	Type     string   // "gobuild", "run", "copy", "mkdir", "rm", "sh"
	Args     []string // Parsed arguments
}

// Target represents a build target
type Target struct {
	Name         string
	Platforms    []string  // Empty = all platforms (from name[platforms]:)
	Requires     []string  // Required OS (errors if not met)
	Dependencies []string  // Target names this depends on
	Commands     []Command // Commands to execute
	LineNum      int       // Line number where target was defined
}

// Buildfile represents a parsed build configuration
type Buildfile struct {
	Variables map[string]string
	Targets   map[string]*Target
	Path      string // Path to the buildfile
}

// ============================================================================
// Parser
// ============================================================================

type parseState int

const (
	stateTopLevel parseState = iota
	stateInTarget
)

// parseBuildfile parses a Buildfile from a reader
func parseBuildfile(path string, content []byte) (*Buildfile, error) {
	bf := &Buildfile{
		Variables: make(map[string]string),
		Targets:   make(map[string]*Target),
		Path:      path,
	}

	// Add built-in variables
	bf.Variables["GOOS"] = runtime.GOOS
	bf.Variables["GOARCH"] = runtime.GOARCH
	bf.Variables["EXE"] = ""
	if runtime.GOOS == "windows" {
		bf.Variables["EXE"] = ".exe"
	}
	bf.Variables["SHLIB_EXT"] = ".so"
	if runtime.GOOS == "darwin" {
		bf.Variables["SHLIB_EXT"] = ".dylib"
	} else if runtime.GOOS == "windows" {
		bf.Variables["SHLIB_EXT"] = ".dll"
	}

	// Add PWD for cross-platform compatibility (not always set on Windows)
	if pwd, err := os.Getwd(); err == nil {
		bf.Variables["PWD"] = pwd
	}

	// Get version from git
	bf.Variables["VERSION"] = getVersionFromGit()

	lines := strings.Split(string(content), "\n")
	state := stateTopLevel
	var currentTarget *Target
	var continuedLine string
	continuedLineNum := 0

	for i, line := range lines {
		lineNum := i + 1

		// Handle line continuation
		if strings.HasSuffix(line, "\\") {
			if continuedLine == "" {
				continuedLineNum = lineNum
			}
			continuedLine += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		if continuedLine != "" {
			line = continuedLine + line
			lineNum = continuedLineNum
			continuedLine = ""
			continuedLineNum = 0
		}

		// Strip comments (but not inside quoted strings - simple approach)
		if idx := strings.Index(line, "#"); idx >= 0 {
			// Simple check: count quotes before the #
			prefix := line[:idx]
			if strings.Count(prefix, `"`)%2 == 0 && strings.Count(prefix, `'`)%2 == 0 {
				line = prefix
			}
		}

		// Trim trailing whitespace
		line = strings.TrimRight(line, " \t\r")

		// Empty line
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check if line starts with whitespace (command in target)
		startsWithWhitespace := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
		trimmedLine := strings.TrimSpace(line)

		if startsWithWhitespace && state == stateInTarget && currentTarget != nil {
			// Parse command
			cmd, err := parseCommand(trimmedLine, lineNum)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNum, err)
			}

			// Check for requires directive (must be first command)
			if cmd.Type == "requires" {
				if len(currentTarget.Commands) > 0 {
					return nil, fmt.Errorf("%s:%d: 'requires' must be the first directive in a target", path, lineNum)
				}
				currentTarget.Requires = cmd.Args
				continue
			}

			currentTarget.Commands = append(currentTarget.Commands, cmd)
			continue
		}

		// Non-indented line - either variable or target header
		state = stateTopLevel
		currentTarget = nil

		// Variable definition: NAME = value
		if idx := strings.Index(trimmedLine, "="); idx > 0 {
			// Check it's not inside a target header (has :)
			if !strings.Contains(trimmedLine[:idx], ":") {
				name := strings.TrimSpace(trimmedLine[:idx])
				value := strings.TrimSpace(trimmedLine[idx+1:])
				if isValidIdentifier(name) {
					bf.Variables[name] = value
					continue
				}
			}
		}

		// Target header: name: deps or name[platforms]: deps
		if idx := strings.Index(trimmedLine, ":"); idx > 0 {
			header := trimmedLine[:idx]
			deps := strings.TrimSpace(trimmedLine[idx+1:])

			// Parse platform specifier: name[platforms]
			var name string
			var platforms []string
			if bracketIdx := strings.Index(header, "["); bracketIdx > 0 {
				if !strings.HasSuffix(header, "]") {
					return nil, fmt.Errorf("%s:%d: unclosed platform specifier", path, lineNum)
				}
				name = strings.TrimSpace(header[:bracketIdx])
				platformStr := header[bracketIdx+1 : len(header)-1]
				for _, p := range strings.Fields(platformStr) {
					platforms = append(platforms, p)
				}
			} else {
				name = strings.TrimSpace(header)
			}

			if !isValidIdentifier(name) {
				return nil, fmt.Errorf("%s:%d: invalid target name %q", path, lineNum, name)
			}

			// Parse dependencies
			var dependencies []string
			if deps != "" {
				for _, dep := range strings.Fields(deps) {
					dependencies = append(dependencies, dep)
				}
			}

			currentTarget = &Target{
				Name:         name,
				Platforms:    platforms,
				Dependencies: dependencies,
				LineNum:      lineNum,
			}
			bf.Targets[name] = currentTarget
			state = stateInTarget
			continue
		}

		return nil, fmt.Errorf("%s:%d: unexpected line: %s", path, lineNum, trimmedLine)
	}

	return bf, nil
}

// parseCommand parses a command line into a Command struct
func parseCommand(line string, lineNum int) (Command, error) {
	cmd := Command{
		Line:    line,
		LineNum: lineNum,
	}

	// Check for platform conditional: @darwin, @linux, @windows
	if strings.HasPrefix(line, "@") {
		parts := strings.SplitN(line, " ", 2)
		cmd.Platform = strings.TrimPrefix(parts[0], "@")
		if len(parts) < 2 {
			return cmd, fmt.Errorf("platform conditional without command")
		}
		line = strings.TrimSpace(parts[1])
	}

	// Parse command type and arguments
	args := tokenize(line)
	if len(args) == 0 {
		return cmd, fmt.Errorf("empty command")
	}

	cmd.Type = args[0]
	cmd.Args = args[1:]

	// Validate command type
	validTypes := map[string]bool{
		"gobuild":  true,
		"run":      true,
		"copy":     true,
		"mkdir":    true,
		"rm":       true,
		"sh":       true,
		"env":      true,
		"requires": true,
	}

	if !validTypes[cmd.Type] {
		return cmd, fmt.Errorf("unknown command type %q (use 'sh' prefix for shell commands)", cmd.Type)
	}

	return cmd, nil
}

// tokenize splits a command line into tokens, respecting quoted strings
func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range line {
		if inQuote {
			if r == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
		} else {
			switch r {
			case '"', '\'':
				inQuote = true
				quoteChar = r
			case ' ', '\t':
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
			default:
				current.WriteRune(r)
			}
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// isValidIdentifier checks if a string is a valid identifier
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_') {
				return false
			}
		} else {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				return false
			}
		}
	}
	return true
}

// expandVariables expands $(VAR) and ${VAR} in a string
func (bf *Buildfile) expandVariables(s string) string {
	// Pattern for $(VAR) or ${VAR}
	re := regexp.MustCompile(`\$\(([^)]+)\)|\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		var name string
		if strings.HasPrefix(match, "$(") {
			name = match[2 : len(match)-1]
		} else {
			name = match[2 : len(match)-1]
		}
		if val, ok := bf.Variables[name]; ok {
			return val
		}
		// Try environment variable
		if val := os.Getenv(name); val != "" {
			return val
		}
		return match // Keep original if not found
	})
}

// ============================================================================
// Dependency Resolution & Execution
// ============================================================================

// resolveDependencies returns targets in execution order (topological sort)
func (bf *Buildfile) resolveDependencies(targetName string) ([]*Target, error) {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var result []*Target

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("circular dependency detected involving %q", name)
		}
		if visited[name] {
			return nil
		}

		target, ok := bf.Targets[name]
		if !ok {
			return fmt.Errorf("target %q not found", name)
		}

		inStack[name] = true

		for _, dep := range target.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}

		inStack[name] = false
		visited[name] = true
		result = append(result, target)
		return nil
	}

	if err := visit(targetName); err != nil {
		return nil, err
	}

	return result, nil
}

// shouldRunOnPlatform checks if a target should run on the current platform
func (target *Target) shouldRunOnPlatform() bool {
	if len(target.Platforms) == 0 {
		return true
	}

	currentPlatform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	for _, p := range target.Platforms {
		if p == currentPlatform || p == runtime.GOOS {
			return true
		}
	}
	return false
}

// checkRequires checks if the current OS is in the requires list
func (target *Target) checkRequires() error {
	if len(target.Requires) == 0 {
		return nil
	}

	for _, req := range target.Requires {
		if req == runtime.GOOS {
			return nil
		}
	}

	return fmt.Errorf("target %q requires one of %v, but running on %s", target.Name, target.Requires, runtime.GOOS)
}

// Executor handles running targets
type Executor struct {
	Buildfile    *Buildfile
	DryRun       bool
	ExtraArgs    []string // Arguments passed after --
	builtOutputs map[string]buildOutput
	envVars      map[string]string // Per-target env vars set by 'env' command
}

// NewExecutor creates a new executor
func NewExecutor(bf *Buildfile, dryRun bool, extraArgs []string) *Executor {
	return &Executor{
		Buildfile:    bf,
		DryRun:       dryRun,
		ExtraArgs:    extraArgs,
		builtOutputs: make(map[string]buildOutput),
	}
}

// Run executes a target and its dependencies
func (e *Executor) Run(targetName string) error {
	targets, err := e.Buildfile.resolveDependencies(targetName)
	if err != nil {
		return err
	}

	for _, target := range targets {
		if err := e.executeTarget(target); err != nil {
			return err
		}
	}

	return nil
}

// executeTarget runs a single target
func (e *Executor) executeTarget(target *Target) error {
	// Check platform filter from target header
	if !target.shouldRunOnPlatform() {
		if !e.DryRun {
			fmt.Printf("skipping target %q (not for current platform)\n", target.Name)
		}
		return nil
	}

	// Check requires directive
	if err := target.checkRequires(); err != nil {
		return err
	}

	fmt.Printf("=== %s ===\n", target.Name)

	e.envVars = nil

	for _, cmd := range target.Commands {
		if err := e.executeCommand(cmd, target); err != nil {
			return fmt.Errorf("target %s: %w", target.Name, err)
		}
	}

	return nil
}

// executeCommand runs a single command
func (e *Executor) executeCommand(cmd Command, target *Target) error {
	// Check platform conditional
	if cmd.Platform != "" && cmd.Platform != runtime.GOOS {
		return nil
	}

	// Expand variables in arguments
	args := make([]string, len(cmd.Args))
	for i, arg := range cmd.Args {
		args[i] = e.Buildfile.expandVariables(arg)
	}

	if e.DryRun {
		if cmd.Platform != "" {
			fmt.Printf("  [@%s] %s %s\n", cmd.Platform, cmd.Type, strings.Join(args, " "))
		} else {
			fmt.Printf("  %s %s\n", cmd.Type, strings.Join(args, " "))
		}
		return nil
	}

	switch cmd.Type {
	case "gobuild":
		return e.handleGoBuild(args, target)
	case "run":
		return e.handleRun(args)
	case "copy":
		return e.handleCopy(args)
	case "mkdir":
		return e.handleMkdir(args)
	case "rm":
		return e.handleRm(args)
	case "sh":
		return e.handleSh(args)
	case "env":
		return e.handleEnv(args)
	default:
		return fmt.Errorf("unknown command type %q", cmd.Type)
	}
}

// ============================================================================
// Command Handlers
// ============================================================================

// handleGoBuild handles the gobuild command
func (e *Executor) handleGoBuild(args []string, target *Target) error {
	if len(args) < 1 {
		return fmt.Errorf("gobuild requires a package argument")
	}

	opts := buildOptions{
		Package: args[0],
		Build:   crossBuild{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	}

	// Parse flags
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("-o requires an argument")
			}
			i++
			opts.OutputName = filepath.Base(args[i])
			// Handle output directory
			if dir := filepath.Dir(args[i]); dir != "." {
				opts.OutputDir = dir
			}
		case "-os":
			if i+1 >= len(args) {
				return fmt.Errorf("-os requires an argument")
			}
			i++
			opts.Build.GOOS = args[i]
		case "-arch":
			if i+1 >= len(args) {
				return fmt.Errorf("-arch requires an argument")
			}
			i++
			opts.Build.GOARCH = args[i]
		case "-tags":
			if i+1 >= len(args) {
				return fmt.Errorf("-tags requires an argument")
			}
			i++
			opts.Tags = strings.Split(args[i], ",")
		case "-race":
			opts.RaceEnabled = true
		case "-cgo":
			opts.CgoEnabled = true
		case "-test":
			opts.BuildTests = true
		case "-entitlements":
			if i+1 >= len(args) {
				return fmt.Errorf("-entitlements requires an argument")
			}
			i++
			opts.EntitlementsPath = args[i]
		case "-app":
			opts.BuildApp = true
		case "-logo":
			if i+1 >= len(args) {
				return fmt.Errorf("-logo requires an argument")
			}
			i++
			opts.LogoPath = args[i]
		case "-icon":
			if i+1 >= len(args) {
				return fmt.Errorf("-icon requires an argument")
			}
			i++
			opts.IconPath = args[i]
		case "-version":
			if i+1 >= len(args) {
				return fmt.Errorf("-version requires an argument")
			}
			i++
			opts.Version = args[i]
		case "-shared":
			opts.BuildShared = true
		case "-appname":
			if i+1 >= len(args) {
				return fmt.Errorf("-appname requires an argument")
			}
			i++
			opts.ApplicationName = args[i]
		default:
			return fmt.Errorf("unknown gobuild flag: %s", args[i])
		}
	}

	// Default output name from package
	if opts.OutputName == "" {
		opts.OutputName = filepath.Base(opts.Package)
	}

	out, err := goBuild(opts)
	if err != nil {
		return err
	}

	// Store the output for later use
	e.builtOutputs[target.Name] = out
	fmt.Printf("built %s\n", out.Path)

	return nil
}

// handleRun handles the run command
func (e *Executor) handleRun(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("run requires a binary argument")
	}

	binary := args[0]
	runArgs := args[1:]

	// Append extra args from command line
	runArgs = append(runArgs, e.ExtraArgs...)

	out := buildOutput{Path: binary}

	return runBuildOutput(out, runArgs, runOptions{}, e.getEnv())
}

// handleCopy handles the copy command
func (e *Executor) handleCopy(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("copy requires exactly 2 arguments (src dst)")
	}

	return copyFile(args[1], args[0], 0644)
}

// handleMkdir handles the mkdir command
func (e *Executor) handleMkdir(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("mkdir requires at least 1 argument")
	}

	for _, dir := range args {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// handleRm handles the rm command
func (e *Executor) handleRm(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("rm requires at least 1 argument")
	}

	for _, path := range args {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

// handleSh handles the sh command
func (e *Executor) handleSh(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("sh requires a command")
	}

	// Join args back into a command string
	cmdStr := strings.Join(args, " ")

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = e.getEnv()

	return cmd.Run()
}

// handleEnv sets environment variables for subsequent commands in this target
func (e *Executor) handleEnv(args []string) error {
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("env: expected KEY=VALUE, got %q", arg)
		}
		if e.envVars == nil {
			e.envVars = make(map[string]string)
		}
		e.envVars[parts[0]] = parts[1]
	}
	return nil
}

// getEnv returns the environment for child processes, including any env vars
// set by 'env' commands. Returns nil if no extra env vars are set (and no
// Windows MSVC env fix is needed).
func (e *Executor) getEnv() []string {
	vcEnv := getVCVarsEnv()

	if len(e.envVars) == 0 && vcEnv == nil {
		return nil
	}

	var env []string
	if vcEnv != nil {
		env = vcEnv
	} else {
		env = os.Environ()
	}

	for k, v := range e.envVars {
		env = append(env, k+"="+v)
	}
	return env
}

// vcVarsEnvCached caches the result of getVCVarsEnv so we only run
// vcvarsall.bat once per process invocation.
var vcVarsEnvCached struct {
	done bool
	env  []string
}

// getVCVarsEnv returns a full environment with MSVC tools configured.
// On Windows, it runs vcvarsall.bat x64 and captures the resulting
// environment. This ensures the MSVC linker, libraries, and include paths
// are all properly set (avoiding issues like Git's link.exe shadowing
// the MSVC linker). Returns nil on non-Windows or if detection fails.
func getVCVarsEnv() []string {
	if vcVarsEnvCached.done {
		return vcVarsEnvCached.env
	}
	vcVarsEnvCached.done = true

	if runtime.GOOS != "windows" {
		return nil
	}

	// Use vswhere to find the Visual Studio installation path.
	programFiles := os.Getenv("ProgramFiles(x86)")
	if programFiles == "" {
		return nil
	}
	vswhere := filepath.Join(programFiles, "Microsoft Visual Studio", "Installer", "vswhere.exe")
	cmd := exec.Command(vswhere, "-latest", "-property", "installationPath")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	vsPath := strings.TrimSpace(string(out))
	if vsPath == "" {
		return nil
	}

	vcvarsall := filepath.Join(vsPath, "VC", "Auxiliary", "Build", "vcvarsall.bat")
	if _, err := os.Stat(vcvarsall); err != nil {
		return nil
	}

	// Run vcvarsall.bat and capture the resulting environment.
	// Use a temp batch file to avoid Go's command-line escaping interacting
	// badly with cmd.exe's quote parsing.
	tmpFile, err := os.CreateTemp("", "vcvars*.bat")
	if err != nil {
		return nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	fmt.Fprintf(tmpFile, "@call \"%s\" x64 >nul 2>&1\r\n@set\r\n", vcvarsall)
	tmpFile.Close()

	cmd = exec.Command("cmd", "/c", tmpPath)
	out, err = cmd.Output()
	if err != nil {
		return nil
	}

	var env []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.Contains(line, "=") {
			env = append(env, line)
		}
	}

	if len(env) == 0 {
		return nil
	}

	vcVarsEnvCached.env = env
	return env
}

// ============================================================================
// Build System Core (preserved from original)
// ============================================================================

type crossBuild struct {
	GOOS   string
	GOARCH string
}

func (cb crossBuild) IsNative() bool {
	return cb.GOOS == runtime.GOOS && cb.GOARCH == runtime.GOARCH
}

func (cb crossBuild) OutputName(name string) string {
	suffix := ""
	if cb.GOOS == "windows" && filepath.Ext(name) != ".dll" {
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

type buildOptions struct {
	Package          string
	ApplicationName  string
	OutputName       string
	OutputDir        string
	CgoEnabled       bool
	Build            crossBuild
	RaceEnabled      bool
	EntitlementsPath string
	BuildTests       bool
	Tags             []string
	BuildApp         bool
	LogoPath         string
	IconPath         string
	Version          string
	BuildShared      bool
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
		if f.PkgPath != "" {
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
		case reflect.Slice:
			if fv.Len() == 0 {
				continue
			}
			arrayStart := xml.StartElement{Name: xml.Name{Local: "array"}}
			if err := enc.EncodeToken(arrayStart); err != nil {
				return nil, fmt.Errorf("plist: encode <array> for %q: %w", key, err)
			}
			elemKind := fv.Type().Elem().Kind()
			for j := 0; j < fv.Len(); j++ {
				elem := fv.Index(j)
				switch elemKind {
				case reflect.String:
					if err := enc.EncodeElement(elem.String(), xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
						return nil, fmt.Errorf("plist: encode string in array %q: %w", key, err)
					}
				case reflect.Struct:
					if err := encodePlistDict(enc, elem); err != nil {
						return nil, fmt.Errorf("plist: encode dict in array %q: %w", key, err)
					}
				default:
					return nil, fmt.Errorf("plist: unsupported slice element kind %s for key %q", elemKind, key)
				}
			}
			if err := enc.EncodeToken(arrayStart.End()); err != nil {
				return nil, fmt.Errorf("plist: encode </array> for %q: %w", key, err)
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

func encodePlistDict(enc *xml.Encoder, rv reflect.Value) error {
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", rv.Kind())
	}
	rt := rv.Type()

	dictStart := xml.StartElement{Name: xml.Name{Local: "dict"}}
	if err := enc.EncodeToken(dictStart); err != nil {
		return fmt.Errorf("encode <dict>: %w", err)
	}

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
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
			return fmt.Errorf("encode key %q: %w", key, err)
		}

		switch fv.Kind() {
		case reflect.String:
			if err := enc.EncodeElement(fv.String(), xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
				return fmt.Errorf("encode string %q: %w", key, err)
			}
		case reflect.Bool:
			elem := "false"
			if fv.Bool() {
				elem = "true"
			}
			start := xml.StartElement{Name: xml.Name{Local: elem}}
			if err := enc.EncodeToken(start); err != nil {
				return fmt.Errorf("encode <%s/> for %q: %w", elem, key, err)
			}
			if err := enc.EncodeToken(start.End()); err != nil {
				return fmt.Errorf("encode </%s> for %q: %w", elem, key, err)
			}
		case reflect.Slice:
			if fv.Len() == 0 {
				continue
			}
			arrayStart := xml.StartElement{Name: xml.Name{Local: "array"}}
			if err := enc.EncodeToken(arrayStart); err != nil {
				return fmt.Errorf("encode <array> for %q: %w", key, err)
			}
			for j := 0; j < fv.Len(); j++ {
				elem := fv.Index(j)
				if elem.Kind() == reflect.String {
					if err := enc.EncodeElement(elem.String(), xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
						return fmt.Errorf("encode string in array %q: %w", key, err)
					}
				}
			}
			if err := enc.EncodeToken(arrayStart.End()); err != nil {
				return fmt.Errorf("encode </array> for %q: %w", key, err)
			}
		default:
			return fmt.Errorf("unsupported kind %s for key %q", fv.Kind(), key)
		}
	}

	if err := enc.EncodeToken(dictStart.End()); err != nil {
		return fmt.Errorf("encode </dict>: %w", err)
	}
	return nil
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

	var iconFileName string
	if logoPath != "" {
		iconFileName = filepath.Base(logoPath)
		iconDst := filepath.Join(resourcesDir, iconFileName)
		if err := copyFile(iconDst, logoPath, 0644); err != nil {
			return fmt.Errorf("copy app icon: %w", err)
		}
	}

	bundleID := "com.tinyrange." + strings.ToLower(appName)

	type urlSchemeType struct {
		CFBundleURLName    string   `plist:"CFBundleURLName"`
		CFBundleURLSchemes []string `plist:"CFBundleURLSchemes"`
	}

	type infoPlist struct {
		CFBundleDevelopmentRegion     string          `plist:"CFBundleDevelopmentRegion"`
		CFBundleExecutable            string          `plist:"CFBundleExecutable"`
		CFBundleIdentifier            string          `plist:"CFBundleIdentifier"`
		CFBundleInfoDictionaryVersion string          `plist:"CFBundleInfoDictionaryVersion"`
		CFBundleName                  string          `plist:"CFBundleName"`
		CFBundlePackageType           string          `plist:"CFBundlePackageType"`
		CFBundleShortVersionString    string          `plist:"CFBundleShortVersionString"`
		CFBundleVersion               string          `plist:"CFBundleVersion"`
		NSHighResolutionCapable       bool            `plist:"NSHighResolutionCapable"`
		CFBundleIconFile              string          `plist:"CFBundleIconFile"`
		CFBundleURLTypes              []urlSchemeType `plist:"CFBundleURLTypes"`
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
		CFBundleURLTypes: []urlSchemeType{{
			CFBundleURLName:    "CrumbleCracker URL",
			CFBundleURLSchemes: []string{"crumblecracker"},
		}},
	})
	if err != nil {
		return fmt.Errorf("encode Info.plist: %w", err)
	}

	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), plistData, 0644); err != nil {
		return fmt.Errorf("write Info.plist: %w", err)
	}

	return nil
}

func generateWindowsResources(pkgPath, iconPath, arch string) (string, error) {
	if iconPath == "" {
		return "", nil
	}

	if _, err := os.Stat(iconPath); os.IsNotExist(err) {
		return "", nil
	}

	absIconPath, err := filepath.Abs(iconPath)
	if err != nil {
		return "", fmt.Errorf("resolve icon path: %w", err)
	}

	absPkgPath, err := filepath.Abs(pkgPath)
	if err != nil {
		return "", fmt.Errorf("resolve package path: %w", err)
	}

	relIconPath, err := filepath.Rel(absPkgPath, absIconPath)
	if err != nil {
		return "", fmt.Errorf("calculate relative icon path: %w", err)
	}

	winresConfig := map[string]any{
		"RT_GROUP_ICON": map[string]any{
			"APP": map[string]any{
				"0000": relIconPath,
			},
		},
	}

	configBytes, err := json.MarshalIndent(winresConfig, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal winres config: %w", err)
	}

	configPath := filepath.Join(absPkgPath, "winres.json")
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		return "", fmt.Errorf("write winres config: %w", err)
	}
	defer os.Remove(configPath)

	cmd := exec.Command("go", "run", "github.com/tc-hib/go-winres@latest", "make",
		"--in", configPath,
		"--out", filepath.Join(absPkgPath, "rsrc"),
		"--arch", arch,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run go-winres: %w", err)
	}

	sysoPath := filepath.Join(absPkgPath, fmt.Sprintf("rsrc_windows_%s.syso", arch))
	return sysoPath, nil
}

func goBuild(opts buildOptions) (buildOutput, error) {
	outputDir := "build"
	if opts.OutputDir != "" {
		outputDir = opts.OutputDir
	}

	output := filepath.Join(outputDir, opts.Build.OutputName(opts.OutputName))
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
	if opts.CgoEnabled || opts.RaceEnabled || opts.BuildShared {
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
	} else if opts.BuildShared {
		args = []string{"go", "build", "-buildmode=c-shared", "-o", output}
	} else {
		args = []string{"go", "build", "-o", output}
	}

	if len(opts.Tags) > 0 {
		args = append(args, "-tags", strings.Join(opts.Tags, " "))
	}

	var ldflags []string

	if opts.Version != "" {
		ldflags = append(ldflags, fmt.Sprintf("-X %s/cmd/ccapp.Version=%s", PACKAGE_NAME, opts.Version))
	}

	if opts.Build.GOOS == "windows" && opts.BuildApp {
		ldflags = append(ldflags, "-H windowsgui")
	}

	if len(ldflags) > 0 {
		args = append(args, "-ldflags="+strings.Join(ldflags, " "))
	}

	var sysoPath string
	if opts.Build.GOOS == "windows" && opts.BuildApp && opts.IconPath != "" {
		var err error
		sysoPath, err = generateWindowsResources(opts.Package, opts.IconPath, opts.Build.GOARCH)
		if err != nil {
			return buildOutput{}, fmt.Errorf("failed to generate Windows resources: %w", err)
		}
		if sysoPath != "" {
			defer os.Remove(sysoPath)
		}
	}

	args = append(args, pkg)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return buildOutput{}, fmt.Errorf("go build failed: %w", err)
	}

	if opts.EntitlementsPath != "" && opts.Build.GOOS == "darwin" && opts.Build.GOARCH == "arm64" {
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
		appName := opts.ApplicationName
		if appName == "" {
			appName = opts.OutputName
		}
		if err := writeMacOSAppBundle(macosBundlePath, output, appName, opts.LogoPath); err != nil {
			return buildOutput{}, fmt.Errorf("failed to create macOS app bundle: %w", err)
		}
		return buildOutput{Path: macosBundlePath}, nil
	}

	return buildOutput{Path: output}, nil
}

type runOptions struct {
	CpuProfilePath string
	MemProfilePath string
}

func runBuildOutput(output buildOutput, args []string, opts runOptions, env []string) error {
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
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run app bundle: %w", err)
		}
		return nil
	}

	if opts.CpuProfilePath != "" {
		args = append([]string{"-cpuprofile", opts.CpuProfilePath}, args...)
	}

	if opts.MemProfilePath != "" {
		args = append([]string{"-memprofile", opts.MemProfilePath}, args...)
	}

	cmd := exec.Command(output.Path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env

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

func getVersionFromGit() string {
	if ref := os.Getenv("GITHUB_REF_NAME"); ref != "" && strings.HasPrefix(ref, "v") {
		return ref
	}

	cmd := exec.Command("git", "describe", "--tags", "--always")
	out, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(out))
		if version != "" {
			return version
		}
	}

	return "dev"
}

// ============================================================================
// CLI Interface
// ============================================================================

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: %s [options] [target] [-- args...]

Options:
  -f <file>      Use specified Buildfile (default: tools/Buildfile)
  --dry-run      Show what would be done without executing
  --list         List all available targets
  -h, --help     Show this help message

Arguments after -- are passed to 'run' commands.

Examples:
  %s                    Build default target
  %s cc                 Build the cc target
  %s bringup            Build and run bringup tests
  %s --list             List all targets
  %s cc -- --help       Build cc and pass --help to run commands
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

func main() {
	// Parse arguments manually to handle -- separator
	var buildfilePath string
	var dryRun bool
	var listTargets bool
	var showHelp bool
	var targetName string
	var extraArgs []string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			extraArgs = args[i+1:]
			break
		}

		switch arg {
		case "-f":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "-f requires an argument\n")
				os.Exit(1)
			}
			i++
			buildfilePath = args[i]
		case "--dry-run":
			dryRun = true
		case "--list":
			listTargets = true
		case "-h", "--help":
			showHelp = true
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(os.Stderr, "unknown option: %s\n", arg)
				usage()
				os.Exit(1)
			}
			if targetName == "" {
				targetName = arg
			} else {
				fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", arg)
				usage()
				os.Exit(1)
			}
		}
	}

	if showHelp {
		usage()
		os.Exit(0)
	}

	// Default buildfile path
	if buildfilePath == "" {
		buildfilePath = filepath.Join("tools", "Buildfile")
	}

	// Read and parse buildfile
	content, err := os.ReadFile(buildfilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read buildfile: %v\n", err)
		os.Exit(1)
	}

	bf, err := parseBuildfile(buildfilePath, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse buildfile: %v\n", err)
		os.Exit(1)
	}

	// List targets
	if listTargets {
		var names []string
		for name := range bf.Targets {
			names = append(names, name)
		}
		sort.Strings(names)

		fmt.Println("Available targets:")
		for _, name := range names {
			target := bf.Targets[name]
			platforms := ""
			if len(target.Platforms) > 0 {
				platforms = fmt.Sprintf(" [%s]", strings.Join(target.Platforms, " "))
			}
			requires := ""
			if len(target.Requires) > 0 {
				requires = fmt.Sprintf(" (requires: %s)", strings.Join(target.Requires, ", "))
			}
			deps := ""
			if len(target.Dependencies) > 0 {
				deps = fmt.Sprintf(" <- %s", strings.Join(target.Dependencies, ", "))
			}
			fmt.Printf("  %s%s%s%s\n", name, platforms, requires, deps)
		}
		os.Exit(0)
	}

	// Default target
	if targetName == "" {
		targetName = "default"
		if _, ok := bf.Targets["default"]; !ok {
			fmt.Fprintf(os.Stderr, "no target specified and no 'default' target found\n")
			usage()
			os.Exit(1)
		}
	}

	// Check target exists
	if _, ok := bf.Targets[targetName]; !ok {
		fmt.Fprintf(os.Stderr, "target %q not found\n", targetName)
		os.Exit(1)
	}

	// Execute
	executor := NewExecutor(bf, dryRun, extraArgs)
	if err := executor.Run(targetName); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
