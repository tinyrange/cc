package virgl

import (
	"bufio"
	"fmt"
	"math/bits"
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
	maxAddress     int
	maxSampler     int
	maxSamplerView int
	samplerViews   map[int]string
	immediates     []string
	instructions   []string
}

var (
	tgsiDeclarationPattern = regexp.MustCompile(`^DCL (IN|OUT|CONST|TEMP|ADDR|SAMP)\[(\d+)(?:\.\.(\d+))?\](?:\.[xyzw]+)?(?:,\s*([^,]+))?`)
	tgsiImmediatePattern   = regexp.MustCompile(`^IMM\[(\d+)\]\s+(\w+)\s+\{([^}]+)\}$`)
	tgsiSamplerViewPattern = regexp.MustCompile(`^DCL SVIEW\[(\d+)(?:\.\.(\d+))?\],\s*([A-Z0-9_]+),\s*([A-Z0-9_]+)$`)
	tgsiInstructionPattern = regexp.MustCompile(`^\s*\d+:\s+([A-Z0-9_]+)(?:\s+(.+))?$`)
	tgsiRegisterPattern    = regexp.MustCompile(`^(IN|OUT|CONST|TEMP|IMM)\[(\d+)\](?:\.([xyzw]+))?$`)
	tgsiIndirectPattern    = regexp.MustCompile(`^(CONST|TEMP)\[ADDR\[(\d+)\]\.([xyzw])([+-]\d+)?\](?:\(\d+\))?(?:\.([xyzw]+))?$`)
	tgsiAddressPattern     = regexp.MustCompile(`^ADDR\[(\d+)\](?:\.([xyzw]+))?$`)
	tgsiControlLabel       = regexp.MustCompile(`(?:^|\s+):\d+$`)
	tgsiSamplerPattern     = regexp.MustCompile(`^SAMP\[(\d+)\]$`)
)

func translateTGSI(source string) (uint32, string, error) {
	shader := tgsiShader{
		inputs:         make(map[int]tgsiDeclaration),
		outputs:        make(map[int]tgsiDeclaration),
		samplerViews:   make(map[int]string),
		maxConstant:    -1,
		maxTemporary:   -1,
		maxAddress:     -1,
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
			case "ADDR":
				shader.maxAddress = max(shader.maxAddress, last)
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
			if (match[3] != "2D" && match[3] != "SHADOW2D") || match[4] != "FLOAT" {
				return 0, "", fmt.Errorf("TGSI line %d sampler view %s/%s is unsupported", lineNumber, match[3], match[4])
			}
			for index := first; index <= last; index++ {
				shader.samplerViews[index] = match[3]
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
				components := make([]string, len(values))
				for index, value := range values {
					if strings.HasPrefix(strings.ToLower(value), "0x") {
						components[index] = "uintBitsToFloat(uint(" + value + "))"
					} else {
						components[index] = value
					}
				}
				shader.immediates = append(shader.immediates, "vec4("+strings.Join(components, ", ")+")")
			default:
				return 0, "", fmt.Errorf("TGSI line %d immediate type %s is unsupported", lineNumber, match[2])
			}
			continue
		}
		if match := tgsiInstructionPattern.FindStringSubmatch(line); match != nil {
			if match[1] == "END" {
				continue
			}
			operands := match[2]
			switch match[1] {
			case "BGNLOOP", "ENDLOOP", "ELSE":
				operands = tgsiControlLabel.ReplaceAllString(operands, "")
			case "IF", "UIF":
				operands = tgsiControlLabel.ReplaceAllString(operands, "")
			}
			statement, err := shader.translateInstruction(match[1], splitTGSIList(operands))
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
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw := strings.Split(value, ",")
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		result = append(result, strings.TrimSpace(item))
	}
	return result
}

