package dockerfile

import (
	"strings"
	"testing"
)

func TestParseSimpleDockerfile(t *testing.T) {
	input := `FROM alpine:3.19
RUN echo hello
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(df.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(df.Stages))
	}

	stage := df.Stages[0]
	if stage.From.Image != "alpine:3.19" {
		t.Errorf("expected image alpine:3.19, got %s", stage.From.Image)
	}

	if len(stage.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(stage.Instructions))
	}

	instr := stage.Instructions[0]
	if instr.Kind != InstructionRun {
		t.Errorf("expected RUN, got %s", instr.Kind)
	}
}

func TestParseFromWithTag(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FROM alpine:3.19\n", "alpine:3.19"},
		{"FROM alpine:latest\n", "alpine:latest"},
		{"FROM golang:1.22-alpine\n", "golang:1.22-alpine"},
		{"FROM ubuntu\n", "ubuntu"},
	}

	for _, tc := range tests {
		df, err := Parse([]byte(tc.input))
		if err != nil {
			t.Errorf("Parse(%q) failed: %v", tc.input, err)
			continue
		}
		if df.Stages[0].From.Image != tc.expected {
			t.Errorf("Parse(%q): expected %q, got %q", tc.input, tc.expected, df.Stages[0].From.Image)
		}
	}
}

func TestParseFromWithAlias(t *testing.T) {
	input := "FROM alpine:3.19 AS builder\n"
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if df.Stages[0].From.Alias != "builder" {
		t.Errorf("expected alias 'builder', got %q", df.Stages[0].From.Alias)
	}
	if df.Stages[0].Name != "builder" {
		t.Errorf("expected stage name 'builder', got %q", df.Stages[0].Name)
	}
}

func TestParseFromWithPlatform(t *testing.T) {
	input := "FROM --platform=linux/amd64 alpine:3.19\n"
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if df.Stages[0].From.Platform != "linux/amd64" {
		t.Errorf("expected platform 'linux/amd64', got %q", df.Stages[0].From.Platform)
	}
}

func TestParseRunShellForm(t *testing.T) {
	input := `FROM alpine
RUN echo "hello world"
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionRun {
		t.Fatalf("expected RUN, got %s", instr.Kind)
	}
	if instr.Flags["form"] != "shell" {
		t.Errorf("expected shell form, got %s", instr.Flags["form"])
	}
	if len(instr.Args) != 1 || instr.Args[0] != `echo "hello world"` {
		t.Errorf("unexpected args: %v", instr.Args)
	}
}

func TestParseRunExecForm(t *testing.T) {
	input := `FROM alpine
RUN ["echo", "hello", "world"]
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Flags["form"] != "exec" {
		t.Errorf("expected exec form, got %s", instr.Flags["form"])
	}
	if len(instr.Args) != 3 || instr.Args[0] != "echo" || instr.Args[1] != "hello" || instr.Args[2] != "world" {
		t.Errorf("unexpected args: %v", instr.Args)
	}
}

func TestParseRunWithContinuation(t *testing.T) {
	input := `FROM alpine
RUN apk add --no-cache \
    gcc \
    make \
    musl-dev
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	// The continuation adds a space after removing the backslash
	// Leading whitespace on continuation lines is preserved
	if !strings.Contains(instr.Args[0], "apk add --no-cache") {
		t.Errorf("expected command to contain 'apk add --no-cache', got %q", instr.Args[0])
	}
	if !strings.Contains(instr.Args[0], "gcc") {
		t.Errorf("expected command to contain 'gcc', got %q", instr.Args[0])
	}
}

func TestParseCopy(t *testing.T) {
	input := `FROM alpine
COPY src dest
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionCopy {
		t.Fatalf("expected COPY, got %s", instr.Kind)
	}
	if len(instr.Args) != 2 || instr.Args[0] != "src" || instr.Args[1] != "dest" {
		t.Errorf("unexpected args: %v", instr.Args)
	}
}

func TestParseCopyWithChown(t *testing.T) {
	input := `FROM alpine
COPY --chown=user:group src dest
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Flags["chown"] != "user:group" {
		t.Errorf("expected chown 'user:group', got %q", instr.Flags["chown"])
	}
}

