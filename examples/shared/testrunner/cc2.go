package testrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runCC2Example runs all cc2 tests for a spec.
func (r *Runner) runCC2Example(ctx context.Context, spec *TestSpec, dir string, cc2Binary string) ExampleResult {
	start := time.Now()
	result := ExampleResult{
		Name:  spec.Name,
		Total: len(spec.CC2Tests),
	}

	// Print example header
	r.Output.PrintExampleHeader(spec.Name)

	// Determine default timeout
	timeout := 2 * time.Minute
	if spec.CC2 != nil && spec.CC2.Timeout.Duration() > 0 {
		timeout = spec.CC2.Timeout.Duration()
	}

	// Determine default image
	defaultImage := "alpine:latest"
	if spec.CC2 != nil && spec.CC2.Image != "" {
		defaultImage = spec.CC2.Image
	}

	// Determine cache dir
	cacheDir := ""
	if spec.CC2 != nil && spec.CC2.CacheDir != "" {
		cacheDir = spec.CC2.CacheDir
	}

	// Check if running in CI
	isCI := os.Getenv("CI") != ""

	// Run CC2 test cases
	for _, tc := range spec.CC2Tests {
		// Skip tests if needed
		if tc.Skip {
			result.Tests = append(result.Tests, TestResult{
				Name:   tc.Name,
				Passed: true,
			})
			result.Passed++
			r.Output.PrintTestPass(tc.Name+" (skipped)", 0)
			continue
		}
		if tc.SkipCI && isCI {
			result.Tests = append(result.Tests, TestResult{
				Name:   tc.Name,
				Passed: true,
			})
			result.Passed++
			r.Output.PrintTestPass(tc.Name+" (skipped in CI)", 0)
			continue
		}
		// Skip GPU tests if they require a display
		if tc.Flags.GPU {
			result.Tests = append(result.Tests, TestResult{
				Name:   tc.Name,
				Passed: true,
			})
			result.Passed++
			r.Output.PrintTestPass(tc.Name+" (skipped: requires display)", 0)
			continue
		}

		r.Output.StartTestRun(tc.Name)
		tcResult := r.runCC2TestCase(ctx, cc2Binary, tc, dir, timeout, defaultImage, cacheDir)
		result.Tests = append(result.Tests, tcResult)
		if tcResult.Passed {
			result.Passed++
			r.Output.PrintTestPass(tc.Name, tcResult.Duration)
		} else {
			result.Failed++
			r.Output.PrintTestFail(tc.Name, tcResult.Error, dir, tcResult.Details)
		}
	}

	result.Duration = time.Since(start)
	return result
}

