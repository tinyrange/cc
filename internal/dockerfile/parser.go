package dockerfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Parse parses a Dockerfile from its byte content.
func Parse(data []byte) (*Dockerfile, error) {
	// Security: validate size
	if err := ValidateDockerfileSize(data); err != nil {
		return nil, err
	}

	p := &parser{
		vars:   make(map[string]string),
		result: &Dockerfile{},
	}

	return p.parse(data)
}

// parser holds state during parsing.
type parser struct {
	vars             map[string]string // ARG/ENV variables
	result           *Dockerfile
	currentStage     *Stage
	instructionCount int
}

// heredocPattern matches heredoc markers: <<EOF, <<'EOF', <<"EOF", <<-EOF
var heredocPattern = regexp.MustCompile(`<<-?['"]?(\w+)['"]?`)

func (p *parser) parse(data []byte) (*Dockerfile, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, MaxLineLength), MaxLineLength)

	lineNum := 0
	var continuation strings.Builder
	continuationStartLine := 0

	// For heredoc handling
	var heredocDelimiters []string
	var heredocContent strings.Builder
	inHeredoc := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Check line length
		if len(line) > MaxLineLength {
			return nil, &ParseError{
				Line:    lineNum,
				Message: "line exceeds maximum length",
			}
		}

		// If we're inside a heredoc, check for the delimiter
		if inHeredoc {
			trimmedLine := strings.TrimSpace(line)
			if len(heredocDelimiters) > 0 && trimmedLine == heredocDelimiters[0] {
				// Found the delimiter, close this heredoc
				heredocDelimiters = heredocDelimiters[1:]
				if len(heredocDelimiters) == 0 {
					// All heredocs closed
					inHeredoc = false
					// heredocContent now contains the full instruction with heredoc content
				}
			} else {
				// Still inside heredoc, accumulate content
				heredocContent.WriteString(line)
				heredocContent.WriteByte('\n')
			}
			continue
		}

		// Handle continuation
		trimmed := strings.TrimRightFunc(line, unicode.IsSpace)

		// If we're in a continuation and this is an empty or comment-only line (without backslash),
		// skip it but DON'T end the continuation
		if continuation.Len() > 0 {
			stripped := strings.TrimSpace(trimmed)
			if !strings.HasSuffix(trimmed, "\\") {
				if stripped == "" || strings.HasPrefix(stripped, "#") {
					// Empty or comment line in middle of continuation - skip without ending continuation
					continue
				}
			}
		}

		if strings.HasSuffix(trimmed, "\\") {
			if continuation.Len() == 0 {
				continuationStartLine = lineNum
			}
			// Remove trailing backslash and add space
			continuation.WriteString(strings.TrimSuffix(trimmed, "\\"))
			continuation.WriteByte(' ')
			continue
		}

		// Complete the line (with any continuation)
		var fullLine string
		var effectiveLine int
		if continuation.Len() > 0 {
			continuation.WriteString(trimmed)
			fullLine = continuation.String()
			effectiveLine = continuationStartLine
			continuation.Reset()
		} else {
			fullLine = trimmed
			effectiveLine = lineNum
		}

		// Skip empty lines and comments
		stripped := strings.TrimSpace(fullLine)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}

		// Check for heredoc markers in the line
		heredocDelimiters = findHeredocDelimiters(stripped)
		if len(heredocDelimiters) > 0 {
			// Start heredoc mode
			inHeredoc = true
			heredocContent.Reset()
			heredocContent.WriteString(stripped)
			heredocContent.WriteByte('\n')
			continuationStartLine = effectiveLine
			continue
		}

		// Parse the instruction
		if err := p.parseInstruction(stripped, effectiveLine); err != nil {
			return nil, err
		}

		// Security: check instruction count
		if p.instructionCount > MaxInstructionCount {
			return nil, ErrTooManyInstructions
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, &ParseError{Message: "read error: " + err.Error()}
	}

	// Handle any remaining heredoc content
	if heredocContent.Len() > 0 {
		stripped := strings.TrimSpace(heredocContent.String())
		if stripped != "" && !strings.HasPrefix(stripped, "#") {
			if err := p.parseInstruction(stripped, continuationStartLine); err != nil {
				return nil, err
			}
		}
	}

	// Handle any remaining continuation
	if continuation.Len() > 0 {
		stripped := strings.TrimSpace(continuation.String())
		if stripped != "" && !strings.HasPrefix(stripped, "#") {
			if err := p.parseInstruction(stripped, continuationStartLine); err != nil {
				return nil, err
			}
		}
	}

	// Finalize current stage
	if p.currentStage != nil {
		p.result.Stages = append(p.result.Stages, *p.currentStage)
	}

	// Validate: must have at least one stage
	if len(p.result.Stages) == 0 {
		return nil, &ParseError{Message: "dockerfile must contain at least one FROM instruction"}
	}

	return p.result, nil
}

