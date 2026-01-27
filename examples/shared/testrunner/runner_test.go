package testrunner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResponse_JSON(t *testing.T) {
	resp := &Response{
		StatusCode: 200,
		Body:       []byte(`{"key": "value", "num": 42}`),
	}

	var data map[string]any
	if err := resp.JSON(&data); err != nil {
		t.Fatalf("JSON failed: %v", err)
	}

	if data["key"] != "value" {
		t.Errorf("key = %v, want %q", data["key"], "value")
	}
	if data["num"] != float64(42) {
		t.Errorf("num = %v, want 42", data["num"])
	}
}

func TestResponse_String(t *testing.T) {
	resp := &Response{
		StatusCode: 200,
		Body:       []byte("hello world"),
	}

	if resp.String() != "hello world" {
		t.Errorf("String = %q, want %q", resp.String(), "hello world")
	}
}

func TestLoadSpec(t *testing.T) {
	// Create temp spec file
	dir := t.TempDir()
	specPath := filepath.Join(dir, "test.yaml")

	content := `
name: test-example
description: A test example

build:
  package: ./cmd/test
  timeout: 60s

server:
  port: 0
  startup_timeout: 5s
  shutdown_timeout: 3s
  env:
    TEST_VAR: "hello"

tests:
  - name: health check
    method: GET
    path: /health
    expect:
      status: 200
      body_contains: "ok"

  - name: post data
    method: POST
    path: /api/data
    body:
      key: value
    expect:
      status: 201
      json:
        success: true
`
	if err := os.WriteFile(specPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	spec, err := LoadSpec(specPath)
	if err != nil {
		t.Fatalf("LoadSpec failed: %v", err)
	}

	if spec.Name != "test-example" {
		t.Errorf("Name = %q, want %q", spec.Name, "test-example")
	}

	if spec.Build.Package != "./cmd/test" {
		t.Errorf("Build.Package = %q, want %q", spec.Build.Package, "./cmd/test")
	}

	if len(spec.Tests) != 2 {
		t.Errorf("Tests len = %d, want 2", len(spec.Tests))
	}

	if spec.Server.Env["TEST_VAR"] != "hello" {
		t.Errorf("Server.Env[TEST_VAR] = %q, want %q", spec.Server.Env["TEST_VAR"], "hello")
	}
}

func TestAssertResponse_Status(t *testing.T) {
	resp := &Response{StatusCode: 200}

	// Should pass
	errs := AssertResponse(resp, Expectation{Status: 200})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Should fail
	errs = AssertResponse(resp, Expectation{Status: 201})
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestAssertResponse_BodyContains(t *testing.T) {
	resp := &Response{Body: []byte("hello world")}

	// Should pass
	errs := AssertResponse(resp, Expectation{BodyContains: "world"})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Should fail
	errs = AssertResponse(resp, Expectation{BodyContains: "foo"})
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestAssertResponse_JSON(t *testing.T) {
	resp := &Response{
		Body: []byte(`{"name": "test", "count": 42, "active": true}`),
	}

	// Should pass exact match
	errs := AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"name":   "test",
			"count":  42,
			"active": true,
		},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Should fail on wrong value
	errs = AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"name": "wrong",
		},
	})
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestAssertResponse_JSON_Contains(t *testing.T) {
	resp := &Response{
		Body: []byte(`{"error": "unsupported language: foo"}`),
	}

	// Should pass contains assertion
	errs := AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"error": map[string]any{"contains": "unsupported"},
		},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Should fail contains assertion
	errs = AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"error": map[string]any{"contains": "notfound"},
		},
	})
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestAssertResponse_JSON_Type(t *testing.T) {
	resp := &Response{
		Body: []byte(`{"str": "hello", "num": 42, "flag": true, "arr": [1,2], "obj": {}}`),
	}

	// Should pass type assertions
	errs := AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"str":  map[string]any{"type": "string"},
			"num":  map[string]any{"type": "number"},
			"flag": map[string]any{"type": "bool"},
			"arr":  map[string]any{"type": "array"},
			"obj":  map[string]any{"type": "object"},
		},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestAssertResponse_JSON_Numeric(t *testing.T) {
	resp := &Response{
		Body: []byte(`{"value": 50}`),
	}

	// Should pass gt
	errs := AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"value": map[string]any{"gt": 40},
		},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors for gt, got %v", errs)
	}

	// Should fail gt
	errs = AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"value": map[string]any{"gt": 60},
		},
	})
	if len(errs) != 1 {
		t.Errorf("expected 1 error for gt, got %d", len(errs))
	}

	// Should pass lt
	errs = AssertResponse(resp, Expectation{
		JSON: map[string]any{
			"value": map[string]any{"lt": 60},
		},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors for lt, got %v", errs)
	}
}

func TestRunner_buildRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"method":       r.Method,
			"path":         r.URL.Path,
			"content_type": r.Header.Get("Content-Type"),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	runner := NewRunner()

	// Test with JSON body
	tc := TestCase{
		Method: "POST",
		Path:   "/test",
		Body:   map[string]string{"key": "value"},
	}

	req, err := runner.buildRequest(context.Background(), server.URL, tc)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("Method = %s, want POST", req.Method)
	}

	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", req.Header.Get("Content-Type"))
	}
}

func TestRunner_buildRequest_RawBody(t *testing.T) {
	runner := NewRunner()

	tc := TestCase{
		Method:  "POST",
		Path:    "/test",
		BodyRaw: "raw data",
		Headers: map[string]string{"Content-Type": "text/plain"},
	}

	req, err := runner.buildRequest(context.Background(), "http://example.com", tc)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	if req.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %s, want text/plain", req.Header.Get("Content-Type"))
	}
}

func TestFindAvailablePort(t *testing.T) {
	port, err := findAvailablePort()
	if err != nil {
		t.Fatalf("findAvailablePort failed: %v", err)
	}

	if port <= 0 || port > 65535 {
		t.Errorf("port = %d, want valid port number", port)
	}
}

func TestFormatErrors(t *testing.T) {
	// Empty errors
	result := FormatErrors(nil)
	if result != "" {
		t.Errorf("FormatErrors(nil) = %q, want empty", result)
	}

	// Single error
	errs := []error{&AssertionError{Field: "status", Expected: 200, Actual: 404}}
	result = FormatErrors(errs)
	if result == "" {
		t.Error("FormatErrors returned empty for single error")
	}

	// Multiple errors
	errs = []error{
		&AssertionError{Field: "status", Expected: 200, Actual: 404},
		&AssertionError{Field: "body", Expected: "ok", Actual: "error"},
	}
	result = FormatErrors(errs)
	if result == "" {
		t.Error("FormatErrors returned empty for multiple errors")
	}
}
