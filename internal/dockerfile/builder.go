package dockerfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/tinyrange/cc/internal/fslayer"
)

// Instance is a minimal interface for executing commands and writing files.
// This mirrors the api.Instance interface to avoid circular imports.
type Instance interface {
	CommandContext(ctx context.Context, name string, args ...string) Cmd
	WriteFile(name string, data []byte, perm fs.FileMode) error
}

// Cmd is a minimal interface for command execution.
type Cmd interface {
	Run() error
	SetEnv(env []string) Cmd
	SetDir(dir string) Cmd
}

// FSLayerOp represents an operation in the filesystem snapshot factory.
// This mirrors api.FSLayerOp to avoid circular imports.
type FSLayerOp interface {
	CacheKey() string
	Apply(ctx context.Context, inst Instance) error
}

// BuildContext provides access to files during COPY/ADD operations.
type BuildContext interface {
	// Open opens a file for reading.
	Open(path string) (io.ReadCloser, error)
	// Stat returns file info for a path.
	Stat(path string) (os.FileInfo, error)
	// Root returns the context root path (for validation).
	Root() string
}

// DirBuildContext implements BuildContext using a directory.
type DirBuildContext struct {
	root string
}

// NewDirBuildContext creates a BuildContext from a directory path.
func NewDirBuildContext(root string) (*DirBuildContext, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve context root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat context root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("context root is not a directory: %s", abs)
	}
	return &DirBuildContext{root: abs}, nil
}

func (c *DirBuildContext) Root() string {
	return c.root
}

func (c *DirBuildContext) Open(path string) (io.ReadCloser, error) {
	// Validate path
	if err := ValidatePath(c.root, path); err != nil {
		return nil, err
	}
	fullPath := filepath.Join(c.root, filepath.Clean(path))
	return os.Open(fullPath)
}

func (c *DirBuildContext) Stat(path string) (os.FileInfo, error) {
	// Validate path
	if err := ValidatePath(c.root, path); err != nil {
		return nil, err
	}
	fullPath := filepath.Join(c.root, filepath.Clean(path))
	return os.Stat(fullPath)
}

// Builder converts a parsed Dockerfile into filesystem layer operations.
type Builder struct {
	dockerfile *Dockerfile
	buildArgs  map[string]string
	context    BuildContext
}

// NewBuilder creates a new Builder for the given Dockerfile.
func NewBuilder(df *Dockerfile) *Builder {
	return &Builder{
		dockerfile: df,
		buildArgs:  make(map[string]string),
	}
}

// WithBuildArg sets a build argument value.
func (b *Builder) WithBuildArg(key, value string) *Builder {
	b.buildArgs[key] = value
	return b
}

// WithContext sets the build context for COPY/ADD operations.
func (b *Builder) WithContext(ctx BuildContext) *Builder {
	b.context = ctx
	return b
}

// BuildResult contains the result of building a Dockerfile.
type BuildResult struct {
	// Ops contains the filesystem layer operations to execute.
	Ops []FSLayerOp
	// ImageRef is the base image reference from FROM.
	ImageRef string
	// RuntimeConfig contains CMD, ENTRYPOINT, USER, etc.
	RuntimeConfig RuntimeConfig
	// Env contains accumulated environment variables.
	Env []string
	// WorkDir contains the final working directory.
	WorkDir string
}

