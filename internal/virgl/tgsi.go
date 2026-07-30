package virgl

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	tgsiVertex   = 0
	tgsiFragment = 1
)

type tgsiDeclaration struct {
	index    int
	semantic string
}

type tgsiShader struct {
	stage          uint32
	inputs         map[int]tgsiDeclaration
	outputs        map[int]tgsiDeclaration
	maxConstant    int
	maxTemporary   int
	maxSampler     int
	maxSamplerView int
	immediates     []string
	instructions   []string
}

var (
	tgsiDeclarationPattern = regexp.MustCompile(`^DCL (IN|OUT|CONST|TEMP|SAMP)\[(\d+)(?:\.\.(\d+))?\](?:,\s*([^,]+))?`)
	tgsiImmediatePattern   = regexp.MustCompile(`^IMM\[(\d+)\]\s+(\w+)\s+\{([^}]+)\}$`)
	tgsiSamplerViewPattern = regexp.MustCompile(`^DCL SVIEW\[(\d+)(?:\.\.(\d+))?\],\s*([A-Z0-9_]+),\s*([A-Z0-9_]+)$`)
	tgsiInstructionPattern = regexp.MustCompile(`^\s*\d+:\s+([A-Z0-9_]+)(?:\s+(.+))?$`)
	tgsiRegisterPattern    = regexp.MustCompile(`^(IN|OUT|CONST|TEMP|IMM)\[(\d+)\](?:\.([xyzw]+))?$`)
	tgsiSamplerPattern     = regexp.MustCompile(`^SAMP\[(\d+)\]$`)
)

func translateTGSI(source string) (uint32, string, error) {
	shader := tgsiShader{
		inputs:         make(map[int]tgsiDeclaration),
		outputs:        make(map[int]tgsiDeclaration),
		maxConstant:    -1,
		maxTemporary:   -1,
		maxSampler:     -1,
		maxSamplerView: -1,
	}
	scanner := bufio.NewScanner(strings.NewReader(source))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "PROPERTY ") {
			continue
		}
		switch line {
		case "VERT":
			shader.stage = tgsiVertex
			continue
		case "FRAG":
			shader.stage = tgsiFragment
			continue
		}
		if match := tgsiDeclarationPattern.FindStringSubmatch(line); match != nil {
			first, _ := strconv.Atoi(match[2])
			last := first
			if match[3] != "" {
				last, _ = strconv.Atoi(match[3])
			}
			semantic := strings.TrimSpace(match[4])
			switch match[1] {
			case "IN":
				shader.inputs[first] = tgsiDeclaration{index: first, semantic: semantic}
			case "OUT":
				shader.outputs[first] = tgsiDeclaration{index: first, semantic: semantic}
			case "CONST":
				shader.maxConstant = max(shader.maxConstant, last)
			case "TEMP":
				shader.maxTemporary = max(shader.maxTemporary, last)
			case "SAMP":
				shader.maxSampler = max(shader.maxSampler, last)
			}
			continue
		}
		if match := tgsiSamplerViewPattern.FindStringSubmatch(line); match != nil {
			first, _ := strconv.Atoi(match[1])
			last := first
			if match[2] != "" {
				last, _ = strconv.Atoi(match[2])
			}
			if match[3] != "2D" || match[4] != "FLOAT" {
				return 0, "", fmt.Errorf("TGSI line %d sampler view %s/%s is unsupported", lineNumber, match[3], match[4])
			}
			shader.maxSamplerView = max(shader.maxSamplerView, last)
			continue
		}
		if match := tgsiImmediatePattern.FindStringSubmatch(line); match != nil {
			index, _ := strconv.Atoi(match[1])
			if index != len(shader.immediates) {
				return 0, "", fmt.Errorf("TGSI line %d immediate index %d is not contiguous", lineNumber, index)
			}
			values := splitTGSIList(match[3])
			if len(values) != 4 {
				return 0, "", fmt.Errorf("TGSI line %d immediate has %d components", lineNumber, len(values))
			}
			switch match[2] {
			case "UINT32":
				shader.immediates = append(shader.immediates,
					"uintBitsToFloat(uvec4("+strings.Join(values, ", ")+"))")
			case "FLT32":
				shader.immediates = append(shader.immediates, "vec4("+strings.Join(values, ", ")+")")
			default:
				return 0, "", fmt.Errorf("TGSI line %d immediate type %s is unsupported", lineNumber, match[2])
			}
			continue
		}
		if match := tgsiInstructionPattern.FindStringSubmatch(line); match != nil {
			if match[1] == "END" {
				continue
			}
			statement, err := shader.translateInstruction(match[1], splitTGSIList(match[2]))
			if err != nil {
				return 0, "", fmt.Errorf("TGSI line %d: %w", lineNumber, err)
			}
			shader.instructions = append(shader.instructions, statement)
			continue
		}
		return 0, "", fmt.Errorf("TGSI line %d is unsupported: %q", lineNumber, line)
	}
	if err := scanner.Err(); err != nil {
		return 0, "", err
	}
	if len(shader.instructions) == 0 {
		return 0, "", fmt.Errorf("TGSI shader has no instructions")
	}
	glsl, err := shader.glsl()
	return shader.stage, glsl, err
}