func TestParseCopyMultipleSources(t *testing.T) {
	input := `FROM alpine
COPY file1 file2 file3 /dest/
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if len(instr.Args) != 4 {
		t.Errorf("expected 4 args, got %d: %v", len(instr.Args), instr.Args)
	}
}

func TestParseCopyFromUnsupported(t *testing.T) {
	input := `FROM alpine AS builder
RUN echo hello
FROM alpine
COPY --from=builder /src /dest
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for COPY --from")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got: %v", err)
	}
}

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single key=value",
			input:    "FROM alpine\nENV FOO=bar\n",
			expected: []string{"FOO=bar"},
		},
		{
			name:     "multiple key=value",
			input:    "FROM alpine\nENV FOO=bar BAZ=qux\n",
			expected: []string{"FOO=bar", "BAZ=qux"},
		},
		{
			name:     "legacy format",
			input:    "FROM alpine\nENV FOO bar\n",
			expected: []string{"FOO=bar"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			instr := df.Stages[0].Instructions[0]
			if instr.Kind != InstructionEnv {
				t.Fatalf("expected ENV, got %s", instr.Kind)
			}
			if len(instr.Args) != len(tc.expected) {
				t.Fatalf("expected %d args, got %d: %v", len(tc.expected), len(instr.Args), instr.Args)
			}
			for i, exp := range tc.expected {
				if instr.Args[i] != exp {
					t.Errorf("arg[%d]: expected %q, got %q", i, exp, instr.Args[i])
				}
			}
		})
	}
}

func TestParseWorkdir(t *testing.T) {
	input := `FROM alpine
WORKDIR /app
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionWorkDir {
		t.Fatalf("expected WORKDIR, got %s", instr.Kind)
	}
	if instr.Args[0] != "/app" {
		t.Errorf("expected /app, got %s", instr.Args[0])
	}
}

func TestParseArg(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		globalArgs  int
		stageInstr  int
		firstArgKey string
	}{
		{
			name:        "global ARG",
			input:       "ARG VERSION=1.0\nFROM alpine\n",
			globalArgs:  1,
			stageInstr:  0,
			firstArgKey: "VERSION",
		},
		{
			name:       "stage ARG",
			input:      "FROM alpine\nARG MYVAR=test\n",
			globalArgs: 0,
			stageInstr: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if len(df.Args) != tc.globalArgs {
				t.Errorf("expected %d global args, got %d", tc.globalArgs, len(df.Args))
			}
			if len(df.Stages[0].Instructions) != tc.stageInstr {
				t.Errorf("expected %d stage instructions, got %d", tc.stageInstr, len(df.Stages[0].Instructions))
			}
			if tc.globalArgs > 0 && df.Args[0].Key != tc.firstArgKey {
				t.Errorf("expected first arg key %q, got %q", tc.firstArgKey, df.Args[0].Key)
			}
		})
	}
}

func TestParseUser(t *testing.T) {
	input := `FROM alpine
USER nobody
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionUser {
		t.Fatalf("expected USER, got %s", instr.Kind)
	}
	if instr.Args[0] != "nobody" {
		t.Errorf("expected nobody, got %s", instr.Args[0])
	}
}

func TestParseExpose(t *testing.T) {
	input := `FROM alpine
EXPOSE 80 443/tcp
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionExpose {
		t.Fatalf("expected EXPOSE, got %s", instr.Kind)
	}
	if len(instr.Args) != 2 || instr.Args[0] != "80" || instr.Args[1] != "443/tcp" {
		t.Errorf("unexpected args: %v", instr.Args)
	}
}

func TestParseLabel(t *testing.T) {
	input := `FROM alpine
LABEL version="1.0" maintainer="test@example.com"
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionLabel {
		t.Fatalf("expected LABEL, got %s", instr.Kind)
	}
	if len(instr.Args) != 2 {
		t.Errorf("expected 2 labels, got %d: %v", len(instr.Args), instr.Args)
	}
}