// Build converts the Dockerfile into filesystem layer operations.
func (b *Builder) Build() (*BuildResult, error) {
	if len(b.dockerfile.Stages) == 0 {
		return nil, ErrMissingFrom
	}

	// For now, only support single-stage builds (last stage)
	stage := b.dockerfile.Stages[len(b.dockerfile.Stages)-1]

	// Initialize variables from global ARGs and build args
	vars := make(map[string]string)
	// First, set defaults from global ARGs
	for _, arg := range b.dockerfile.Args {
		vars[arg.Key] = arg.Value
	}
	// Then, override with provided build args
	for k, v := range b.buildArgs {
		vars[k] = v
	}

	// Re-expand the image reference with the complete variable set
	// This allows build args to override ARG defaults
	imageRef := stage.From.Image
	if stage.From.ImageTemplate != "" {
		expanded, err := ExpandVariables(stage.From.ImageTemplate, vars)
		if err != nil {
			return nil, &BuildError{Op: "FROM", Message: "variable expansion failed", Err: err}
		}
		imageRef = expanded
	}

	result := &BuildResult{
		ImageRef: imageRef,
		RuntimeConfig: RuntimeConfig{
			Shell:  DefaultShell(),
			Labels: make(map[string]string),
		},
	}

	// Process instructions
	for _, instr := range stage.Instructions {
		if err := b.processInstruction(instr, result, vars); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (b *Builder) processInstruction(instr Instruction, result *BuildResult, vars map[string]string) error {
	switch instr.Kind {
	case InstructionRun:
		return b.processRun(instr, result, vars)
	case InstructionCopy:
		return b.processCopy(instr, result, vars)
	case InstructionAdd:
		return b.processAdd(instr, result, vars)
	case InstructionEnv:
		return b.processEnv(instr, result, vars)
	case InstructionWorkDir:
		return b.processWorkDir(instr, result, vars)
	case InstructionArg:
		return b.processArg(instr, vars)
	case InstructionUser:
		return b.processUser(instr, result)
	case InstructionExpose:
		return b.processExpose(instr, result)
	case InstructionLabel:
		return b.processLabel(instr, result)
	case InstructionCmd:
		return b.processCmd(instr, result)
	case InstructionEntrypoint:
		return b.processEntrypoint(instr, result)
	case InstructionShell:
		return b.processShell(instr, result)
	case InstructionStopSignal:
		return b.processStopSignal(instr, result)
	default:
		return &UnsupportedError{Feature: instr.Kind.String(), Line: instr.Line}
	}
}

func (b *Builder) processRun(instr Instruction, result *BuildResult, vars map[string]string) error {
	var cmd []string

	isExec := instr.Flags["form"] == "exec"
	if isExec {
		// Exec form: args are the command
		cmd = instr.Args
	} else {
		// Shell form: wrap with shell
		cmd = append(result.RuntimeConfig.Shell, instr.Args[0])
	}

	// Expand variables in command
	for i, arg := range cmd {
		expanded, err := ExpandVariables(arg, vars)
		if err != nil {
			return &BuildError{Op: "RUN", Line: instr.Line, Message: "variable expansion failed", Err: err}
		}
		cmd[i] = expanded
	}

	op := &runOp{
		cmd:     cmd,
		env:     append([]string{}, result.Env...),
		workDir: result.WorkDir,
	}
	result.Ops = append(result.Ops, op)
	return nil
}

func (b *Builder) processCopy(instr Instruction, result *BuildResult, vars map[string]string) error {
	if b.context == nil {
		return &BuildError{Op: "COPY", Line: instr.Line, Message: "no build context provided"}
	}

	// Last arg is destination, rest are sources
	if len(instr.Args) < 2 {
		return &BuildError{Op: "COPY", Line: instr.Line, Message: "requires source and destination"}
	}

	srcs := instr.Args[:len(instr.Args)-1]
	dst := instr.Args[len(instr.Args)-1]

	// Expand variables in destination
	var err error
	dst, err = ExpandVariables(dst, vars)
	if err != nil {
		return &BuildError{Op: "COPY", Line: instr.Line, Message: "variable expansion failed", Err: err}
	}

	// Validate and process each source
	for _, src := range srcs {
		// Expand variables in source
		src, err = ExpandVariables(src, vars)
		if err != nil {
			return &BuildError{Op: "COPY", Line: instr.Line, Message: "variable expansion failed", Err: err}
		}

		// Validate source path
		if err := ValidatePath(b.context.Root(), src); err != nil {
			return &PathTraversalError{Path: src, Line: instr.Line}
		}

		// Read source content and compute hash
		rc, err := b.context.Open(src)
		if err != nil {
			return &BuildError{Op: "COPY", Line: instr.Line, Message: fmt.Sprintf("open source %q", src), Err: err}
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return &BuildError{Op: "COPY", Line: instr.Line, Message: fmt.Sprintf("read source %q", src), Err: err}
		}

		h := sha256.Sum256(data)
		contentHash := hex.EncodeToString(h[:])

		// Determine final destination path (use path package for Linux-style container paths)
		finalDst := dst
		if len(srcs) > 1 || isDir(dst) {
			// Multiple sources or directory destination: append filename
			finalDst = path.Join(dst, path.Base(src))
		}

		op := &readerOp{
			data:        data,
			dst:         finalDst,
			contentHash: contentHash,
			chown:       instr.Flags["chown"],
		}
		result.Ops = append(result.Ops, op)
	}

	return nil
}

func (b *Builder) processAdd(instr Instruction, result *BuildResult, vars map[string]string) error {
	// ADD is treated like COPY for local files
	// URL and archive extraction are not supported
	return b.processCopy(instr, result, vars)
}

func (b *Builder) processEnv(instr Instruction, result *BuildResult, vars map[string]string) error {
	for _, kv := range instr.Args {
		result.Env = append(result.Env, kv)
		// Also add to vars for subsequent expansion
		if idx := findEq(kv); idx != -1 {
			vars[kv[:idx]] = kv[idx+1:]
		}
	}
	return nil
}

func (b *Builder) processWorkDir(instr Instruction, result *BuildResult, vars map[string]string) error {
	if len(instr.Args) == 0 {
		return &BuildError{Op: "WORKDIR", Line: instr.Line, Message: "requires a path"}
	}

	dir, err := ExpandVariables(instr.Args[0], vars)
	if err != nil {
		return &BuildError{Op: "WORKDIR", Line: instr.Line, Message: "variable expansion failed", Err: err}
	}

	// Make absolute if relative (use path package for Linux-style paths)
	if !path.IsAbs(dir) {
		if result.WorkDir == "" {
			dir = "/" + dir
		} else {
			dir = path.Join(result.WorkDir, dir)
		}
	}

	result.WorkDir = dir

	// Add operation to create directory
	op := &runOp{
		cmd:     []string{"mkdir", "-p", dir},
		env:     result.Env,
		workDir: "",
	}
	result.Ops = append(result.Ops, op)

	return nil
}

func (b *Builder) processArg(instr Instruction, vars map[string]string) error {
	if len(instr.Args) >= 2 {
		name := instr.Args[0]
		defaultVal := instr.Args[1]
		if _, exists := vars[name]; !exists {
			vars[name] = defaultVal
		}
	} else if len(instr.Args) == 1 {
		// ARG with no default, add empty if not set
		name := instr.Args[0]
		if _, exists := vars[name]; !exists {
			vars[name] = ""
		}
	}
	return nil
}

func (b *Builder) processUser(instr Instruction, result *BuildResult) error {
	if len(instr.Args) > 0 {
		result.RuntimeConfig.User = instr.Args[0]
	}
	return nil
}

func (b *Builder) processExpose(instr Instruction, result *BuildResult) error {
	result.RuntimeConfig.ExposePorts = append(result.RuntimeConfig.ExposePorts, instr.Args...)
	return nil
}

func (b *Builder) processLabel(instr Instruction, result *BuildResult) error {
	for _, kv := range instr.Args {
		if idx := findEq(kv); idx != -1 {
			result.RuntimeConfig.Labels[kv[:idx]] = kv[idx+1:]
		}
	}
	return nil
}

func (b *Builder) processCmd(instr Instruction, result *BuildResult) error {
	isExec := instr.Flags["form"] == "exec"
	if isExec {
		result.RuntimeConfig.Cmd = instr.Args
	} else {
		// Shell form
		result.RuntimeConfig.Cmd = append(result.RuntimeConfig.Shell, instr.Args[0])
	}
	return nil
}

func (b *Builder) processEntrypoint(instr Instruction, result *BuildResult) error {
	isExec := instr.Flags["form"] == "exec"
	if isExec {
		result.RuntimeConfig.Entrypoint = instr.Args
	} else {
		// Shell form
		result.RuntimeConfig.Entrypoint = append(result.RuntimeConfig.Shell, instr.Args[0])
	}
	return nil
}

func (b *Builder) processShell(instr Instruction, result *BuildResult) error {
	result.RuntimeConfig.Shell = instr.Args
	return nil
}

func (b *Builder) processStopSignal(instr Instruction, result *BuildResult) error {
	if len(instr.Args) > 0 {
		result.RuntimeConfig.StopSignal = instr.Args[0]
	}
	return nil
}

// runOp implements FSLayerOp for RUN instructions.
type runOp struct {
	cmd     []string
	env     []string
	workDir string
}

func (o *runOp) CacheKey() string {
	return fslayer.RunOpKey(o.cmd, o.env, o.workDir)
}

func (o *runOp) Apply(ctx context.Context, inst Instance) error {
	cmd := inst.CommandContext(ctx, o.cmd[0], o.cmd[1:]...)
	if len(o.env) > 0 {
		cmd = cmd.SetEnv(o.env)
	}
	if o.workDir != "" {
		cmd = cmd.SetDir(o.workDir)
	}
	return cmd.Run()
}

// readerOp implements FSLayerOp for COPY/ADD instructions.
type readerOp struct {
	data        []byte
	dst         string
	contentHash string
	chown       string // Optional --chown flag value
}

func (o *readerOp) CacheKey() string {
	return fslayer.CopyOpKey("reader", o.dst, o.contentHash)
}

func (o *readerOp) Apply(ctx context.Context, inst Instance) error {
	// Write file
	if err := inst.WriteFile(o.dst, o.data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", o.dst, err)
	}

	// Handle --chown if specified
	// TODO: Implement chown support when needed

	return nil
}

// isDir returns true if the path looks like a directory (ends with /).
func isDir(path string) bool {
	return len(path) > 0 && path[len(path)-1] == '/'
}

// findEq finds the index of '=' in a string.
func findEq(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}
