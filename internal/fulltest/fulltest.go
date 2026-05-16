package fulltest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/nifti"
)

const (
	DefaultCVMFSMirror           = "https://cvmfs.neurodesk.org"
	DefaultCVMFSRepo             = "neurodesk.ardc.edu.au"
	DefaultMemoryMB              = 12288
	DefaultMaxConsecutiveTimeout = 3
	DefaultOpenNeuroBase         = "https://s3.amazonaws.com/openneuro.org"
)

type RequiredDataset struct {
	Dataset string   `yaml:"dataset"`
	Files   []string `yaml:"files"`
}

type SuiteScript struct {
	Script     string `yaml:"script"`
	HostScript string `yaml:"host_script"`
}

type TestCase struct {
	Name                   string
	Command                string
	Timeout                int
	DependsOn              []string
	ExpectedOutputContains []string
	ExpectedExitCode       int
	IgnoreExitCode         bool
	Validate               []map[string]any
}

type Suite struct {
	Name            string
	Container       string
	EnvSetup        string
	RequiredFiles   []RequiredDataset
	TestData        map[string]string
	Setup           SuiteScript
	Cleanup         SuiteScript
	Tests           []TestCase
	DefaultTimeout  int
	RawDefaultTypo  int
	RecipeDirectory string
}

type Options struct {
	Recipe                  string
	ImageSource             string
	ImageName               string
	WorkDir                 string
	Filter                  string
	KeepVM                  bool
	Mirror                  string
	Repo                    string
	CacheDir                string
	Prefetch                bool
	PrefetchWorkers         int
	MemoryMB                uint64
	CPUs                    int
	Dmesg                   bool
	MaxConsecutiveTimeouts  int
	Dockerfile              string
	DockerContext           string
	DockerTag               string
	DockerBinary            string
	Network                 *client.NetworkConfig
	Progress                io.Writer
	HostCommandTimeoutExtra time.Duration
}

