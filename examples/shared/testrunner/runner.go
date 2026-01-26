// Package testrunner provides a YAML-driven test framework for example services.
package testrunner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner manages the lifecycle of example services for testing.
type Runner struct {
	Verbose   bool
	KeepAlive bool
	Parallel  int
}

// NewRunner creates a new test runner.
func NewRunner() *Runner {
	return &Runner{
		Parallel: 4, // Default parallelism for builds
	}
}

// Results contains the results of running tests.
type Results struct {
	Examples []ExampleResult
	Total    int
	Passed   int
	Failed   int
	Duration time.Duration
}

// ExampleResult contains results for a single example.
type ExampleResult struct {
	Name     string
	Tests    []TestResult
	Total    int
	Passed   int
	Failed   int
	Error    string
	Duration time.Duration
}

// TestResult contains the result of a single test case.
type TestResult struct {
	Name     string
	Passed   bool
	Error    string
	Duration time.Duration
}

// Run executes tests for all matching patterns.
func (r *Runner) Run(ctx context.Context, patterns []string) (*Results, error) {
	start := time.Now()
	results := &Results{}

	// Find all test.yaml files
	specPaths, err := r.findSpecs(patterns)
	if err != nil {
		return nil, fmt.Errorf("finding specs: %w", err)
	}

	if len(specPaths) == 0 {
		return nil, fmt.Errorf("no test.yaml files found matching patterns")
	}

	// Load specs
	specs := make([]*TestSpec, 0, len(specPaths))
	specDirs := make([]string, 0, len(specPaths))
	for _, path := range specPaths {
		spec, err := LoadSpec(path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}
		specs = append(specs, spec)
		specDirs = append(specDirs, filepath.Dir(path))
	}

	// Build all binaries in parallel
	if r.Verbose {
		fmt.Printf("Building %d examples...\n", len(specs))
	}

	binaries, err := r.buildAll(ctx, specs, specDirs)
	if err != nil {
		return nil, fmt.Errorf("building: %w", err)
	}

	// Run tests for each example
	for i, spec := range specs {
		if ctx.Err() != nil {
			break
		}

		result := r.runExample(ctx, spec, binaries[i])
		results.Examples = append(results.Examples, result)
		results.Total += result.Total
		results.Passed += result.Passed
		results.Failed += result.Failed
	}

	results.Duration = time.Since(start)
	return results, nil
}

// findSpecs finds all test.yaml files matching the given patterns.
func (r *Runner) findSpecs(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		patterns = []string{"./examples/..."}
	}

	var paths []string
	seen := make(map[string]bool)

	for _, pattern := range patterns {
		// Handle ... pattern
		if strings.HasSuffix(pattern, "/...") {
			baseDir := strings.TrimSuffix(pattern, "/...")
			err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.Name() == "test.yaml" && !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			// Treat as directory path
			testYaml := filepath.Join(pattern, "test.yaml")
			if _, err := os.Stat(testYaml); err == nil {
				if !seen[testYaml] {
					seen[testYaml] = true
					paths = append(paths, testYaml)
				}
			}
		}
	}

	return paths, nil
}

// buildAll builds all binaries in parallel.
func (r *Runner) buildAll(ctx context.Context, specs []*TestSpec, dirs []string) ([]string, error) {
	binaries := make([]string, len(specs))
	errors := make([]error, len(specs))
	var wg sync.WaitGroup

	// Create semaphore for parallelism
	sem := make(chan struct{}, r.Parallel)

	for i := range specs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			binary, err := r.buildExample(ctx, specs[idx], dirs[idx])
			if err != nil {
				errors[idx] = err
				return
			}
			binaries[idx] = binary
		}(i)
	}

	wg.Wait()

	// Collect errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("building %s: %w", specs[i].Name, err)
		}
	}

	return binaries, nil
}

