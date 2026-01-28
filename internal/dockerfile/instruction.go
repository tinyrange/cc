package dockerfile

// InstructionKind identifies the type of Dockerfile instruction.
type InstructionKind int

const (
	InstructionFrom InstructionKind = iota
	InstructionRun
	InstructionCopy
	InstructionAdd
	InstructionEnv
	InstructionWorkDir
	InstructionArg
	InstructionLabel
	InstructionUser
	InstructionExpose
	InstructionCmd
	InstructionEntrypoint
	InstructionShell
	InstructionStopSignal
)

func (k InstructionKind) String() string {
	switch k {
	case InstructionFrom:
		return "FROM"
	case InstructionRun:
		return "RUN"
	case InstructionCopy:
		return "COPY"
	case InstructionAdd:
		return "ADD"
	case InstructionEnv:
		return "ENV"
	case InstructionWorkDir:
		return "WORKDIR"
	case InstructionArg:
		return "ARG"
	case InstructionLabel:
		return "LABEL"
	case InstructionUser:
		return "USER"
	case InstructionExpose:
		return "EXPOSE"
	case InstructionCmd:
		return "CMD"
	case InstructionEntrypoint:
		return "ENTRYPOINT"
	case InstructionShell:
		return "SHELL"
	case InstructionStopSignal:
		return "STOPSIGNAL"
	default:
		return "UNKNOWN"
	}
}

// Instruction represents a single parsed Dockerfile instruction.
type Instruction struct {
	Kind     InstructionKind
	Line     int               // Source line number (1-indexed)
	Original string            // Original instruction text
	Args     []string          // Parsed arguments
	Flags    map[string]string // Flags like --from, --chown, etc.
}

// FromInstruction holds parsed FROM instruction details.
type FromInstruction struct {
	Image         string // Image reference after variable expansion
	ImageTemplate string // Original image reference before expansion (may contain $VAR)
	Tag           string // Tag portion if separated (empty if included in Image)
	Alias         string // Stage alias from "AS name"
	Platform      string // Platform from --platform flag
}

// KeyValue represents a key-value pair (for ARG, ENV, LABEL).
type KeyValue struct {
	Key   string
	Value string
}

// Stage represents a build stage in a Dockerfile.
type Stage struct {
	Name         string // Stage name from "AS name" (empty if unnamed)
	From         FromInstruction
	Instructions []Instruction
}

// Dockerfile represents a complete parsed Dockerfile.
type Dockerfile struct {
	Stages []Stage    // Build stages (at least one)
	Args   []KeyValue // Global ARGs declared before first FROM
}

// RuntimeConfig holds metadata from CMD, ENTRYPOINT, USER, EXPOSE, LABEL, SHELL, STOPSIGNAL.
// This metadata does not affect the filesystem but may be used by the runtime.
type RuntimeConfig struct {
	Cmd         []string          // CMD instruction
	Entrypoint  []string          // ENTRYPOINT instruction
	User        string            // USER instruction
	ExposePorts []string          // EXPOSE instructions (e.g., "80/tcp")
	Labels      map[string]string // LABEL instructions
	Shell       []string          // SHELL instruction (default: ["/bin/sh", "-c"])
	StopSignal  string            // STOPSIGNAL instruction (e.g., "SIGTERM")
}

// DefaultShell returns the default shell used for shell-form RUN commands.
func DefaultShell() []string {
	return []string{"/bin/sh", "-c"}
}