type TestResult struct {
	Name            string  `json:"name"`
	Passed          bool    `json:"passed,omitempty"`
	Skipped         bool    `json:"skipped,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	Message         string  `json:"message,omitempty"`
}

type RunResult struct {
	Suite   string       `json:"suite"`
	WorkDir string       `json:"work_dir"`
	Results []TestResult `json:"results"`
}

type API interface {
	PullImageStream(string, client.PullImageRequest, func(client.ProgressEvent) error) error
	CreateInstanceStreamWithID(string, client.CreateInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error)
	RunIn(string, client.RunRequest) (client.ExecResponse, error)
	ShutdownInstanceWithID(string) error
}

func DefaultCPUs() int {
	n := runtime.NumCPU()
	if n <= 0 {
		return 1
	}
	if n > 16 {
		return 16
	}
	return n
}

func LoadSuite(recipe string) (Suite, error) {
	buf, err := os.ReadFile(recipe)
	if err != nil {
		return Suite{}, err
	}
	var raw struct {
		Name          string            `yaml:"name"`
		Container     string            `yaml:"container"`
		EnvSetup      string            `yaml:"env_setup"`
		RequiredFiles []RequiredDataset `yaml:"required_files"`
		TestData      map[string]string `yaml:"test_data"`
		Setup         SuiteScript       `yaml:"setup"`
		Cleanup       SuiteScript       `yaml:"cleanup"`
		Tests         []struct {
			Name                   string           `yaml:"name"`
			Command                string           `yaml:"command"`
			Script                 string           `yaml:"script"`
			Timeout                int              `yaml:"timeout"`
			DependsOn              stringList       `yaml:"depends_on"`
			ExpectedOutputContains stringList       `yaml:"expected_output_contains"`
			ExpectedExitCode       *int             `yaml:"expected_exit_code"`
			IgnoreExitCode         bool             `yaml:"ignore_exit_code"`
			Validate               []map[string]any `yaml:"validate"`
		} `yaml:"tests"`
		DefaultTimeout int `yaml:"default_timeout"`
		DefaultTimout  int `yaml:"default_timout"`
	}
	if err := yaml.Unmarshal(buf, &raw); err != nil {
		return Suite{}, err
	}
	if strings.TrimSpace(raw.Container) == "" {
		return Suite{}, fmt.Errorf("suite container is required")
	}
	suiteName := strings.TrimSpace(raw.Name)
	if suiteName == "" {
		suiteName = strings.TrimSuffix(filepath.Base(recipe), filepath.Ext(recipe))
	}
	tests := make([]TestCase, 0, len(raw.Tests))
	for i, item := range raw.Tests {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return Suite{}, fmt.Errorf("test %d is missing name", i+1)
		}
		command := item.Command
		if command == "" && item.Script != "" {
			return Suite{}, fmt.Errorf("test %d %q uses matlab script; Go fulltest requires command", i+1, name)
		}
		if command == "" {
			return Suite{}, fmt.Errorf("test %d %q must define command", i+1, name)
		}
		expectedExit := 0
		if item.ExpectedExitCode != nil {
			expectedExit = *item.ExpectedExitCode
		}
		tests = append(tests, TestCase{
			Name:                   name,
			Command:                command,
			Timeout:                item.Timeout,
			DependsOn:              []string(item.DependsOn),
			ExpectedOutputContains: []string(item.ExpectedOutputContains),
			ExpectedExitCode:       expectedExit,
			IgnoreExitCode:         item.IgnoreExitCode,
			Validate:               item.Validate,
		})
	}
	defaultTimeout := raw.DefaultTimeout
	if defaultTimeout == 0 {
		defaultTimeout = raw.DefaultTimout
	}
	if raw.TestData == nil {
		raw.TestData = map[string]string{}
	}
	return Suite{
		Name:            suiteName,
		Container:       raw.Container,
		EnvSetup:        raw.EnvSetup,
		RequiredFiles:   raw.RequiredFiles,
		TestData:        raw.TestData,
		Setup:           raw.Setup,
		Cleanup:         raw.Cleanup,
		Tests:           tests,
		DefaultTimeout:  defaultTimeout,
		RawDefaultTypo:  raw.DefaultTimout,
		RecipeDirectory: filepath.Dir(recipe),
	}, nil
}

type stringList []string

func (s *stringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if strings.TrimSpace(value.Value) == "" {
			*s = nil
			return nil
		}
		*s = []string{value.Value}
		return nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			out = append(out, item.Value)
		}
		*s = out
		return nil
	case 0:
		return nil
	default:
		return fmt.Errorf("expected string or list")
	}
}

func Run(ctx context.Context, api API, opts Options) (RunResult, error) {
	if opts.Recipe == "" {
		return RunResult{}, fmt.Errorf("recipe is required")
	}
	if opts.Mirror == "" {
		opts.Mirror = DefaultCVMFSMirror
	}
	if opts.Repo == "" {
		opts.Repo = DefaultCVMFSRepo
	}
	if opts.MemoryMB == 0 {
		opts.MemoryMB = DefaultMemoryMB
	}
	if opts.CPUs == 0 {
		opts.CPUs = DefaultCPUs()
	}
	suite, err := LoadSuite(opts.Recipe)
	if err != nil {
		return RunResult{}, err
	}
	workDir := opts.WorkDir
	if strings.TrimSpace(workDir) == "" {
		workDir, err = os.MkdirTemp("", "cc-fulltest-")
		if err != nil {
			return RunResult{}, err
		}
	} else if err := os.MkdirAll(workDir, 0o755); err != nil {
		return RunResult{}, err
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return RunResult{}, err
	}
	if err := prepareRequiredFiles(ctx, workDir, suite.RequiredFiles); err != nil {
		return RunResult{}, err
	}

	imageSource := strings.TrimSpace(opts.ImageSource)
	if strings.TrimSpace(opts.Dockerfile) != "" {
		archive, tag, err := buildDockerArchive(ctx, workDir, opts)
		if err != nil {
			return RunResult{}, err
		}
		imageSource = "docker-archive:" + archive + "#" + tag
	}
	imageName := opts.ImageName
	if strings.TrimSpace(imageName) == "" {
		imageName = imageCacheName(firstNonEmpty(imageSource, suite.Container))
	}
	pullReq := buildPullRequest(suite, imageSource, opts)
	progressf(opts.Progress, "[fulltest] suite=%s tests=%d work_dir=%s\n", suite.Name, len(selectTests(suite.Tests, opts.Filter)), workDir)
	progressf(opts.Progress, "[fulltest] importing image=%s source=%s\n", imageName, pullSourceText(pullReq))
	if err := api.PullImageStream(imageName, pullReq, progressReporter(opts.Progress, imageName)); err != nil {
		return RunResult{}, err
	}

	vmID := "fulltest-" + imageCacheName(imageName)[:16]
	progressf(opts.Progress, "[fulltest] loading image=%s memory=%dMiB cpus=%d\n", imageName, opts.MemoryMB, opts.CPUs)
	_, err = api.CreateInstanceStreamWithID(vmID, client.CreateInstanceRequest{
		ID:       vmID,
		Image:    imageName,
		Shares:   []client.ShareMount{{Source: workDir, Mount: "/work", Writable: true}},
		Network:  opts.Network,
		MemoryMB: opts.MemoryMB,
		CPUs:     opts.CPUs,
		Dmesg:    opts.Dmesg,
	}, bootReporter(opts.Progress))
	if err != nil {
		return RunResult{}, err
	}
	if !opts.KeepVM {
		defer api.ShutdownInstanceWithID(vmID)
	}

	guestVars := buildGuestVars(suite.TestData)
	hostVars := buildHostVars(workDir, suite.TestData)
	results := make([]TestResult, 0, len(suite.Tests))
	failed := map[string]bool{}
	consecutiveTimeouts := 0
	selected := selectTests(suite.Tests, opts.Filter)

	if strings.TrimSpace(suite.Setup.HostScript) != "" {
		progressf(opts.Progress, "[fulltest] host setup\n")
		if output, code := runHostScript(ctx, suite.Setup.HostScript, workDir, hostVars, timeoutFor(120, suite.DefaultTimeout), opts.HostCommandTimeoutExtra); code != 0 {
			return RunResult{}, fmt.Errorf("host setup failed with exit code %d: %s", code, output)
		}
	}
	if strings.TrimSpace(suite.Setup.Script) != "" {
		progressf(opts.Progress, "[fulltest] setup\n")
		output, code, err := runGuest(ctx, api, vmID, applyEnvSetup(substitute(suite.Setup.Script, guestVars), suite.EnvSetup), timeoutFor(120, suite.DefaultTimeout))
		if err != nil {
			return RunResult{}, err
		}
		if code != 0 {
			return RunResult{}, fmt.Errorf("setup failed with exit code %d: %s", code, output)
		}
	}

	for i, test := range selected {
		progressf(opts.Progress, "[fulltest] [%d/%d] %s\n", i+1, len(selected), test.Name)
		if hasFailedDependency(test, failed) {
			results = append(results, TestResult{Name: test.Name, Skipped: true, Message: "dependency failed"})
			failed[test.Name] = true
			progressf(opts.Progress, "[fulltest] skipped %s: dependency failed\n", test.Name)
			continue
		}
		start := time.Now()
		command := applyEnvSetup(substitute(test.Command, guestVars), suite.EnvSetup)
		output, code, err := runGuest(ctx, api, vmID, command, timeoutFor(test.Timeout, suite.DefaultTimeout))
		duration := time.Since(start).Seconds()
		message := ""
		if err != nil {
			message = err.Error()
		} else {
			message = validateTest(output, code, test, hostVars)
		}
		if message != "" {
			results = append(results, TestResult{Name: test.Name, DurationSeconds: duration, Message: message})
			failed[test.Name] = true
			progressf(opts.Progress, "[fulltest] failed %s (%.2fs): %s\n", test.Name, duration, message)
			if isTimeoutResult(output, code) {
				consecutiveTimeouts++
				if opts.MaxConsecutiveTimeouts > 0 && consecutiveTimeouts >= opts.MaxConsecutiveTimeouts {
					msg := fmt.Sprintf("aborted after %d consecutive command timeouts", opts.MaxConsecutiveTimeouts)
					progressf(opts.Progress, "[fulltest] %s\n", msg)
					for _, remaining := range selected[i+1:] {
						results = append(results, TestResult{Name: remaining.Name, Skipped: true, Message: msg})
						failed[remaining.Name] = true
					}
					break
				}
			} else {
				consecutiveTimeouts = 0
			}
			continue
		}
		consecutiveTimeouts = 0
		results = append(results, TestResult{Name: test.Name, Passed: true, DurationSeconds: duration, Message: "ok"})
		progressf(opts.Progress, "[fulltest] passed %s (%.2fs)\n", test.Name, duration)
	}

	if strings.TrimSpace(suite.Cleanup.Script) != "" {
		progressf(opts.Progress, "[fulltest] cleanup\n")
		_, _, _ = runGuest(ctx, api, vmID, applyEnvSetup(substitute(suite.Cleanup.Script, guestVars), suite.EnvSetup), timeoutFor(60, suite.DefaultTimeout))
	}
	if strings.TrimSpace(suite.Cleanup.HostScript) != "" {
		progressf(opts.Progress, "[fulltest] host cleanup\n")
		_, _ = runHostScript(ctx, suite.Cleanup.HostScript, workDir, hostVars, timeoutFor(60, suite.DefaultTimeout), opts.HostCommandTimeoutExtra)
	}

	return RunResult{Suite: suite.Name, WorkDir: workDir, Results: results}, nil
}

func selectTests(tests []TestCase, filter string) []TestCase {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return append([]TestCase(nil), tests...)
	}
	out := make([]TestCase, 0, len(tests))
	for _, test := range tests {
		if strings.Contains(strings.ToLower(test.Name), filter) {
			out = append(out, test)
		}
	}
	return out
}

func buildPullRequest(suite Suite, imageSource string, opts Options) client.PullImageRequest {
	if strings.TrimSpace(imageSource) != "" {
		if strings.HasPrefix(imageSource, "/containers/") {
			return client.PullImageRequest{
				SourceRef:       &client.ImageSource{Type: "cvmfs", Mirror: opts.Mirror, Repo: opts.Repo, Path: imageSource},
				CacheDir:        opts.CacheDir,
				Prefetch:        opts.Prefetch,
				PrefetchWorkers: opts.PrefetchWorkers,
			}
		}
		return client.PullImageRequest{Source: imageSource, CacheDir: opts.CacheDir, Prefetch: opts.Prefetch, PrefetchWorkers: opts.PrefetchWorkers}
	}
	container := strings.TrimSpace(suite.Container)
	if strings.HasPrefix(container, "docker-archive:") || strings.HasPrefix(container, "cvmfs://") || strings.Contains(container, "/cvmfs/") || strings.HasSuffix(strings.ToLower(container), ".simg") || strings.HasSuffix(strings.ToLower(container), ".sif") {
		return client.PullImageRequest{Source: container, CacheDir: opts.CacheDir, Prefetch: opts.Prefetch, PrefetchWorkers: opts.PrefetchWorkers}
	}
	container = strings.TrimSuffix(container, ".simg")
	return client.PullImageRequest{
		SourceRef:       &client.ImageSource{Type: "cvmfs", Mirror: opts.Mirror, Repo: opts.Repo, Path: "/containers/" + container},
		CacheDir:        opts.CacheDir,
		Prefetch:        opts.Prefetch,
		PrefetchWorkers: opts.PrefetchWorkers,
	}
}

func pullSourceText(req client.PullImageRequest) string {
	if req.Source != "" {
		return req.Source
	}
	if req.SourceRef != nil {
		if s, err := req.SourceString(); err == nil {
			return s
		}
	}
	return ""
}

func buildGuestVars(testData map[string]string) map[string]string {
	out := make(map[string]string, len(testData))
	for key, value := range testData {
		if path.IsAbs(value) {
			out[key] = value
		} else {
			out[key] = path.Join("/work", filepath.ToSlash(value))
		}
	}
	return out
}

func buildHostVars(workDir string, testData map[string]string) map[string]string {
	out := make(map[string]string, len(testData))
	for key, value := range testData {
		if filepath.IsAbs(value) {
			out[key] = value
		} else {
			out[key] = filepath.Join(workDir, filepath.FromSlash(value))
		}
	}
	return out
}

func substitute(text string, vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := text
	for _, key := range keys {
		out = strings.ReplaceAll(out, "${"+key+"}", vars[key])
		out = strings.ReplaceAll(out, "$"+key, vars[key])
	}
	return out
}

func applyEnvSetup(command, envSetup string) string {
	parts := []string{
		`export MPLBACKEND="${MPLBACKEND:-agg}"`,
		`export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-offscreen}"`,
		`export NO_AT_BRIDGE="${NO_AT_BRIDGE:-1}"`,
		`export KMP_AFFINITY="${KMP_AFFINITY:-disabled}"`,
		`export MCR_CACHE_ROOT="${MCR_CACHE_ROOT:-/tmp/cc-fulltest-mcr-cache}"`,
		`export MATLAB_PREFDIR="${MATLAB_PREFDIR:-/tmp/cc-fulltest-matlab-prefdir}"`,
		`mkdir -p "$MCR_CACHE_ROOT" "$MATLAB_PREFDIR" 2>/dev/null || true`,
	}
	if strings.TrimSpace(envSetup) != "" {
		parts = append(parts, strings.TrimSpace(envSetup))
	}
	if strings.TrimSpace(command) != "" {
		parts = append(parts, strings.Trim(command, "\n"))
	}
	return strings.Join(parts, "\n")
}

