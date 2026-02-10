package dockerfile

import (
	"strings"
)

// expandVariablesImpl expands variable references in a string.
// Supports: $VAR, ${VAR}, ${VAR:-default}, ${VAR:+alternate}
// Use $$ for literal $.
// If preserveUndefined is true, undefined variables are left as-is (for RUN commands).
// If preserveUndefined is false, undefined variables expand to empty string (for ENV, COPY, etc).
func expandVariablesImpl(s string, vars map[string]string, depth int, preserveUndefined bool) (string, error) {
	if depth > MaxVariableExpansion {
		return "", ErrVariableExpansionLoop
	}

	var result strings.Builder
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] != '$' {
			result.WriteByte(s[i])
			i++
			continue
		}

		// Found $
		if i+1 >= len(s) {
			// $ at end of string
			result.WriteByte('$')
			i++
			continue
		}

		next := s[i+1]

		// $$ -> literal $
		if next == '$' {
			result.WriteByte('$')
			i += 2
			continue
		}

		// ${...} form
		if next == '{' {
			end := strings.Index(s[i:], "}")
			if end == -1 {
				// Unclosed brace, treat as literal
				result.WriteByte('$')
				i++
				continue
			}
			end += i // Convert to absolute index

			expr := s[i+2 : end] // Content between ${ and }
			expanded, err := expandBraceExprImpl(expr, vars, depth, preserveUndefined)
			if err != nil {
				return "", err
			}
			result.WriteString(expanded)
			i = end + 1
			continue
		}

		// $VAR form - variable name is alphanumeric + underscore
		j := i + 1
		for j < len(s) && isVarChar(s[j]) {
			j++
		}

		if j == i+1 {
			// No valid variable name after $
			result.WriteByte('$')
			i++
			continue
		}

		varName := s[i+1 : j]
		if val, ok := vars[varName]; ok {
			// Recursively expand the value
			expanded, err := expandVariablesImpl(val, vars, depth+1, preserveUndefined)
			if err != nil {
				return "", err
			}
			result.WriteString(expanded)
		} else if preserveUndefined {
			// Variable not found - leave as-is for shell to handle
			result.WriteByte('$')
			result.WriteString(varName)
		}
		// If not preserveUndefined, expand to empty string (Docker behavior for ENV, etc)
		i = j
	}

	return result.String(), nil
}

// expandBraceExprImpl expands a ${...} expression.
func expandBraceExprImpl(expr string, vars map[string]string, depth int, preserveUndefined bool) (string, error) {
	// Check for modifier patterns
	colonIdx := strings.Index(expr, ":")
	if colonIdx == -1 {
		// Simple ${VAR}
		if val, ok := vars[expr]; ok {
			return expandVariablesImpl(val, vars, depth+1, preserveUndefined)
		}
		if preserveUndefined {
			return "${" + expr + "}", nil
		}
		return "", nil
	}

	varName := expr[:colonIdx]
	if colonIdx+1 >= len(expr) {
		// ${VAR:} with nothing after colon
		if val, ok := vars[varName]; ok {
			return expandVariablesImpl(val, vars, depth+1, preserveUndefined)
		}
		if preserveUndefined {
			return "${" + expr + "}", nil
		}
		return "", nil
	}

	modifier := expr[colonIdx+1]
	value := expr[colonIdx+2:]

	varVal, varSet := vars[varName]
	varEmpty := !varSet || varVal == ""

	switch modifier {
	case '-':
		// ${VAR:-default}: use default if VAR is unset or empty
		if varEmpty {
			return expandVariablesImpl(value, vars, depth+1, preserveUndefined)
		}
		return expandVariablesImpl(varVal, vars, depth+1, preserveUndefined)

	case '+':
		// ${VAR:+alternate}: use alternate if VAR is set and non-empty
		if !varEmpty {
			return expandVariablesImpl(value, vars, depth+1, preserveUndefined)
		}
		return "", nil

	default:
		// Unknown modifier, treat as literal
		if val, ok := vars[varName]; ok {
			return expandVariablesImpl(val, vars, depth+1, preserveUndefined)
		}
		if preserveUndefined {
			return "${" + expr + "}", nil
		}
		return "", nil
	}
}

// isVarChar returns true if c is valid in a variable name.
func isVarChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

// ExpandVariables expands variables in a string using the provided map.
// Undefined variables are expanded to empty string (Docker behavior for ENV, COPY, etc).
// This is the public entry point.
func ExpandVariables(s string, vars map[string]string) (string, error) {
	return expandVariablesImpl(s, vars, 0, false)
}

// ExpandVariablesPreserve expands variables in a string, but leaves undefined
// variables as-is. This is used for RUN commands where shell variables should
// be passed through to the shell for expansion.
func ExpandVariablesPreserve(s string, vars map[string]string) (string, error) {
	return expandVariablesImpl(s, vars, 0, true)
}