func splitTGSIList(value string) []string {
	raw := strings.Split(value, ",")
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		result = append(result, strings.TrimSpace(item))
	}
	return result
}

func (s *tgsiShader) translateInstruction(opcode string, operands []string) (string, error) {
	if opcode == "TEX" {
		if len(operands) != 4 || operands[3] != "2D" {
			return "", fmt.Errorf("opcode TEX operands %q are unsupported", operands)
		}
		destination, mask, err := s.register(operands[0], true)
		if err != nil {
			return "", err
		}
		coordinate, _, err := s.register(operands[1], false)
		if err != nil {
			return "", err
		}
		sampler := tgsiSamplerPattern.FindStringSubmatch(operands[2])
		if sampler == nil {
			return "", fmt.Errorf("invalid texture sampler %q", operands[2])
		}
		expression := fmt.Sprintf("texture(sampler%s, (%s).xy)", sampler[1], coordinate)
		if mask != "" {
			expression = "(" + expression + ")." + mask
		}
		return destination + " = " + expression + ";", nil
	}
	arities := map[string]int{
		"MOV": 2, "RSQ": 2,
		"ADD": 3, "MUL": 3, "DIV": 3, "DP3": 3, "DP4": 3, "MAX": 3, "MIN": 3,
		"MAD": 4,
	}
	arity, ok := arities[opcode]
	if !ok {
		return "", fmt.Errorf("opcode %s is unsupported", opcode)
	}
	if len(operands) != arity {
		return "", fmt.Errorf("opcode %s has %d operands, want %d", opcode, len(operands), arity)
	}
	destination, mask, err := s.register(operands[0], true)
	if err != nil {
		return "", err
	}
	sources := make([]string, 0, len(operands)-1)
	for _, operand := range operands[1:] {
		source, _, err := s.register(operand, false)
		if err != nil {
			return "", err
		}
		sources = append(sources, source)
	}
	var expression string
	switch opcode {
	case "MOV":
		expression = sources[0]
	case "RSQ":
		expression = "vec4(inversesqrt((" + sources[0] + ").x))"
	case "ADD":
		expression = "(" + sources[0] + " + " + sources[1] + ")"
	case "MUL":
		expression = "(" + sources[0] + " * " + sources[1] + ")"
	case "DIV":
		expression = "(" + sources[0] + " / " + sources[1] + ")"
	case "MAX":
		expression = "max(" + sources[0] + ", " + sources[1] + ")"
	case "MIN":
		expression = "min(" + sources[0] + ", " + sources[1] + ")"
	case "DP3":
		expression = "vec4(dot((" + sources[0] + ").xyz, (" + sources[1] + ").xyz))"
	case "DP4":
		expression = "vec4(dot(" + sources[0] + ", " + sources[1] + "))"
	case "MAD":
		expression = "((" + sources[0] + " * " + sources[1] + ") + " + sources[2] + ")"
	}
	if mask != "" {
		expression = "(" + expression + ")." + mask
	}
	return destination + " = " + expression + ";", nil
}

func (s *tgsiShader) register(raw string, destination bool) (string, string, error) {
	negative := strings.HasPrefix(raw, "-")
	if negative {
		if destination {
			return "", "", fmt.Errorf("destination %q is negative", raw)
		}
		raw = strings.TrimPrefix(raw, "-")
	}
	match := tgsiRegisterPattern.FindStringSubmatch(raw)
	if match == nil {
		return "", "", fmt.Errorf("invalid register %q", raw)
	}
	index, _ := strconv.Atoi(match[2])
	mask := match[3]
	var name string
	switch match[1] {
	case "IN":
		name = s.inputName(index)
	case "OUT":
		name = s.outputName(index)
	case "CONST":
		name = fmt.Sprintf("uConstants[%d]", index)
	case "TEMP":
		name = fmt.Sprintf("temporary[%d]", index)
	case "IMM":
		name = fmt.Sprintf("immediate%d", index)
	}
	if !destination && mask != "" {
		if len(mask) < 4 {
			mask += strings.Repeat(mask[len(mask)-1:], 4-len(mask))
		}
		name += "." + mask
		mask = ""
	} else if destination && mask != "" {
		name += "." + mask
	}
	if negative {
		name = "(-" + name + ")"
	}
	return name, mask, nil
}