func runGuest(ctx context.Context, api API, vmID, command string, timeoutSeconds float64) (string, int, error) {
	req := client.RunRequest{
		ID:             vmID,
		Command:        []string{"bash", "-lc", command},
		WorkDir:        "/work",
		TimeoutSeconds: timeoutSeconds,
	}
	resp, err := api.RunIn(vmID, req)
	if err != nil {
		return resp.Output, resp.ExitCode, err
	}
	return resp.Output, resp.ExitCode, nil
}

func runHostScript(ctx context.Context, script, workDir string, vars map[string]string, timeoutSeconds float64, extra time.Duration) (string, int) {
	if extra <= 0 {
		extra = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds*float64(time.Second))+extra)
	defer cancel()
	command := substitute(script, vars)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && strings.TrimSpace(os.Getenv("CC_FULLTEST_HOST_SHELL")) == "" {
		cmd = exec.CommandContext(ctx, firstNonEmpty(os.Getenv("COMSPEC"), "cmd.exe"), "/d", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, firstNonEmpty(os.Getenv("CC_FULLTEST_HOST_SHELL"), "bash"), "-lc", command)
	}
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(output) + fmt.Sprintf("\n[fulltest] command timed out after %.1fs: %s\n", timeoutSeconds, command), 124
	}
	if err == nil {
		return string(output), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	return string(output) + err.Error(), 1
}