// buildExample builds a single example binary.
func (r *Runner) buildExample(ctx context.Context, spec *TestSpec, dir string) (string, error) {
	timeout := spec.Build.Timeout.Duration()
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Determine package path
	pkg := spec.Build.Package
	if pkg == "" {
		pkg = "./" + dir
	}

	// Create temp file for binary
	tmpFile, err := os.CreateTemp("", "testrunner-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpFile.Close()
	binaryPath := tmpFile.Name()

	// Build
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if r.Verbose {
		fmt.Printf("  Building %s...\n", spec.Name)
	}

	if err := cmd.Run(); err != nil {
		os.Remove(binaryPath)
		return "", fmt.Errorf("go build failed: %s", stderr.String())
	}

	return binaryPath, nil
}

// runExample runs tests for a single example.
func (r *Runner) runExample(ctx context.Context, spec *TestSpec, binaryPath string) ExampleResult {
	start := time.Now()

	// Handle CLI tests (no server)
	if spec.IsCLI() {
		return r.runCLIExample(ctx, spec, binaryPath)
	}

	result := ExampleResult{
		Name:  spec.Name,
		Total: len(spec.Tests),
	}

	// Clean up binary after tests
	if !r.KeepAlive {
		defer os.Remove(binaryPath)
	}

	// Start server
	server, err := r.startServer(ctx, spec, binaryPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to start server: %v", err)
		result.Failed = result.Total
		result.Duration = time.Since(start)
		return result
	}

	if !r.KeepAlive {
		defer server.Stop()
	}

	// Run test cases
	for _, tc := range spec.Tests {
		tcResult := r.runTestCase(ctx, server, tc)
		result.Tests = append(result.Tests, tcResult)
		if tcResult.Passed {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	result.Duration = time.Since(start)

	// Print result
	if result.Failed > 0 {
		fmt.Printf("[FAIL] %s (%d/%d tests)\n", spec.Name, result.Passed, result.Total)
		for _, tr := range result.Tests {
			if !tr.Passed {
				fmt.Printf("       - %s: %s\n", tr.Name, tr.Error)
			}
		}
	} else if r.Verbose {
		fmt.Printf("[PASS] %s (%d/%d tests)\n", spec.Name, result.Passed, result.Total)
	}

	return result
}

// runCLIExample runs CLI tests for a single example (no server).
func (r *Runner) runCLIExample(ctx context.Context, spec *TestSpec, binaryPath string) ExampleResult {
	start := time.Now()
	result := ExampleResult{
		Name:  spec.Name,
		Total: len(spec.CLITests),
	}

	// Clean up binary after tests
	if !r.KeepAlive {
		defer os.Remove(binaryPath)
	}

	// Determine timeout
	timeout := 30 * time.Second
	if spec.CLI != nil && spec.CLI.Timeout.Duration() > 0 {
		timeout = spec.CLI.Timeout.Duration()
	}

	// Run CLI test cases
	for _, tc := range spec.CLITests {
		tcResult := r.runCLITestCase(ctx, spec, binaryPath, tc, timeout)
		result.Tests = append(result.Tests, tcResult)
		if tcResult.Passed {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	result.Duration = time.Since(start)

	// Print result
	if result.Failed > 0 {
		fmt.Printf("[FAIL] %s (%d/%d tests)\n", spec.Name, result.Passed, result.Total)
		for _, tr := range result.Tests {
			if !tr.Passed {
				fmt.Printf("       - %s: %s\n", tr.Name, tr.Error)
			}
		}
	} else if r.Verbose {
		fmt.Printf("[PASS] %s (%d/%d tests)\n", spec.Name, result.Passed, result.Total)
	}

	return result
}

// runCLITestCase executes a single CLI test case.
func (r *Runner) runCLITestCase(ctx context.Context, spec *TestSpec, binaryPath string, tc CLITestCase, defaultTimeout time.Duration) TestResult {
	start := time.Now()
	result := TestResult{Name: tc.Name}

	// Create command with timeout
	testCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(testCtx, binaryPath, tc.Args...)

	// Set up stdout/stderr capture
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set stdin if provided
	if tc.Stdin != "" {
		cmd.Stdin = strings.NewReader(tc.Stdin)
	}

	// Set environment
	env := os.Environ()
	if spec.CLI != nil {
		for k, v := range spec.CLI.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	for k, v := range tc.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	// Run command
	err := cmd.Run()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if testCtx.Err() == context.DeadlineExceeded {
			result.Error = "test timed out"
			result.Duration = time.Since(start)
			return result
		}
	}

	// Assert results
	errors := AssertCLIOutput(stdout.String(), stderr.String(), exitCode, tc.Expect)
	if len(errors) > 0 {
		result.Error = FormatErrors(errors)
	} else {
		result.Passed = true
	}

	result.Duration = time.Since(start)
	return result
}

// Server represents a running example server.
type Server struct {
	cmd     *exec.Cmd
	port    int
	baseURL string
	client  *http.Client
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
}

// startServer starts the example server.
func (r *Runner) startServer(ctx context.Context, spec *TestSpec, binaryPath string) (*Server, error) {
	// Find available port
	port, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("finding port: %w", err)
	}

	server := &Server{
		port:    port,
		baseURL: fmt.Sprintf("http://localhost:%d", port),
		client:  &http.Client{Timeout: 10 * time.Second},
		stdout:  new(bytes.Buffer),
		stderr:  new(bytes.Buffer),
	}

	// Create command
	server.cmd = exec.Command(binaryPath)
	server.cmd.Stdout = server.stdout
	server.cmd.Stderr = server.stderr

	// Set environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("PORT=%d", port))
	for k, v := range spec.Server.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	server.cmd.Env = env

	// Start process
	if err := server.cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting server: %w", err)
	}

	// Wait for health endpoint
	timeout := spec.Server.StartupTimeout.Duration()
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	healthCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := server.waitForHealth(healthCtx); err != nil {
		server.Stop()
		return nil, fmt.Errorf("waiting for health: %w (stdout: %s, stderr: %s)",
			err, server.stdout.String(), server.stderr.String())
	}

	return server, nil
}

// waitForHealth polls the health endpoint until it responds.
func (s *Server) waitForHealth(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := s.client.Get(s.baseURL + "/health")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// Stop terminates the server.
func (s *Server) Stop() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	// Send interrupt signal
	s.cmd.Process.Signal(os.Interrupt)

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
	}

	return nil
}

// runTestCase executes a single test case.
func (r *Runner) runTestCase(ctx context.Context, server *Server, tc TestCase) TestResult {
	start := time.Now()
	result := TestResult{Name: tc.Name}

	// Build request
	req, err := r.buildRequest(ctx, server.baseURL, tc)
	if err != nil {
		result.Error = fmt.Sprintf("building request: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Execute request
	resp, err := server.client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("reading response: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Create Response object
	response := &Response{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    resp.Header,
	}

	// Assert response
	errors := AssertResponse(response, tc.Expect)
	if len(errors) > 0 {
		result.Error = FormatErrors(errors)
	} else {
		result.Passed = true
	}

	result.Duration = time.Since(start)
	return result
}

// buildRequest creates an HTTP request from a test case.
func (r *Runner) buildRequest(ctx context.Context, baseURL string, tc TestCase) (*http.Request, error) {
	var body io.Reader

	// Determine request body
	if tc.Body != nil {
		data, err := json.Marshal(tc.Body)
		if err != nil {
			return nil, fmt.Errorf("marshaling body: %w", err)
		}
		body = bytes.NewReader(data)
	} else if tc.BodyRaw != "" {
		body = strings.NewReader(tc.BodyRaw)
	} else if tc.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(tc.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("decoding base64 body: %w", err)
		}
		body = bytes.NewReader(decoded)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, tc.Method, baseURL+tc.Path, body)
	if err != nil {
		return nil, err
	}

	// Set default content type for JSON bodies
	if tc.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set custom headers
	for k, v := range tc.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

// Response represents an HTTP response.
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// JSON unmarshals the response body into v.
func (r *Response) JSON(v any) error {
	return json.Unmarshal(r.Body, v)
}

// String returns the response body as a string.
func (r *Response) String() string {
	return string(r.Body)
}

// findAvailablePort finds an available TCP port.
func findAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// PrintResults prints a summary of test results.
func PrintResults(results *Results) {
	fmt.Println()
	fmt.Printf("Results: %d examples, %d tests, %d passed, %d failed\n",
		len(results.Examples), results.Total, results.Passed, results.Failed)
	fmt.Printf("Duration: %s\n", results.Duration.Round(time.Millisecond))
}
