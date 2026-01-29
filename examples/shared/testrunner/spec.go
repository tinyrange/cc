package testrunner

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// TestSpec defines the complete test specification for an example.
type TestSpec struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Build       BuildConfig  `yaml:"build"`
	Server      ServerConfig `yaml:"server"`
	Tests       []TestCase   `yaml:"tests"`
	// CLI specifies this is a CLI test (no server).
	CLI      *CLIConfig    `yaml:"cli,omitempty"`
	CLITests []CLITestCase `yaml:"cli_tests,omitempty"`
	// CC2 specifies this is a cc2 test (container VM).
	CC2      *CC2Config    `yaml:"cc2,omitempty"`
	CC2Tests []CC2TestCase `yaml:"cc2_tests,omitempty"`
}

// IsCLI returns true if this spec is for CLI tests (no server).
func (s *TestSpec) IsCLI() bool {
	return s.CLI != nil || len(s.CLITests) > 0
}

// IsCC2 returns true if this spec is for cc2 tests (container VM).
func (s *TestSpec) IsCC2() bool {
	return s.CC2 != nil || len(s.CC2Tests) > 0
}

// BuildConfig configures how to build the example binary.
type BuildConfig struct {
	Package string   `yaml:"package"`
	Timeout Duration `yaml:"timeout"`
}

// ServerConfig configures how to run the server.
type ServerConfig struct {
	Port            int               `yaml:"port"`
	StartupTimeout  Duration          `yaml:"startup_timeout"`
	ShutdownTimeout Duration          `yaml:"shutdown_timeout"`
	Env             map[string]string `yaml:"env"`
}

// CLIConfig configures CLI (non-server) tests.
type CLIConfig struct {
	// Timeout for each CLI test execution.
	Timeout Duration `yaml:"timeout"`
	// Env sets environment variables for all CLI tests.
	Env map[string]string `yaml:"env"`
}

// CLITestCase defines a single CLI test case.
type CLITestCase struct {
	Name   string            `yaml:"name"`
	Args   []string          `yaml:"args"`
	Stdin  string            `yaml:"stdin"`
	Env    map[string]string `yaml:"env"`
	Expect CLIExpectation    `yaml:"expect"`
}

// CLIExpectation defines expected CLI output values.
type CLIExpectation struct {
	ExitCode       int    `yaml:"exit_code"`
	StdoutContains string `yaml:"stdout_contains"`
	StdoutEquals   string `yaml:"stdout_equals"`
	StderrContains string `yaml:"stderr_contains"`
	StderrEquals   string `yaml:"stderr_equals"`
}

// TestCase defines a single HTTP test case.
type TestCase struct {
	Name       string            `yaml:"name"`
	Method     string            `yaml:"method"`
	Path       string            `yaml:"path"`
	Headers    map[string]string `yaml:"headers"`
	Body       any               `yaml:"body"`
	BodyRaw    string            `yaml:"body_raw"`
	BodyBase64 string            `yaml:"body_base64"`
	Expect     Expectation       `yaml:"expect"`
}

// Expectation defines expected response values.
type Expectation struct {
	Status       int               `yaml:"status"`
	Headers      map[string]string `yaml:"headers"`
	BodyContains string            `yaml:"body_contains"`
	BodyEquals   string            `yaml:"body_equals"`
	JSON         map[string]any    `yaml:"json"`
}

// Duration wraps time.Duration for YAML unmarshaling.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// CC2Config configures cc2 tests.
type CC2Config struct {
	Timeout  Duration `yaml:"timeout"`   // Default timeout per test (default: 2m)
	CacheDir string   `yaml:"cache_dir"` // Shared OCI cache
	Image    string   `yaml:"image"`     // Default image (alpine:latest)
}

// CC2TestCase defines a single cc2 test case.
type CC2TestCase struct {
	Name   string `yaml:"name"`
	Skip   bool   `yaml:"skip"`
	SkipCI bool   `yaml:"skip_ci"`

	// Source (one of - uses cc2.Image default if none specified)
	Image      string `yaml:"image"`
	Dockerfile string `yaml:"dockerfile"`
	Bundle     string `yaml:"bundle"`

	// cc2 flags
	Flags CC2Flags `yaml:"flags"`

	// Command and input
	Command []string `yaml:"command"`
	Stdin   string   `yaml:"stdin"`

	// Fixtures
	Fixtures CC2Fixtures `yaml:"fixtures"`

	// Expected results
	Expect CC2Expectation `yaml:"expect"`
}

// CC2Flags maps to cc2 command-line flags.
type CC2Flags struct {
	Memory     uint64   `yaml:"memory"`
	CPUs       int      `yaml:"cpus"`
	Timeout    Duration `yaml:"timeout"`
	Workdir    string   `yaml:"workdir"`
	User       string   `yaml:"user"`
	Dmesg      bool     `yaml:"dmesg"`
	Exec       bool     `yaml:"exec"`
	Packetdump string   `yaml:"packetdump"`
	GPU        bool     `yaml:"gpu"`
	Arch       string   `yaml:"arch"`
	CacheDir   string   `yaml:"cache_dir"`
	Build      string   `yaml:"build"`
	Env        []string `yaml:"env"`
	Mounts     []string `yaml:"mounts"`
}

// CC2Fixtures defines test setup files.
type CC2Fixtures struct {
	Files map[string]string `yaml:"files"` // path -> content
	Dirs  []string          `yaml:"dirs"`
}

// CC2Expectation defines expected cc2 test results.
type CC2Expectation struct {
	ExitCode       int                   `yaml:"exit_code"`
	StdoutEquals   string                `yaml:"stdout_equals"`
	StdoutContains string                `yaml:"stdout_contains"`
	StderrEquals   string                `yaml:"stderr_equals"`
	StderrContains string                `yaml:"stderr_contains"`
	Files          map[string]FileExpect `yaml:"files"`
}

// FileExpect defines file existence/content expectations.
type FileExpect struct {
	Exists   bool   `yaml:"exists"`
	Contains string `yaml:"contains"`
}

// LoadSpec loads a test specification from a YAML file.
func LoadSpec(path string) (*TestSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec file: %w", err)
	}

	var spec TestSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing spec file: %w", err)
	}

	// Apply defaults
	if spec.Build.Timeout == 0 {
		spec.Build.Timeout = Duration(30 * time.Second)
	}
	if spec.Server.StartupTimeout == 0 {
		spec.Server.StartupTimeout = Duration(10 * time.Second)
	}
	if spec.Server.ShutdownTimeout == 0 {
		spec.Server.ShutdownTimeout = Duration(5 * time.Second)
	}

	return &spec, nil
}