func TestParseCmdShellForm(t *testing.T) {
	input := `FROM alpine
CMD echo hello
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionCmd {
		t.Fatalf("expected CMD, got %s", instr.Kind)
	}
	if instr.Flags["form"] != "shell" {
		t.Errorf("expected shell form")
	}
}

func TestParseCmdExecForm(t *testing.T) {
	input := `FROM alpine
CMD ["echo", "hello"]
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Flags["form"] != "exec" {
		t.Errorf("expected exec form")
	}
	if len(instr.Args) != 2 || instr.Args[0] != "echo" {
		t.Errorf("unexpected args: %v", instr.Args)
	}
}

func TestParseEntrypoint(t *testing.T) {
	input := `FROM alpine
ENTRYPOINT ["/entrypoint.sh"]
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionEntrypoint {
		t.Fatalf("expected ENTRYPOINT, got %s", instr.Kind)
	}
}

func TestParseShell(t *testing.T) {
	input := `FROM alpine
SHELL ["/bin/bash", "-c"]
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionShell {
		t.Fatalf("expected SHELL, got %s", instr.Kind)
	}
	if len(instr.Args) != 2 {
		t.Errorf("expected 2 args, got %v", instr.Args)
	}
}

func TestParseCommentsAndBlanks(t *testing.T) {
	input := `# This is a comment
FROM alpine

# Another comment
RUN echo hello

`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(df.Stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(df.Stages))
	}
	if len(df.Stages[0].Instructions) != 1 {
		t.Errorf("expected 1 instruction, got %d", len(df.Stages[0].Instructions))
	}
}

func TestParseVariableSubstitution(t *testing.T) {
	input := `ARG VERSION=1.0
FROM alpine:$VERSION
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if df.Stages[0].From.Image != "alpine:1.0" {
		t.Errorf("expected alpine:1.0, got %s", df.Stages[0].From.Image)
	}
}

func TestParseMultiStage(t *testing.T) {
	// Note: We don't support COPY --from, but we do parse multiple stages
	input := `FROM alpine AS builder
RUN echo build

FROM alpine
RUN echo run
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(df.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(df.Stages))
	}
	if df.Stages[0].Name != "builder" {
		t.Errorf("expected stage[0] name 'builder', got %q", df.Stages[0].Name)
	}
}

func TestParseNoFrom(t *testing.T) {
	input := `RUN echo hello`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing FROM")
	}
}

func TestParseUnsupportedInstruction(t *testing.T) {
	unsupported := []string{
		"FROM alpine\nVOLUME /data\n",
		"FROM alpine\nHEALTHCHECK CMD curl localhost\n",
		"FROM alpine\nONBUILD RUN echo\n",
	}

	for _, input := range unsupported {
		_, err := Parse([]byte(input))
		if err == nil {
			t.Errorf("expected error for: %s", input)
		}
	}
}

func TestParseStopSignal(t *testing.T) {
	input := `FROM alpine
STOPSIGNAL SIGTERM
`
	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(df.Stages[0].Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(df.Stages[0].Instructions))
	}

	instr := df.Stages[0].Instructions[0]
	if instr.Kind != InstructionStopSignal {
		t.Errorf("expected STOPSIGNAL, got %s", instr.Kind)
	}
	if instr.Args[0] != "SIGTERM" {
		t.Errorf("expected SIGTERM, got %s", instr.Args[0])
	}
}