func (s *tgsiShader) translateInstruction(opcode string, operands []string) (string, error) {
	saturate := strings.HasSuffix(opcode, "_SAT")
	if saturate {
		opcode = strings.TrimSuffix(opcode, "_SAT")
	}
	switch opcode {
	case "BGNLOOP":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode BGNLOOP has %d operands, want 0", len(operands))
		}
		return "while (true) {", nil
	case "ENDLOOP":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode ENDLOOP has %d operands, want 0", len(operands))
		}
		return "}", nil
	case "BRK":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode BRK has %d operands, want 0", len(operands))
		}
		return "break;", nil
	case "CONT":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode CONT has %d operands, want 0", len(operands))
		}
		return "continue;", nil
	case "ELSE":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode ELSE has %d operands, want 0", len(operands))
		}
		return "} else {", nil
	case "ENDIF":
		if len(operands) != 0 {
			return "", fmt.Errorf("opcode ENDIF has %d operands, want 0", len(operands))
		}
		return "}", nil
	case "IF", "UIF":
		if len(operands) != 1 {
			return "", fmt.Errorf("opcode %s has %d operands, want 1", opcode, len(operands))
		}
		condition, _, err := s.register(operands[0], false)
		if err != nil {
			return "", err
		}
		if opcode == "UIF" {
			return "if (floatBitsToUint((" + condition + ").x) != 0u) {", nil
		}
		return "if ((" + condition + ").x != 0.0) {", nil
	case "ARL", "UARL":
		if len(operands) != 2 {
			return "", fmt.Errorf("opcode %s has %d operands, want 2", opcode, len(operands))
		}
		destination, err := s.addressRegister(operands[0])
		if err != nil {
			return "", err
		}
		source, _, err := s.register(operands[1], false)
		if err != nil {
			return "", err
		}
		expression := "ivec4(floor(" + source + "))"
		if opcode == "UARL" {
			expression = "floatBitsToInt(" + source + ")"
		}
		address := tgsiAddressPattern.FindStringSubmatch(operands[0])
		if mask := address[2]; mask != "" {
			expression = "(" + expression + ")." + mask
		}
		return destination + " = " + expression + ";", nil
	case "KILL_IF":
		if len(operands) != 1 {
			return "", fmt.Errorf("opcode KILL_IF has %d operands, want 1", len(operands))
		}
		source, _, err := s.register(operands[0], false)
		if err != nil {
			return "", err
		}
		return "if (any(lessThan(" + source + ", vec4(0.0)))) discard;", nil
	}
	if opcode == "TEX" {
		if len(operands) != 4 || (operands[3] != "2D" && operands[3] != "SHADOW2D") {
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
		samplerIndex, _ := strconv.Atoi(sampler[1])
		var expression string
		if operands[3] == "SHADOW2D" || s.samplerViews[samplerIndex] == "SHADOW2D" {
			expression = fmt.Sprintf("vec4(texture(sampler%s, (%s).xyz))", sampler[1], coordinate)
		} else {
			expression = fmt.Sprintf("texture(sampler%s, (%s).xy)", sampler[1], coordinate)
		}
		if saturate {
			expression = "clamp(" + expression + ", 0.0, 1.0)"
		}
		if mask != "" {
			expression = "(" + expression + ")." + mask
		}
		return destination + " = " + expression + ";", nil
	}
	arities := map[string]int{
		"MOV": 2, "RSQ": 2, "RCP": 2, "FLR": 2, "FRC": 2, "EX2": 2, "LG2": 2, "SIN": 2, "COS": 2, "DDX": 2, "DDY": 2, "SSG": 2, "NOT": 2,
		"F2I": 2, "F2U": 2, "I2F": 2, "U2F": 2,
		"ADD": 3, "MUL": 3, "DIV": 3, "DP2": 3, "DP3": 3, "DP4": 3, "MAX": 3, "MIN": 3,
		"POW": 3, "FSLT": 3, "FSGE": 3, "SGE": 3, "FSEQ": 3, "FSNE": 3, "ISGE": 3, "USEQ": 3, "USNE": 3,
		"AND": 3, "OR": 3, "XOR": 3, "UADD": 3, "UMUL": 3, "SHL": 3, "USHR": 3, "ISHR": 3,
		"MAD": 4, "LRP": 4, "UCMP": 4,
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
	case "RCP":
		expression = "vec4(1.0 / (" + sources[0] + ").x)"
	case "FLR":
		expression = "floor(" + sources[0] + ")"
	case "FRC":
		expression = "fract(" + sources[0] + ")"
	case "EX2":
		expression = "vec4(exp2((" + sources[0] + ").x))"
	case "LG2":
		expression = "vec4(log2((" + sources[0] + ").x))"
	case "SIN":
		expression = "vec4(sin((" + sources[0] + ").x))"
	case "COS":
		expression = "vec4(cos((" + sources[0] + ").x))"
	case "DDX":
		expression = "dFdx(" + sources[0] + ")"
	case "DDY":
		expression = "dFdy(" + sources[0] + ")"
	case "SSG":
		expression = "sign(" + sources[0] + ")"
	case "NOT":
		expression = "uintBitsToFloat(~floatBitsToUint(" + sources[0] + "))"
	case "F2I":
		expression = "intBitsToFloat(ivec4(" + sources[0] + "))"
	case "F2U":
		expression = "uintBitsToFloat(uvec4(" + sources[0] + "))"
	case "I2F":
		expression = "vec4(floatBitsToInt(" + sources[0] + "))"
	case "U2F":
		expression = "vec4(floatBitsToUint(" + sources[0] + "))"
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
	case "DP2":
		expression = "vec4(dot((" + sources[0] + ").xy, (" + sources[1] + ").xy))"
	case "DP4":
		expression = "vec4(dot(" + sources[0] + ", " + sources[1] + "))"
	case "POW":
		expression = "vec4(pow((" + sources[0] + ").x, (" + sources[1] + ").x))"
	case "FSLT":
		expression = "intBitsToFloat(-ivec4(lessThan(" + sources[0] + ", " + sources[1] + ")))"
	case "FSGE":
		expression = "intBitsToFloat(-ivec4(greaterThanEqual(" + sources[0] + ", " + sources[1] + ")))"
	case "SGE":
		expression = "vec4(greaterThanEqual(" + sources[0] + ", " + sources[1] + "))"
	case "FSEQ":
		expression = "intBitsToFloat(-ivec4(equal(" + sources[0] + ", " + sources[1] + ")))"
	case "FSNE":
		expression = "intBitsToFloat(-ivec4(notEqual(" + sources[0] + ", " + sources[1] + ")))"
	case "ISGE":
		expression = "intBitsToFloat(-ivec4(greaterThanEqual(" +
			"floatBitsToInt(" + sources[0] + "), floatBitsToInt(" + sources[1] + "))))"
	case "USEQ":
		expression = "intBitsToFloat(-ivec4(equal(" +
			"floatBitsToUint(" + sources[0] + "), floatBitsToUint(" + sources[1] + "))))"
	case "USNE":
		expression = "intBitsToFloat(-ivec4(notEqual(" +
			"floatBitsToUint(" + sources[0] + "), floatBitsToUint(" + sources[1] + "))))"
	case "AND":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") & floatBitsToUint(" + sources[1] + "))"
	case "OR":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") | floatBitsToUint(" + sources[1] + "))"
	case "XOR":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") ^ floatBitsToUint(" + sources[1] + "))"
	case "UADD":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") + floatBitsToUint(" + sources[1] + "))"
	case "UMUL":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") * floatBitsToUint(" + sources[1] + "))"
	case "SHL":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") << floatBitsToUint(" + sources[1] + "))"
	case "USHR":
		expression = "uintBitsToFloat(floatBitsToUint(" + sources[0] + ") >> floatBitsToUint(" + sources[1] + "))"
	case "ISHR":
		expression = "intBitsToFloat(floatBitsToInt(" + sources[0] + ") >> floatBitsToInt(" + sources[1] + "))"
	case "MAD":
		expression = "((" + sources[0] + " * " + sources[1] + ") + " + sources[2] + ")"
	case "LRP":
		expression = "mix(" + sources[2] + ", " + sources[1] + ", " + sources[0] + ")"
	case "UCMP":
		expression = "mix(" + sources[2] + ", " + sources[1] +
			", notEqual(floatBitsToUint(" + sources[0] + "), uvec4(0)))"
	}
	if saturate {
		expression = "clamp(" + expression + ", 0.0, 1.0)"
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
	if match := tgsiIndirectPattern.FindStringSubmatch(raw); match != nil {
		if destination {
			return "", "", fmt.Errorf("indirect destination %q is unsupported", raw)
		}
		addressIndex, _ := strconv.Atoi(match[2])
		if addressIndex > s.maxAddress {
			return "", "", fmt.Errorf("address register %d is not declared", addressIndex)
		}
		offset := match[4]
		index := fmt.Sprintf("address[%d].%s", addressIndex, match[3])
		if offset != "" {
			index += offset
		}
		name := ""
		if match[1] == "CONST" {
			name = fmt.Sprintf("%s[%s]", s.constantName(), index)
		} else {
			name = fmt.Sprintf("temporary[%s]", index)
		}
		if swizzle := match[5]; swizzle != "" {
			if len(swizzle) < 4 {
				swizzle += strings.Repeat(swizzle[len(swizzle)-1:], 4-len(swizzle))
			}
			name += "." + swizzle
		}
		if negative {
			name = "(-" + name + ")"
		}
		return name, "", nil
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
		name = fmt.Sprintf("%s[%d]", s.constantName(), index)
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

func (s *tgsiShader) constantName() string {
	if s.stage == tgsiFragment {
		return "uFragmentConstants"
	}
	return "uVertexConstants"
}

func (s *tgsiShader) addressRegister(raw string) (string, error) {
	match := tgsiAddressPattern.FindStringSubmatch(raw)
	if match == nil {
		return "", fmt.Errorf("invalid address register %q", raw)
	}
	index, _ := strconv.Atoi(match[1])
	if index > s.maxAddress {
		return "", fmt.Errorf("address register %d is not declared", index)
	}
	name := fmt.Sprintf("address[%d]", index)
	if mask := match[2]; mask != "" {
		name += "." + mask
	}
	return name, nil
}

func (s *tgsiShader) inputName(index int) string {
	declaration := s.inputs[index]
	if s.stage == tgsiFragment {
		if declaration.semantic == "POSITION" {
			return "gl_FragCoord"
		}
		if declaration.semantic == "FACE" {
			return "vec4(gl_FrontFacing ? 1.0 : -1.0)"
		}
		if declaration.semantic == "PCOORD" {
			return "vec4(gl_PointCoord, 0.0, 1.0)"
		}
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

func declarationRank(declarations map[int]tgsiDeclaration, index int, _ bool) int {
	rank := 0
	for candidate := 0; candidate < index; candidate++ {
		declaration, ok := declarations[candidate]
		if !ok || declaration.semantic == "FACE" || declaration.semantic == "POSITION" {
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

type glslVarying struct {
	name      string
	qualifier string
}

func pointSpriteFragmentSource(fragment string, coordinates uint32) string {
	for coordinates != 0 {
		index := bits.TrailingZeros32(coordinates)
		name := fmt.Sprintf("varying_generic_%d", index)
		declaration := regexp.MustCompile(`(?m)^\s*(?:(?:flat|smooth|noperspective)\s+)?in\s+vec4\s+` +
			regexp.QuoteMeta(name) + `\s*;\s*\n?`)
		if declaration.MatchString(fragment) {
			fragment = declaration.ReplaceAllString(fragment, "")
			fragment = regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b`).
				ReplaceAllString(fragment, "vec4(gl_PointCoord, 0.0, 1.0)")
		}
		coordinates &^= 1 << index
	}
	return fragment
}

func linkTGSIInterfaces(vertex, fragment string) string {
	vertexOutputs := glslVaryings(vertex, "out")
	fragmentInputs := glslVaryings(fragment, "in")
	matched := make(map[string]bool, len(fragmentInputs))

	for _, output := range vertexOutputs {
		for _, input := range fragmentInputs {
			if output.name == input.name {
				matched[input.name] = true
				break
			}
		}
	}
	fallback := regexp.MustCompile(`^varying\d+$`)
	for _, output := range vertexOutputs {
		if matched[output.name] || !fallback.MatchString(output.name) {
			continue
		}
		for _, input := range fragmentInputs {
			if matched[input.name] {
				continue
			}
			vertex = regexp.MustCompile(`\b`+regexp.QuoteMeta(output.name)+`\b`).
				ReplaceAllString(vertex, input.name)
			matched[input.name] = true
			break
		}
	}

	var declarations strings.Builder
	var initializers strings.Builder
	for _, input := range fragmentInputs {
		if matched[input.name] {
			continue
		}
		if input.qualifier != "" {
			declarations.WriteString(input.qualifier)
			declarations.WriteByte(' ')
		}
		fmt.Fprintf(&declarations, "out vec4 %s;\n", input.name)
		fmt.Fprintf(&initializers, "    %s = vec4(0.0);\n", input.name)
	}
	if declarations.Len() == 0 {
		return vertex
	}
	vertex = strings.Replace(vertex, "#version 410 core\n", "#version 410 core\n"+declarations.String(), 1)
	return strings.Replace(vertex, "void main() {\n", "void main() {\n"+initializers.String(), 1)
}

func glslVaryings(source, direction string) []glslVarying {
	var varyings []glslVarying
	for _, line := range strings.Split(source, "\n") {
		fields := strings.Fields(line)
		directionIndex := -1
		for index, field := range fields {
			if field == direction {
				directionIndex = index
				break
			}
		}
		if directionIndex < 0 || directionIndex+2 >= len(fields) || fields[directionIndex+1] != "vec4" {
			continue
		}
		name := strings.TrimSuffix(fields[directionIndex+2], ";")
		if !strings.HasPrefix(name, "varying") {
			continue
		}
		varyings = append(varyings, glslVarying{
			name:      name,
			qualifier: strings.Join(fields[:directionIndex], " "),
		})
	}
	return varyings
}

func (s *tgsiShader) glsl() (string, error) {
	var source strings.Builder
	source.WriteString("#version 410 core\n")
	if s.stage == tgsiVertex {
		// VirGL applications commonly render coplanar geometry in multiple
		// passes with different vertex programs.  GLSL only guarantees identical
		// window-space positions across those programs when gl_Position is
		// invariant; without it, distant surfaces can fail an EQUAL/LEQUAL depth
		// comparison as alternating triangles.
		source.WriteString("invariant gl_Position;\n")
		source.WriteString("uniform float uWinsysAdjustY;\n")
	}
	for index := 0; index <= maxDeclarationIndex(s.inputs); index++ {
		declaration, ok := s.inputs[index]
		if !ok {
			continue
		}
		if s.stage == tgsiFragment && (declaration.semantic == "POSITION" ||
			declaration.semantic == "FACE" || declaration.semantic == "PCOORD") {
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
		fmt.Fprintf(&source, "uniform vec4 %s[%d];\n", s.constantName(), s.maxConstant+1)
	}
	for index := 0; index <= max(s.maxSampler, s.maxSamplerView); index++ {
		samplerType := "sampler2D"
		if s.samplerViews[index] == "SHADOW2D" {
			samplerType = "sampler2DShadow"
		}
		fmt.Fprintf(&source, "uniform %s sampler%d;\n", samplerType, index)
	}
	for index, immediate := range s.immediates {
		fmt.Fprintf(&source, "const vec4 immediate%d = %s;\n", index, immediate)
	}
	source.WriteString("void main() {\n")
	if s.maxTemporary >= 0 {
		fmt.Fprintf(&source, "    vec4 temporary[%d];\n", s.maxTemporary+1)
	}
	if s.maxAddress >= 0 {
		fmt.Fprintf(&source, "    ivec4 address[%d];\n", s.maxAddress+1)
	}
	for _, instruction := range s.instructions {
		fmt.Fprintf(&source, "    %s\n", instruction)
	}
	if s.stage == tgsiVertex {
		for index := 0; index <= maxDeclarationIndex(s.outputs); index++ {
			if declaration, ok := s.outputs[index]; ok && declaration.semantic == "PSIZE" {
				fmt.Fprintf(&source, "    gl_PointSize = %s.x;\n", s.outputName(index))
				break
			}
		}
		source.WriteString("    gl_Position.y *= uWinsysAdjustY;\n")
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