// findHeredocDelimiters extracts heredoc delimiter(s) from a line.
// Returns empty slice if no heredocs found.
func findHeredocDelimiters(line string) []string {
	matches := heredocPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return nil
	}

	var delimiters []string
	for _, match := range matches {
		if len(match) >= 2 {
			delimiters = append(delimiters, match[1])
		}
	}
	return delimiters
}

func (p *parser) parseInstruction(line string, lineNum int) error {
	p.instructionCount++

	// Split into instruction and arguments
	// Handle parser directives (# directive=value) - skip for now

	// Find instruction keyword
	spaceIdx := strings.IndexFunc(line, unicode.IsSpace)
	var keyword, rest string
	if spaceIdx == -1 {
		keyword = line
		rest = ""
	} else {
		keyword = line[:spaceIdx]
		rest = strings.TrimSpace(line[spaceIdx+1:])
	}

	keyword = strings.ToUpper(keyword)

	switch keyword {
	case "FROM":
		return p.parseFrom(rest, lineNum, line)
	case "RUN":
		return p.parseRun(rest, lineNum, line)
	case "COPY":
		return p.parseCopy(rest, lineNum, line)
	case "ADD":
		return p.parseAdd(rest, lineNum, line)
	case "ENV":
		return p.parseEnv(rest, lineNum, line)
	case "WORKDIR":
		return p.parseWorkdir(rest, lineNum, line)
	case "ARG":
		return p.parseArg(rest, lineNum, line)
	case "LABEL":
		return p.parseLabel(rest, lineNum, line)
	case "USER":
		return p.parseUser(rest, lineNum, line)
	case "EXPOSE":
		return p.parseExpose(rest, lineNum, line)
	case "CMD":
		return p.parseCmd(rest, lineNum, line)
	case "ENTRYPOINT":
		return p.parseEntrypoint(rest, lineNum, line)
	case "SHELL":
		return p.parseShell(rest, lineNum, line)
	case "STOPSIGNAL":
		return p.parseStopSignal(rest, lineNum, line)
	case "VOLUME", "ONBUILD", "HEALTHCHECK":
		return &UnsupportedError{Feature: keyword, Line: lineNum}
	case "MAINTAINER":
		// Deprecated, ignore
		return nil
	default:
		return &ParseError{
			Line:    lineNum,
			Message: "unknown instruction: " + keyword,
		}
	}
}

func (p *parser) parseFrom(rest string, lineNum int, _ string) error {
	// Finalize previous stage if any
	if p.currentStage != nil {
		p.result.Stages = append(p.result.Stages, *p.currentStage)
	}

	// Parse: [--platform=...] image[:tag|@digest] [AS name]
	flags := make(map[string]string)
	rest = parseFlags(rest, flags)

	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return &ParseError{Line: lineNum, Message: "FROM requires an image argument"}
	}

	imageTemplate := parts[0]

	// Expand variables in image reference
	imageRef, err := ExpandVariables(imageTemplate, p.vars)
	if err != nil {
		return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
	}

	var alias string
	if len(parts) >= 3 && strings.ToUpper(parts[1]) == "AS" {
		alias = parts[2]
	}

	from := FromInstruction{
		Image:         imageRef,
		ImageTemplate: imageTemplate,
		Alias:         alias,
		Platform:      flags["platform"],
	}

	p.currentStage = &Stage{
		Name: alias,
		From: from,
	}

	return nil
}