func TestParseHeredoc(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "shell heredoc in RUN",
			input: `FROM alpine
RUN cat > /test.sh <<'EOF'
#!/bin/sh
echo "hello"
EOF
RUN chmod +x /test.sh
`,
		},
		{
			name: "multiple heredocs in RUN",
			input: `FROM alpine
RUN cat > /a.txt <<'EOF1' && cat > /b.txt <<'EOF2'
content a
EOF1
content b
EOF2
`,
		},
		{
			name: "heredoc with variable syntax",
			input: `FROM alpine
RUN cat > /script.sh <<EOF
echo $PATH
echo ${HOME}
EOF
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if len(df.Stages) == 0 {
				t.Error("expected at least one stage")
			}

			// Verify we got the expected number of instructions
			// (RUN with heredoc should be one instruction)
			if len(df.Stages[0].Instructions) == 0 {
				t.Error("expected at least one instruction")
			}
		})
	}
}

func TestParseUnknownInstruction(t *testing.T) {
	input := `FROM alpine
FOOBAR something
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for unknown instruction")
	}
	if !strings.Contains(err.Error(), "unknown instruction") {
		t.Errorf("expected 'unknown instruction' error, got: %v", err)
	}
}

func TestParseRunWithCommentInContinuation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "comment without backslash in continuation",
			input: `FROM alpine
RUN echo "test" \
    && echo "more" \
    # This comment doesn't end with backslash
    && echo "end"
`,
		},
		{
			name: "multiple comments in continuation",
			input: `FROM alpine
RUN echo "start" \
    # comment 1
    && echo "middle" \
    # comment 2
    && echo "end"
`,
		},
		{
			name: "comment with backslash still works",
			input: `FROM alpine
RUN echo "test" \
    # This comment DOES end with backslash \
    && echo "end"
`,
		},
		{
			name: "comment at start of continuation",
			input: `FROM alpine
RUN echo "test" \
    # comment at start
    && echo "end"
`,
		},
		{
			name: "empty line in continuation",
			input: `FROM alpine
RUN echo "test" \

    && echo "end"
`,
		},
		{
			name: "empty line and comment in continuation",
			input: `FROM alpine
RUN echo "test" \

    # comment after empty line
    && echo "end"
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(df.Stages) != 1 {
				t.Errorf("expected 1 stage, got %d", len(df.Stages))
			}
			if len(df.Stages[0].Instructions) != 1 {
				t.Errorf("expected 1 RUN instruction, got %d", len(df.Stages[0].Instructions))
			}
		})
	}
}

func TestParseRealDockerfile(t *testing.T) {
	// Test parsing a real Dockerfile from the tests directory
	input := `FROM alpine:latest

# Install build dependencies
RUN apk add --no-cache \
    gcc \
    make \
    musl-dev \
    wget \
    tar \
    bash

# Download and extract the hello archive
WORKDIR /build
RUN wget -q https://ftp.gnu.org/gnu/hello/hello-2.12.tar.gz && \
    tar -xzf hello-2.12.tar.gz && \
    rm hello-2.12.tar.gz

# Create entrypoint script
RUN echo '#!/bin/bash' > /entrypoint.sh && \
    echo 'set -e' >> /entrypoint.sh && \
    echo 'cd /build/hello-2.12' >> /entrypoint.sh && \
    echo './configure' >> /entrypoint.sh && \
    echo 'make' >> /entrypoint.sh && \
    echo 'make install' >> /entrypoint.sh && \
    echo 'exec "$@"' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["hello"]
`

	df, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(df.Stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(df.Stages))
	}
	if df.Stages[0].From.Image != "alpine:latest" {
		t.Errorf("expected alpine:latest, got %s", df.Stages[0].From.Image)
	}

	// Check we got all the instructions
	// RUN (apk), WORKDIR, RUN (wget), RUN (echo), ENTRYPOINT, CMD
	if len(df.Stages[0].Instructions) != 6 {
		t.Errorf("expected 6 instructions, got %d", len(df.Stages[0].Instructions))
	}
}