func timeoutFor(testTimeout, defaultTimeout int) float64 {
	if testTimeout > 0 {
		return float64(testTimeout)
	}
	if defaultTimeout > 0 {
		return float64(defaultTimeout)
	}
	return 120
}

func validateTest(output string, exitCode int, test TestCase, hostVars map[string]string) string {
	if !test.IgnoreExitCode && exitCode != test.ExpectedExitCode {
		return strings.TrimSpace(fmt.Sprintf("exit code %d, want %d\n%s", exitCode, test.ExpectedExitCode, output))
	}
	for _, fragment := range test.ExpectedOutputContains {
		if fragment != "" && !strings.Contains(output, fragment) {
			return fmt.Sprintf("missing output fragment %q", fragment)
		}
	}
	for _, validation := range test.Validate {
		for kind, arg := range validation {
			switch kind {
			case "output_exists":
				filePath := substitute(fmt.Sprint(arg), hostVars)
				if _, err := os.Stat(filePath); err != nil {
					return "missing output " + filePath
				}
			case "same_dimensions":
				items, ok := arg.([]any)
				if !ok || len(items) != 2 {
					return "invalid same_dimensions validation"
				}
				left := substitute(fmt.Sprint(items[0]), hostVars)
				right := substitute(fmt.Sprint(items[1]), hostVars)
				leftHeader, err := nifti.ReadHeader(left)
				if err != nil {
					return err.Error()
				}
				rightHeader, err := nifti.ReadHeader(right)
				if err != nil {
					return err.Error()
				}
				if !sameInts(nifti.Shape(leftHeader), nifti.Shape(rightHeader)) {
					return fmt.Sprintf("dimension mismatch %s vs %s", left, right)
				}
			case "is_3d":
				filePath := substitute(fmt.Sprint(arg), hostVars)
				header, err := nifti.ReadHeader(filePath)
				if err != nil {
					return err.Error()
				}
				if !nifti.Is3D(header) {
					return filePath + " is not 3D"
				}
			}
		}
	}
	return ""
}

func sameInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func isTimeoutResult(output string, exitCode int) bool {
	return exitCode == 124 && (strings.Contains(output, "[fulltest] command timed out after") || strings.Contains(output, "[ccvm] command timed out after"))
}

func hasFailedDependency(test TestCase, failed map[string]bool) bool {
	for _, dep := range test.DependsOn {
		if failed[dep] {
			return true
		}
	}
	return false
}

func prepareRequiredFiles(ctx context.Context, workDir string, required []RequiredDataset) error {
	httpClient := &http.Client{Timeout: 120 * time.Second}
	for _, dataset := range required {
		for _, relative := range dataset.Files {
			dst := filepath.Join(workDir, dataset.Dataset, filepath.FromSlash(relative))
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			url := strings.TrimRight(DefaultOpenNeuroBase, "/") + "/" + dataset.Dataset + "/" + relative
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				return err
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				resp.Body.Close()
				return fmt.Errorf("download %s: HTTP %s", url, resp.Status)
			}
			tmp := dst + ".tmp"
			file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				resp.Body.Close()
				return err
			}
			_, copyErr := io.Copy(file, resp.Body)
			closeErr := file.Close()
			resp.Body.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Rename(tmp, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildDockerArchive(ctx context.Context, workDir string, opts Options) (string, string, error) {
	dockerfile, err := filepath.Abs(opts.Dockerfile)
	if err != nil {
		return "", "", err
	}
	contextDir := opts.DockerContext
	if strings.TrimSpace(contextDir) == "" {
		contextDir = filepath.Dir(dockerfile)
	}
	contextDir, err = filepath.Abs(contextDir)
	if err != nil {
		return "", "", err
	}
	tag := strings.TrimSpace(opts.DockerTag)
	if tag == "" {
		tag = "cc-fulltest:" + imageCacheName(dockerfile)[:12]
	}
	binary := firstNonEmpty(strings.TrimSpace(opts.DockerBinary), "docker")
	build := exec.CommandContext(ctx, binary, "build", "-t", tag, "-f", dockerfile, contextDir)
	build.Stdout = opts.Progress
	build.Stderr = opts.Progress
	if err := build.Run(); err != nil {
		return "", "", fmt.Errorf("docker build: %w", err)
	}
	archive := filepath.Join(workDir, strings.ReplaceAll(imageCacheName(tag), ":", "-")+".docker.tar")
	save := exec.CommandContext(ctx, binary, "save", "-o", archive, tag)
	save.Stdout = opts.Progress
	save.Stderr = opts.Progress
	if err := save.Run(); err != nil {
		return "", "", fmt.Errorf("docker save: %w", err)
	}
	return archive, tag, nil
}

func imageCacheName(seed string) string {
	sum := sha1.Sum([]byte(seed))
	return "fulltest-" + hex.EncodeToString(sum[:8])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func progressReporter(w io.Writer, imageName string) func(client.ProgressEvent) error {
	if w == nil {
		return nil
	}
	return func(event client.ProgressEvent) error {
		status := event.Status
		if status == "" {
			status = "preparing"
		}
		artifact := firstNonEmpty(event.Artifact, imageName)
		parts := []string{"[fulltest] pull", artifact, status}
		if event.Blob != "" {
			parts = append(parts, event.Blob)
		}
		if event.Error != "" {
			parts = append(parts, event.Error)
		}
		_, err := fmt.Fprintln(w, strings.Join(parts, " | "))
		return err
	}
}

func bootReporter(w io.Writer) func(client.BootEvent) error {
	if w == nil {
		return nil
	}
	return func(event client.BootEvent) error {
		switch event.Kind {
		case "status":
			if event.Message != "" {
				_, _ = fmt.Fprintln(w, "[fulltest] boot | "+event.Message)
			}
		case "ready":
			_, _ = fmt.Fprintln(w, "[fulltest] boot | ready")
		case "error":
			_, _ = fmt.Fprintln(w, "[fulltest] boot | error | "+event.Error)
		}
		return nil
	}
}

func progressf(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}

func Summary(result RunResult, w io.Writer) int {
	passed, failed, skipped := 0, 0, 0
	for _, item := range result.Results {
		switch {
		case item.Passed:
			passed++
			fmt.Fprintf(w, "PASS %s (%.2fs)\n", item.Name, item.DurationSeconds)
		case item.Skipped:
			skipped++
			fmt.Fprintf(w, "SKIP %s: %s\n", item.Name, item.Message)
		default:
			failed++
			fmt.Fprintf(w, "FAIL %s: %s\n", item.Name, item.Message)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Suite: %s\n", result.Suite)
	fmt.Fprintf(w, "Work dir: %s\n", result.WorkDir)
	fmt.Fprintf(w, "Passed: %d\n", passed)
	fmt.Fprintf(w, "Failed: %d\n", failed)
	fmt.Fprintf(w, "Skipped: %d\n", skipped)
	if failed > 0 {
		return 1
	}
	return 0
}

func ParsePositiveInt(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
