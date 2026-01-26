package testrunner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// AssertionError represents a failed assertion.
type AssertionError struct {
	Field    string
	Expected any
	Actual   any
	Message  string
}

func (e *AssertionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%s: expected %v, got %v", e.Field, e.Expected, e.Actual)
}

// AssertResponse checks if the response matches expectations.
func AssertResponse(resp *Response, expect Expectation) []error {
	var errors []error

	// Check status code
	if expect.Status != 0 && resp.StatusCode != expect.Status {
		errors = append(errors, &AssertionError{
			Field:    "status",
			Expected: expect.Status,
			Actual:   resp.StatusCode,
		})
	}

	// Check headers
	for key, expected := range expect.Headers {
		actual := resp.Headers.Get(key)
		if actual != expected {
			errors = append(errors, &AssertionError{
				Field:    fmt.Sprintf("header[%s]", key),
				Expected: expected,
				Actual:   actual,
			})
		}
	}

	// Check body contains
	if expect.BodyContains != "" {
		body := string(resp.Body)
		if !strings.Contains(body, expect.BodyContains) {
			errors = append(errors, &AssertionError{
				Field:    "body",
				Expected: fmt.Sprintf("contains %q", expect.BodyContains),
				Actual:   truncate(body, 200),
			})
		}
	}

	// Check body equals
	if expect.BodyEquals != "" {
		body := string(resp.Body)
		if body != expect.BodyEquals {
			errors = append(errors, &AssertionError{
				Field:    "body",
				Expected: truncate(expect.BodyEquals, 200),
				Actual:   truncate(body, 200),
			})
		}
	}

	// Check JSON fields
	if len(expect.JSON) > 0 {
		var jsonBody map[string]any
		if err := json.Unmarshal(resp.Body, &jsonBody); err != nil {
			errors = append(errors, &AssertionError{
				Message: fmt.Sprintf("failed to parse response as JSON: %v", err),
			})
		} else {
			jsonErrors := assertJSONFields(jsonBody, expect.JSON, "")
			errors = append(errors, jsonErrors...)
		}
	}

	return errors
}

// assertJSONFields recursively checks JSON field expectations.
func assertJSONFields(actual map[string]any, expected map[string]any, prefix string) []error {
	var errors []error

	for key, expectedValue := range expected {
		fieldPath := key
		if prefix != "" {
			fieldPath = prefix + "." + key
		}

		actualValue, exists := actual[key]

		// Handle special assertion objects
		if m, ok := expectedValue.(map[string]any); ok {
			if err := handleSpecialAssertion(fieldPath, actualValue, m, exists); err != nil {
				errors = append(errors, err)
				continue
			}
			// If it was a special assertion, we're done with this field
			if isSpecialAssertion(m) {
				continue
			}
			// Otherwise, recurse for nested objects
			if actualMap, ok := actualValue.(map[string]any); ok {
				nested := assertJSONFields(actualMap, m, fieldPath)
				errors = append(errors, nested...)
			} else if exists {
				errors = append(errors, &AssertionError{
					Field:    fieldPath,
					Expected: "object",
					Actual:   fmt.Sprintf("%T", actualValue),
				})
			}
			continue
		}

		if !exists {
			errors = append(errors, &AssertionError{
				Field:    fieldPath,
				Expected: expectedValue,
				Actual:   "<missing>",
			})
			continue
		}

		// Direct value comparison
		if !valuesEqual(actualValue, expectedValue) {
			errors = append(errors, &AssertionError{
				Field:    fieldPath,
				Expected: expectedValue,
				Actual:   actualValue,
			})
		}
	}

	return errors
}