// runCC2TestCase executes a single cc2 test case.
func (r *Runner) runCC2TestCase(ctx context.Context, cc2Binary string, tc CC2TestCase, _ string, defaultTimeout time.Duration, defaultImage string, defaultCacheDir string) TestResult {
	start := time.Now()
	result := TestResult{Name: tc.Name}

	// Create temp directory for test
	testDir, err := os.MkdirTemp("", "cc2test-*")
	if err != nil {
		result.Error = fmt.Sprintf("create temp dir: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer os.RemoveAll(testDir)

	// Set up fixtures
	if err := setupCC2Fixtures(testDir, tc.Fixtures); err != nil {
		result.Error = fmt.Sprintf("setup fixtures: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Determine timeout for this test
	testTimeout := defaultTimeout
	if tc.Flags.Timeout.Duration() > 0 {
		testTimeout = tc.Flags.Timeout.Duration()
	}

	// Build command args
	args := buildCC2Args(tc, defaultImage, defaultCacheDir, testDir)

	// Create command with timeout
	testCtx, cancel := context.WithTimeout(ctx, testTimeout+30*time.Second) // Extra buffer for VM boot
	defer cancel()

	cmd := exec.CommandContext(testCtx, cc2Binary, args...)

	// Set up stdout/stderr capture
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set stdin if provided
	if tc.Stdin != "" {
		cmd.Stdin = strings.NewReader(tc.Stdin)
	}

	// Run command
	err = cmd.Run()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if testCtx.Err() == context.DeadlineExceeded {
			result.Error = "test timed out"
			result.Details = &TestResultDetails{
				Args:     args,
				ExitCode: -1,
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
			}
			result.Duration = time.Since(start)
			return result
		}
	}

	// Validate CLI expectations
	cliExpect := CLIExpectation{
		ExitCode:       tc.Expect.ExitCode,
		StdoutContains: tc.Expect.StdoutContains,
		StdoutEquals:   tc.Expect.StdoutEquals,
		StderrContains: tc.Expect.StderrContains,
		StderrEquals:   tc.Expect.StderrEquals,
	}
	errors := AssertCLIOutput(stdout.String(), stderr.String(), exitCode, cliExpect)

	// Validate file expectations
	fileErrors := assertCC2Files(testDir, tc.Expect.Files)
	for _, fe := range fileErrors {
		errors = append(errors, fmt.Errorf("%s", fe))
	}

	if len(errors) > 0 {
		result.Error = FormatErrors(errors)
		result.Details = &TestResultDetails{
			Args:     args,
			ExitCode: exitCode,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		}
	} else {
		result.Passed = true
	}

	result.Duration = time.Since(start)
	return result
}

// buildCC2Args converts CC2TestCase to command-line arguments.
func buildCC2Args(tc CC2TestCase, defaultImage string, defaultCacheDir string, testDir string) []string {
	var args []string

	// Add flags
	if tc.Flags.Memory != 0 {
		args = append(args, "-memory", fmt.Sprint(tc.Flags.Memory))
	}
	if tc.Flags.CPUs != 0 {
		args = append(args, "-cpus", fmt.Sprint(tc.Flags.CPUs))
	}
	if tc.Flags.Timeout.Duration() > 0 {
		args = append(args, "-timeout", tc.Flags.Timeout.Duration().String())
	}
	if tc.Flags.Workdir != "" {
		args = append(args, "-workdir", tc.Flags.Workdir)
	}
	if tc.Flags.User != "" {
		args = append(args, "-user", tc.Flags.User)
	}
	if tc.Flags.Dmesg {
		args = append(args, "-dmesg")
	}
	if tc.Flags.Exec {
		args = append(args, "-exec")
	}
	if tc.Flags.GPU {
		args = append(args, "-gpu")
	}
	if tc.Flags.Arch != "" {
		args = append(args, "-arch", tc.Flags.Arch)
	}
	if tc.Flags.Build != "" {
		args = append(args, "-build", filepath.Join(testDir, tc.Flags.Build))
	}

	// Cache dir: test-specific > default > none
	cacheDir := tc.Flags.CacheDir
	if cacheDir == "" {
		cacheDir = defaultCacheDir
	}
	if cacheDir != "" {
		args = append(args, "-cache-dir", cacheDir)
	}

	if tc.Flags.Packetdump != "" {
		args = append(args, "-packetdump", filepath.Join(testDir, tc.Flags.Packetdump))
	}
	for _, env := range tc.Flags.Env {
		args = append(args, "-env", env)
	}
	for _, mount := range tc.Flags.Mounts {
		// Resolve mount paths relative to test dir
		parts := strings.Split(mount, ":")
		if len(parts) >= 2 {
			// Check if the host path is relative
			hostPath := parts[1]
			if !filepath.IsAbs(hostPath) {
				parts[1] = filepath.Join(testDir, hostPath)
				mount = strings.Join(parts, ":")
			}
		}
		args = append(args, "-mount", mount)
	}

	// Add source (dockerfile, bundle, or image)
	if tc.Dockerfile != "" {
		dockerfilePath := tc.Dockerfile
		// If it's relative, make it relative to test dir (where fixtures are set up)
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(testDir, dockerfilePath)
		}
		args = append(args, "-dockerfile", dockerfilePath)
	} else if tc.Bundle != "" {
		bundlePath := tc.Bundle
		if !filepath.IsAbs(bundlePath) {
			bundlePath = filepath.Join(testDir, bundlePath)
		}
		args = append(args, bundlePath)
	} else {
		image := tc.Image
		if image == "" {
			image = defaultImage
		}
		args = append(args, image)
	}

	// Add command
	args = append(args, tc.Command...)
	return args
}

// setupCC2Fixtures creates test fixture files and directories.
func setupCC2Fixtures(testDir string, fixtures CC2Fixtures) error {
	for _, dir := range fixtures.Dirs {
		fullPath := filepath.Join(testDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	for path, content := range fixtures.Files {
		fullPath := filepath.Join(testDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write file %s: %w", path, err)
		}
	}
	return nil
}

// assertCC2Files validates file expectations.
func assertCC2Files(testDir string, files map[string]FileExpect) []string {
	var errors []string
	for path, expect := range files {
		fullPath := filepath.Join(testDir, path)
		content, err := os.ReadFile(fullPath)
		exists := err == nil

		if expect.Exists && !exists {
			errors = append(errors, fmt.Sprintf("file %s: expected to exist but does not", path))
		} else if !expect.Exists && exists {
			errors = append(errors, fmt.Sprintf("file %s: expected not to exist but it does", path))
		} else if exists && expect.Contains != "" && !strings.Contains(string(content), expect.Contains) {
			errors = append(errors, fmt.Sprintf("file %s: expected to contain %q but content is %q", path, expect.Contains, truncateOutput(string(content), 100)))
		}
	}
	return errors
}