func (s *tgsiShader) inputName(index int) string {
	declaration := s.inputs[index]
	if s.stage == tgsiFragment {
		if declaration.semantic == "" || isInterpolationQualifier(declaration.semantic) {
			return fmt.Sprintf("varying%d", declarationRank(s.inputs, index, false))
		}
		return semanticName(declaration.semantic, index)
	}
	return fmt.Sprintf("attribute%d", index)
}

func (s *tgsiShader) outputName(index int) string {
	declaration := s.outputs[index]
	if s.stage == tgsiVertex && declaration.semantic == "POSITION" {
		return "gl_Position"
	}
	if s.stage == tgsiFragment && strings.HasPrefix(declaration.semantic, "COLOR") {
		return fmt.Sprintf("fragmentColor%d", index)
	}
	if s.stage == tgsiVertex && (declaration.semantic == "" || isInterpolationQualifier(declaration.semantic)) {
		return fmt.Sprintf("varying%d", declarationRank(s.outputs, index, true))
	}
	return semanticName(declaration.semantic, index)
}

func declarationRank(declarations map[int]tgsiDeclaration, index int, skipPosition bool) int {
	rank := 0
	for candidate := 0; candidate < index; candidate++ {
		declaration, ok := declarations[candidate]
		if !ok || (skipPosition && declaration.semantic == "POSITION") {
			continue
		}
		rank++
	}
	return rank
}

func isInterpolationQualifier(value string) bool {
	switch value {
	case "CONSTANT", "LINEAR", "PERSPECTIVE", "COLOR":
		return true
	default:
		return false
	}
}

func semanticName(semantic string, fallback int) string {
	semantic = strings.TrimSpace(semantic)
	if semantic == "" {
		return fmt.Sprintf("varying%d", fallback)
	}
	replacer := strings.NewReplacer("[", "_", "]", "", ".", "_")
	return "varying_" + strings.ToLower(replacer.Replace(semantic))
}

func (s *tgsiShader) glsl() (string, error) {
	var source strings.Builder
	source.WriteString("#version 410 core\n")
	for index := 0; index <= maxDeclarationIndex(s.inputs); index++ {
		declaration, ok := s.inputs[index]
		if !ok {
			continue
		}
		if s.stage == tgsiVertex {
			fmt.Fprintf(&source, "layout(location = %d) in vec4 %s;\n", index, s.inputName(index))
		} else {
			qualifier := ""
			if strings.Contains(declaration.semantic, "FLAT") {
				qualifier = "flat "
			}
			fmt.Fprintf(&source, "%sin vec4 %s;\n", qualifier, s.inputName(index))
		}
	}
	for index := 0; index <= maxDeclarationIndex(s.outputs); index++ {
		declaration, ok := s.outputs[index]
		if !ok || (s.stage == tgsiVertex && declaration.semantic == "POSITION") {
			continue
		}
		if s.stage == tgsiFragment && strings.HasPrefix(declaration.semantic, "COLOR") {
			fmt.Fprintf(&source, "layout(location = %d) out vec4 %s;\n", index, s.outputName(index))
		} else {
			fmt.Fprintf(&source, "out vec4 %s;\n", s.outputName(index))
		}
	}
	if s.maxConstant >= 0 {
		fmt.Fprintf(&source, "uniform vec4 uConstants[%d];\n", s.maxConstant+1)
	}
	for index := 0; index <= max(s.maxSampler, s.maxSamplerView); index++ {
		fmt.Fprintf(&source, "uniform sampler2D sampler%d;\n", index)
	}
	for index, immediate := range s.immediates {
		fmt.Fprintf(&source, "const vec4 immediate%d = %s;\n", index, immediate)
	}
	source.WriteString("void main() {\n")
	if s.maxTemporary >= 0 {
		fmt.Fprintf(&source, "    vec4 temporary[%d];\n", s.maxTemporary+1)
	}
	for _, instruction := range s.instructions {
		fmt.Fprintf(&source, "    %s\n", instruction)
	}
	source.WriteString("}\n")
	return source.String(), nil
}

func maxDeclarationIndex(declarations map[int]tgsiDeclaration) int {
	result := -1
	for index := range declarations {
		result = max(result, index)
	}
	return result
}
