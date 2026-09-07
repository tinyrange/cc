package termui

import (
	"io"

	"j5.nz/cc/frontends/vmsh/internal/termui/asciicast"
	"j5.nz/cc/frontends/vmsh/internal/termui/editor"
	"j5.nz/cc/frontends/vmsh/internal/termui/progress"
	"j5.nz/cc/frontends/vmsh/internal/termui/shell"
	"j5.nz/cc/frontends/vmsh/internal/termui/terminal"
	"j5.nz/cc/frontends/vmsh/internal/termui/wait"
)

var (
	ErrLineInterrupted = editor.ErrLineInterrupted
	ErrInterrupted     = wait.ErrInterrupted
)

type (
	Editor         = editor.Editor
	Options        = editor.Options
	Completer      = editor.Completer
	CompletionKind = editor.CompletionKind
	WaitSpec       = wait.Spec
	Progress       = progress.Snapshot
	Reporter       = progress.Reporter
	Capabilities   = terminal.Capabilities
	Mode           = terminal.Mode
	Shell          = shell.Shell
	ExternalRunner = shell.ExternalRunner
	Recorder       = asciicast.Recorder
)

const (
	CompletionNone    = editor.CompletionNone
	CompletionAt      = editor.CompletionAt
	CompletionCommand = editor.CompletionCommand
	CompletionPath    = editor.CompletionPath
	CompletionOption  = editor.CompletionOption

	ModeNonInteractive     = terminal.ModeNonInteractive
	ModePlainInteractive   = terminal.ModePlainInteractive
	ModeDynamicInteractive = terminal.ModeDynamicInteractive
)

func New(opts Options) *Editor {
	return editor.New(opts)
}

func NewReporter() *Reporter {
	return progress.NewReporter()
}

func NewShell(ed *Editor, stdin io.Reader, stdout, stderr io.Writer) *Shell {
	return shell.New(ed, stdin, stdout, stderr)
}

func CreateRecorder(path string, width, height int) (*Recorder, error) {
	return asciicast.Create(path, width, height)
}