// isSpecialAssertion checks if a map is a special assertion object.
func isSpecialAssertion(m map[string]any) bool {
	specialKeys := []string{"contains", "type", "gt", "lt", "gte", "lte", "exists", "regex"}
	for _, key := range specialKeys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

// handleSpecialAssertion handles special assertion objects like {contains: "foo"}.
func handleSpecialAssertion(fieldPath string, actualValue any, assertion map[string]any, exists bool) error {
	// Check exists
	if existsVal, ok := assertion["exists"]; ok {
		shouldExist, _ := existsVal.(bool)
		if shouldExist && !exists {
			return &AssertionError{
				Field:    fieldPath,
				Expected: "exists",
				Actual:   "<missing>",
			}
		}
		if !shouldExist && exists {
			return &AssertionError{
				Field:    fieldPath,
				Expected: "not exists",
				Actual:   actualValue,
			}
		}
		return nil
	}

	if !exists {
		return &AssertionError{
			Field:    fieldPath,
			Expected: assertion,
			Actual:   "<missing>",
		}
	}

	// Check contains
	if containsVal, ok := assertion["contains"]; ok {
		expected := fmt.Sprintf("%v", containsVal)
		actual := fmt.Sprintf("%v", actualValue)
		if !strings.Contains(actual, expected) {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf("contains %q", expected),
				Actual:   actual,
			}
		}
		return nil
	}

	// Check type
	if typeVal, ok := assertion["type"]; ok {
		expectedType := fmt.Sprintf("%v", typeVal)
		actualType := getJSONType(actualValue)
		if actualType != expectedType {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf("type %s", expectedType),
				Actual:   fmt.Sprintf("type %s", actualType),
			}
		}
		return nil
	}

	// Check numeric comparisons
	if gtVal, ok := assertion["gt"]; ok {
		actualNum, ok := toFloat64(actualValue)
		expectedNum, _ := toFloat64(gtVal)
		if !ok || actualNum <= expectedNum {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf("> %v", expectedNum),
				Actual:   actualValue,
			}
		}
	}

	if ltVal, ok := assertion["lt"]; ok {
		actualNum, ok := toFloat64(actualValue)
		expectedNum, _ := toFloat64(ltVal)
		if !ok || actualNum >= expectedNum {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf("< %v", expectedNum),
				Actual:   actualValue,
			}
		}
	}

	if gteVal, ok := assertion["gte"]; ok {
		actualNum, ok := toFloat64(actualValue)
		expectedNum, _ := toFloat64(gteVal)
		if !ok || actualNum < expectedNum {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf(">= %v", expectedNum),
				Actual:   actualValue,
			}
		}
	}

	if lteVal, ok := assertion["lte"]; ok {
		actualNum, ok := toFloat64(actualValue)
		expectedNum, _ := toFloat64(lteVal)
		if !ok || actualNum > expectedNum {
			return &AssertionError{
				Field:    fieldPath,
				Expected: fmt.Sprintf("<= %v", expectedNum),
				Actual:   actualValue,
			}
		}
	}

	return nil
}

// getJSONType returns the JSON type name for a value.
func getJSONType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case bool:
		return "bool"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return reflect.TypeOf(v).String()
	}
}

// toFloat64 converts a value to float64.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// valuesEqual compares two values for equality.
func valuesEqual(a, b any) bool {
	// Handle numeric comparison (JSON numbers are float64)
	if aNum, aOk := toFloat64(a); aOk {
		if bNum, bOk := toFloat64(b); bOk {
			return aNum == bNum
		}
	}
	return reflect.DeepEqual(a, b)
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FormatErrors formats multiple errors into a single string.
func FormatErrors(errors []error) string {
	if len(errors) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range errors {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AssertHeaders checks response headers match expectations.
func AssertHeaders(resp http.Header, expected map[string]string) []error {
	var errors []error
	for key, expectedVal := range expected {
		actual := resp.Get(key)
		if actual != expectedVal {
			errors = append(errors, &AssertionError{
				Field:    fmt.Sprintf("header[%s]", key),
				Expected: expectedVal,
				Actual:   actual,
			})
		}
	}
	return errors
}

// AssertCLIOutput checks CLI command output matches expectations.
func AssertCLIOutput(stdout, stderr string, exitCode int, expect CLIExpectation) []error {
	var errors []error

	// Check exit code
	if exitCode != expect.ExitCode {
		errors = append(errors, &AssertionError{
			Field:    "exit_code",
			Expected: expect.ExitCode,
			Actual:   exitCode,
		})
	}

	// Check stdout contains
	if expect.StdoutContains != "" {
		if !strings.Contains(stdout, expect.StdoutContains) {
			errors = append(errors, &AssertionError{
				Field:    "stdout",
				Expected: fmt.Sprintf("contains %q", expect.StdoutContains),
				Actual:   truncate(stdout, 200),
			})
		}
	}

	// Check stdout equals
	if expect.StdoutEquals != "" {
		if stdout != expect.StdoutEquals {
			errors = append(errors, &AssertionError{
				Field:    "stdout",
				Expected: truncate(expect.StdoutEquals, 200),
				Actual:   truncate(stdout, 200),
			})
		}
	}

	// Check stderr contains
	if expect.StderrContains != "" {
		if !strings.Contains(stderr, expect.StderrContains) {
			errors = append(errors, &AssertionError{
				Field:    "stderr",
				Expected: fmt.Sprintf("contains %q", expect.StderrContains),
				Actual:   truncate(stderr, 200),
			})
		}
	}

	// Check stderr equals
	if expect.StderrEquals != "" {
		if stderr != expect.StderrEquals {
			errors = append(errors, &AssertionError{
				Field:    "stderr",
				Expected: truncate(expect.StderrEquals, 200),
				Actual:   truncate(stderr, 200),
			})
		}
	}

	return errors
}