func (p *parser) parseRun(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "RUN must come after FROM"}
	}

	// Parse exec form [...] or shell form
	args, isExec := parseExecOrShellForm(rest)

	// Note: We do NOT expand variables in RUN commands at parse time.
	// Shell variables like $PATH should be left for the shell to expand.
	// Only Dockerfile ARG references need expansion, which is done at build time.

	instr := Instruction{
		Kind:     InstructionRun,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}
	if isExec {
		instr.Flags = map[string]string{"form": "exec"}
	} else {
		instr.Flags = map[string]string{"form": "shell"}
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseCopy(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "COPY must come after FROM"}
	}

	flags := make(map[string]string)
	rest = parseFlags(rest, flags)

	// Check for unsupported --from flag
	if _, hasFrom := flags["from"]; hasFrom {
		return &UnsupportedError{Feature: "COPY --from (multi-stage builds)", Line: lineNum}
	}

	// Parse arguments (exec form or space-separated)
	args := parseSpaceSeparatedOrExec(rest)
	if len(args) < 2 {
		return &ParseError{Line: lineNum, Message: "COPY requires source and destination"}
	}

	// Expand variables in arguments
	for i, arg := range args {
		expanded, err := ExpandVariables(arg, p.vars)
		if err != nil {
			return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
		}
		args[i] = expanded
	}

	instr := Instruction{
		Kind:     InstructionCopy,
		Line:     lineNum,
		Original: original,
		Args:     args,
		Flags:    flags,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseAdd(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "ADD must come after FROM"}
	}

	flags := make(map[string]string)
	rest = parseFlags(rest, flags)

	// Parse arguments (exec form or space-separated)
	args := parseSpaceSeparatedOrExec(rest)
	if len(args) < 2 {
		return &ParseError{Line: lineNum, Message: "ADD requires source and destination"}
	}

	// Check for URL sources (unsupported)
	for i := 0; i < len(args)-1; i++ {
		if strings.HasPrefix(args[i], "http://") || strings.HasPrefix(args[i], "https://") {
			return &UnsupportedError{Feature: "ADD with URLs", Line: lineNum}
		}
	}

	// Expand variables in arguments
	for i, arg := range args {
		expanded, err := ExpandVariables(arg, p.vars)
		if err != nil {
			return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
		}
		args[i] = expanded
	}

	instr := Instruction{
		Kind:     InstructionAdd,
		Line:     lineNum,
		Original: original,
		Args:     args,
		Flags:    flags,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseEnv(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		// ENV before FROM adds to global vars
		kvs, err := parseKeyValues(rest)
		if err != nil {
			return &ParseError{Line: lineNum, Message: err.Error()}
		}
		if len(p.vars) > MaxVariableCount {
			return ErrTooManyVariables
		}
		for _, kv := range kvs {
			p.vars[kv.Key] = kv.Value
		}
		return nil
	}

	kvs, err := parseKeyValues(rest)
	if err != nil {
		return &ParseError{Line: lineNum, Message: err.Error()}
	}

	// Add to variables for subsequent expansion
	for _, kv := range kvs {
		if len(p.vars) > MaxVariableCount {
			return ErrTooManyVariables
		}
		p.vars[kv.Key] = kv.Value
	}

	// Build args as KEY=VALUE strings
	var args []string
	for _, kv := range kvs {
		args = append(args, kv.Key+"="+kv.Value)
	}

	instr := Instruction{
		Kind:     InstructionEnv,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseWorkdir(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "WORKDIR must come after FROM"}
	}

	rest = strings.TrimSpace(rest)
	if rest == "" {
		return &ParseError{Line: lineNum, Message: "WORKDIR requires a path"}
	}

	// Expand variables
	expanded, err := ExpandVariables(rest, p.vars)
	if err != nil {
		return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
	}

	instr := Instruction{
		Kind:     InstructionWorkDir,
		Line:     lineNum,
		Original: original,
		Args:     []string{expanded},
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseArg(rest string, lineNum int, original string) error {
	// ARG can appear before FROM (global) or after (stage-local)
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return &ParseError{Line: lineNum, Message: "ARG requires a name"}
	}

	var name, defaultVal string
	if eqIdx := strings.Index(rest, "="); eqIdx != -1 {
		name = rest[:eqIdx]
		defaultVal = rest[eqIdx+1:]
	} else {
		name = rest
	}

	// Security: check variable count
	if len(p.vars) > MaxVariableCount {
		return ErrTooManyVariables
	}

	// Store in vars for expansion (only if not already set)
	if _, exists := p.vars[name]; !exists {
		p.vars[name] = defaultVal
	}

	if p.currentStage == nil {
		// Global ARG
		p.result.Args = append(p.result.Args, KeyValue{Key: name, Value: defaultVal})
	} else {
		instr := Instruction{
			Kind:     InstructionArg,
			Line:     lineNum,
			Original: original,
			Args:     []string{name, defaultVal},
		}
		p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	}

	return nil
}

func (p *parser) parseLabel(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "LABEL must come after FROM"}
	}

	kvs, err := parseKeyValues(rest)
	if err != nil {
		return &ParseError{Line: lineNum, Message: err.Error()}
	}

	var args []string
	for _, kv := range kvs {
		args = append(args, kv.Key+"="+kv.Value)
	}

	instr := Instruction{
		Kind:     InstructionLabel,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseUser(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "USER must come after FROM"}
	}

	rest = strings.TrimSpace(rest)
	if rest == "" {
		return &ParseError{Line: lineNum, Message: "USER requires a username"}
	}

	// Expand variables
	expanded, err := ExpandVariables(rest, p.vars)
	if err != nil {
		return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
	}

	instr := Instruction{
		Kind:     InstructionUser,
		Line:     lineNum,
		Original: original,
		Args:     []string{expanded},
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseExpose(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "EXPOSE must come after FROM"}
	}

	// Split on whitespace
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return &ParseError{Line: lineNum, Message: "EXPOSE requires at least one port"}
	}

	// Expand variables in each port
	for i, port := range parts {
		expanded, err := ExpandVariables(port, p.vars)
		if err != nil {
			return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
		}
		parts[i] = expanded
	}

	instr := Instruction{
		Kind:     InstructionExpose,
		Line:     lineNum,
		Original: original,
		Args:     parts,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseCmd(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "CMD must come after FROM"}
	}

	args, isExec := parseExecOrShellForm(rest)

	instr := Instruction{
		Kind:     InstructionCmd,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}
	if isExec {
		instr.Flags = map[string]string{"form": "exec"}
	} else {
		instr.Flags = map[string]string{"form": "shell"}
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseEntrypoint(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "ENTRYPOINT must come after FROM"}
	}

	args, isExec := parseExecOrShellForm(rest)

	instr := Instruction{
		Kind:     InstructionEntrypoint,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}
	if isExec {
		instr.Flags = map[string]string{"form": "exec"}
	} else {
		instr.Flags = map[string]string{"form": "shell"}
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseShell(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "SHELL must come after FROM"}
	}

	// SHELL must be exec form
	args, isExec := parseExecOrShellForm(rest)
	if !isExec {
		return &ParseError{Line: lineNum, Message: "SHELL must use exec form ([\"executable\", \"arg\", ...])"}
	}

	instr := Instruction{
		Kind:     InstructionShell,
		Line:     lineNum,
		Original: original,
		Args:     args,
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

func (p *parser) parseStopSignal(rest string, lineNum int, original string) error {
	if p.currentStage == nil {
		return &ParseError{Line: lineNum, Message: "STOPSIGNAL must come after FROM"}
	}

	rest = strings.TrimSpace(rest)
	if rest == "" {
		return &ParseError{Line: lineNum, Message: "STOPSIGNAL requires a signal"}
	}

	// Expand variables
	expanded, err := ExpandVariables(rest, p.vars)
	if err != nil {
		return &ParseError{Line: lineNum, Message: "variable expansion failed: " + err.Error()}
	}

	instr := Instruction{
		Kind:     InstructionStopSignal,
		Line:     lineNum,
		Original: original,
		Args:     []string{expanded},
	}

	p.currentStage.Instructions = append(p.currentStage.Instructions, instr)
	return nil
}

// parseFlags extracts --key=value flags from the beginning of a string.
// Returns the remaining string after flags.
func parseFlags(s string, flags map[string]string) string {
	for {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "--") {
			break
		}

		// Find end of flag
		spaceIdx := strings.IndexFunc(s, unicode.IsSpace)
		var flag string
		if spaceIdx == -1 {
			flag = s
			s = ""
		} else {
			flag = s[:spaceIdx]
			s = s[spaceIdx+1:]
		}

		// Parse --key=value
		flag = strings.TrimPrefix(flag, "--")
		if eqIdx := strings.Index(flag, "="); eqIdx != -1 {
			flags[flag[:eqIdx]] = flag[eqIdx+1:]
		} else {
			flags[flag] = ""
		}
	}

	return s
}

// parseExecOrShellForm parses either exec form ["cmd", "arg"] or shell form "cmd arg".
// Returns the parsed arguments and whether it was exec form.
// For shell form, returns the entire string as a single argument (to be wrapped with shell).
func parseExecOrShellForm(s string) ([]string, bool) {
	s = strings.TrimSpace(s)

	// Check for exec form
	if strings.HasPrefix(s, "[") {
		var args []string
		if err := json.Unmarshal([]byte(s), &args); err == nil {
			return args, true
		}
		// If JSON parsing fails, fall through to shell form
	}

	// Shell form - return as single argument (to be wrapped with shell)
	if s != "" {
		return []string{s}, false
	}
	return nil, false
}

// parseSpaceSeparatedOrExec parses either exec form ["a", "b"] or space-separated "a b c".
// Used for COPY/ADD where arguments are not wrapped in a shell.
func parseSpaceSeparatedOrExec(s string) []string {
	s = strings.TrimSpace(s)

	// Check for exec form
	if strings.HasPrefix(s, "[") {
		var args []string
		if err := json.Unmarshal([]byte(s), &args); err == nil {
			return args
		}
		// If JSON parsing fails, fall through to space-separated
	}

	// Space-separated form
	return strings.Fields(s)
}

// parseKeyValues parses KEY=VALUE pairs (for ENV, LABEL).
// Supports both "KEY VALUE" (legacy) and "KEY=VALUE KEY2=VALUE2" forms.
func parseKeyValues(s string) ([]KeyValue, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	var result []KeyValue

	// Check for legacy single "KEY VALUE" form (no = sign before first space)
	firstSpace := strings.IndexFunc(s, unicode.IsSpace)
	firstEq := strings.Index(s, "=")

	if firstEq == -1 || (firstSpace != -1 && firstSpace < firstEq) {
		// Legacy form: KEY VALUE
		parts := strings.SplitN(s, " ", 2)
		if len(parts) == 2 {
			return []KeyValue{{Key: parts[0], Value: strings.TrimSpace(parts[1])}}, nil
		}
		return []KeyValue{{Key: s, Value: ""}}, nil
	}

	// Modern form: KEY=VALUE KEY2=VALUE2 or KEY="value with spaces"
	// Simple parsing - split on spaces but respect quotes
	for s != "" {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}

		// Find key
		eqIdx := strings.Index(s, "=")
		if eqIdx == -1 {
			// No more key=value pairs
			break
		}

		key := s[:eqIdx]
		s = s[eqIdx+1:]

		// Parse value (may be quoted)
		var value string
		if strings.HasPrefix(s, "\"") {
			// Quoted value
			endQuote := findClosingQuote(s[1:])
			if endQuote == -1 {
				value = s[1:]
				s = ""
			} else {
				value = s[1 : endQuote+1]
				s = s[endQuote+2:]
			}
		} else if strings.HasPrefix(s, "'") {
			// Single quoted value
			endQuote := strings.Index(s[1:], "'")
			if endQuote == -1 {
				value = s[1:]
				s = ""
			} else {
				value = s[1 : endQuote+1]
				s = s[endQuote+2:]
			}
		} else {
			// Unquoted - read until space
			spaceIdx := strings.IndexFunc(s, unicode.IsSpace)
			if spaceIdx == -1 {
				value = s
				s = ""
			} else {
				value = s[:spaceIdx]
				s = s[spaceIdx+1:]
			}
		}

		result = append(result, KeyValue{Key: key, Value: value})
	}

	return result, nil
}

// findClosingQuote finds the index of the closing " in a string, handling escapes.
func findClosingQuote(s string) int {
	escaped := false
	for i := 0; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' {
			escaped = true
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}
