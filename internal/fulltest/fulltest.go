package fulltest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"j5.nz/cc/client"
	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/nifti"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

type Runner struct {
	Kernel *alpine.Manager
	Images *oci.Store
	VMs    *vm.Manager
	HTTP   *http.Client
}

type Options struct {
	ImageSource string
	ImageName   string
	WorkDir     string
	KeepVM      bool
	Filter      string
}

type Suite struct {
	Name          string            `yaml:"name"`
	Container     string            `yaml:"container"`
	RequiredFiles []RequiredDataset `yaml:"required_files"`
	TestData      map[string]string `yaml:"test_data"`
	Setup         SuiteScript       `yaml:"setup"`
	Cleanup       SuiteScript       `yaml:"cleanup"`
	Tests         []Test            `yaml:"tests"`
	DefaultTimout int               `yaml:"default_timeout"`
}

type RequiredDataset struct {
	Dataset string   `yaml:"dataset"`
	Files   []string `yaml:"files"`
}

type SuiteScript struct {
	Script string `yaml:"script"`
}

type Test struct {
	Name                   string           `yaml:"name"`
	Command                string           `yaml:"command"`
	Timeout                int              `yaml:"timeout"`
	DependsOn              any              `yaml:"depends_on"`
	ExpectedOutputContains any              `yaml:"expected_output_contains"`
	ExpectedExitCode       *int             `yaml:"expected_exit_code"`
	Validate               []map[string]any `yaml:"validate"`
}

type TestResult struct {
	Name     string
	Passed   bool
	Skipped  bool
	Duration time.Duration
	Message  string
}

type Result struct {
	Suite   string
	WorkDir string
	Results []TestResult
}

func LoadSuite(path string) (*Suite, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var suite Suite
	if err := yaml.Unmarshal(buf, &suite); err != nil {
		return nil, err
	}
	if suite.Name == "" {
		suite.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return &suite, nil
}

func (r *Runner) Run(ctx context.Context, suitePath string, opts Options) (*Result, error) {
	if r == nil || r.Kernel == nil || r.Images == nil || r.VMs == nil {
		return nil, fmt.Errorf("runner is not configured")
	}
	suite, err := LoadSuite(suitePath)
	if err != nil {
		return nil, err
	}
	if r.HTTP == nil {
		r.HTTP = http.DefaultClient
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "ccx3-fulltest-*")
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}

	if err := prepareRequiredFiles(ctx, r.HTTP, workDir, suite.RequiredFiles); err != nil {
		return nil, err
	}

	imageSource, cleanupSource, err := normalizeImageSource(ctx, r.HTTP, opts.ImageSource)
	if err != nil {
		return nil, err
	}
	if cleanupSource != nil {
		defer cleanupSource()
	}
	if imageSource == "" {
		imageSource, err = resolveDefaultImageSource(suite.Container)
		if err != nil {
			return nil, err
		}
	}

	imageName := opts.ImageName
	if imageName == "" {
		imageName = imageCacheName(imageSource)
	}
	if _, err := r.Images.Pull(ctx, imageName, imageSource); err != nil {
		return nil, err
	}
	if err := r.Kernel.Ensure(ctx); err != nil {
		return nil, err
	}
	shares := []client.ShareMount{{
		Source:   workDir,
		Mount:    "/work",
		Writable: true,
	}}

	if opts.KeepVM {
		if _, err := r.VMs.Start(ctx, client.CreateInstanceRequest{
			Image:  imageName,
			Shares: shares,
		}); err != nil {
			return nil, err
		}
		defer r.VMs.Shutdown(context.Background())
	}

	guestVars := buildGuestVars(workDir, suite.TestData)
	hostVars := buildHostVars(workDir, suite.TestData)

	if script := strings.TrimSpace(suite.Setup.Script); script != "" {
		if _, _, err := runCommand(ctx, r.VMs, imageName, shares, "/work", substituteVariables(script, guestVars), timeoutFor(120, suite.DefaultTimout)); err != nil {
			return nil, fmt.Errorf("setup failed: %w", err)
		}
	}

	res := &Result{Suite: suite.Name, WorkDir: workDir}
	failed := map[string]bool{}
	for _, test := range suite.Tests {
		if opts.Filter != "" && !strings.Contains(strings.ToLower(test.Name), strings.ToLower(opts.Filter)) {
			continue
		}
		deps := toStringSlice(test.DependsOn)
		skipped := false
		for _, dep := range deps {
			if failed[dep] {
				res.Results = append(res.Results, TestResult{Name: test.Name, Skipped: true, Message: "dependency failed"})
				failed[test.Name] = true
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		start := time.Now()
		output, exitCode, err := runCommand(ctx, r.VMs, imageName, shares, "/work", substituteVariables(test.Command, guestVars), timeoutFor(test.Timeout, suite.DefaultTimout))
		result := TestResult{Name: test.Name, Duration: time.Since(start)}
		if err != nil {
			result.Message = err.Error()
			failed[test.Name] = true
			res.Results = append(res.Results, result)
			continue
		}
		if expected := expectedExitCode(test.ExpectedExitCode); exitCode != expected {
			result.Message = fmt.Sprintf("exit code %d, want %d", exitCode, expected)
			failed[test.Name] = true
			res.Results = append(res.Results, result)
			continue
		}
		if want := toStringSlice(test.ExpectedOutputContains); len(want) > 0 {
			missing := ""
			for _, frag := range want {
				if frag != "" && !strings.Contains(output, frag) {
					missing = frag
					break
				}
			}
			if missing != "" {
				result.Message = fmt.Sprintf("missing output fragment %q", missing)
				failed[test.Name] = true
				res.Results = append(res.Results, result)
				continue
			}
		}
		if msg := validateOutputs(hostVars, test.Validate); msg != "" {
			result.Message = msg
			failed[test.Name] = true
			res.Results = append(res.Results, result)
			continue
		}
		result.Passed = true
		result.Message = "ok"
		res.Results = append(res.Results, result)
	}

	if script := strings.TrimSpace(suite.Cleanup.Script); script != "" {
		_, _, _ = runCommand(context.Background(), r.VMs, imageName, shares, "/work", substituteVariables(script, guestVars), timeoutFor(60, suite.DefaultTimout))
	}

	return res, nil
}

func runCommand(parent context.Context, mgr *vm.Manager, imageName string, shares []client.ShareMount, workdir, command string, timeout time.Duration) (string, int, error) {
	ctx := parent
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
		defer cancel()
	}
	resp, err := mgr.Run(ctx, client.RunRequest{
		Image:   imageName,
		Shares:  append([]client.ShareMount(nil), shares...),
		Command: []string{"/bin/sh", "-lc", command},
		WorkDir: workdir,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.Output, resp.ExitCode, nil
}

func timeoutFor(testTimeout, defaultTimeout int) time.Duration {
	if testTimeout > 0 {
		return time.Duration(testTimeout) * time.Second
	}
	if defaultTimeout > 0 {
		return time.Duration(defaultTimeout) * time.Second
	}
	return 120 * time.Second
}

func buildGuestVars(workDir string, data map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range data {
		if key == "output_dir" {
			out[key] = "/work/" + strings.TrimPrefix(filepath.ToSlash(value), "/")
			continue
		}
		out[key] = "/work/" + strings.TrimPrefix(filepath.ToSlash(value), "/")
	}
	return out
}

func buildHostVars(workDir string, data map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range data {
		out[key] = filepath.Join(workDir, filepath.FromSlash(value))
	}
	return out
}

func substituteVariables(text string, vars map[string]string) string {
	result := text
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		result = strings.ReplaceAll(result, "${"+key+"}", value)
		result = strings.ReplaceAll(result, "$"+key, value)
	}
	return result
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), val...)
	default:
		return nil
	}
}

func expectedExitCode(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func validateOutputs(vars map[string]string, validations []map[string]any) string {
	for _, validation := range validations {
		for kind, arg := range validation {
			switch kind {
			case "output_exists":
				path := substituteVariables(fmt.Sprint(arg), vars)
				if _, err := os.Stat(path); err != nil {
					return fmt.Sprintf("missing output %s", path)
				}
			case "same_dimensions":
				list, ok := arg.([]any)
				if !ok || len(list) != 2 {
					return "invalid same_dimensions validation"
				}
				a := substituteVariables(fmt.Sprint(list[0]), vars)
				b := substituteVariables(fmt.Sprint(list[1]), vars)
				ha, err := nifti.ReadHeader(a)
				if err != nil {
					return fmt.Sprintf("read header %s: %v", a, err)
				}
				hb, err := nifti.ReadHeader(b)
				if err != nil {
					return fmt.Sprintf("read header %s: %v", b, err)
				}
				sa := nifti.Shape(ha)
				sb := nifti.Shape(hb)
				if len(sa) != len(sb) {
					return fmt.Sprintf("dimension mismatch %v vs %v", sa, sb)
				}
				for i := range sa {
					if sa[i] != sb[i] {
						return fmt.Sprintf("dimension mismatch %v vs %v", sa, sb)
					}
				}
			case "is_3d":
				path := substituteVariables(fmt.Sprint(arg), vars)
				hdr, err := nifti.ReadHeader(path)
				if err != nil {
					return fmt.Sprintf("read header %s: %v", path, err)
				}
				if !nifti.Is3D(hdr) {
					return fmt.Sprintf("%s is not 3D", path)
				}
			}
		}
	}
	return ""
}

func prepareRequiredFiles(ctx context.Context, httpClient *http.Client, workDir string, required []RequiredDataset) error {
	for _, entry := range required {
		for _, file := range entry.Files {
			hostPath := filepath.Join(workDir, filepath.FromSlash(entry.Dataset), filepath.FromSlash(file))
			if _, err := os.Stat(hostPath); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
				return err
			}
			url := fmt.Sprintf("https://s3.amazonaws.com/openneuro.org/%s/%s", entry.Dataset, file)
			if err := downloadFile(ctx, httpClient, url, hostPath); err != nil {
				return fmt.Errorf("download %s: %w", url, err)
			}
		}
	}
	return nil
}

func downloadFile(ctx context.Context, httpClient *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func normalizeImageSource(ctx context.Context, httpClient *http.Client, source string) (string, func(), error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", nil, nil
	}
	if strings.HasPrefix(source, "/cvmfs/") {
		return source, nil, nil
	}
	if strings.HasPrefix(source, "cvmfs://") || strings.Contains(source, "/cvmfs/") {
		client := &intcvmfs.Client{HTTPClient: httpClient}
		data, err := client.ReadFile(source)
		if err != nil {
			return "", nil, err
		}
		tmp, err := os.CreateTemp("", "ccx3-cvmfs-*.simg")
		if err != nil {
			return "", nil, err
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return "", nil, err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmp.Name())
			return "", nil, err
		}
		return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
	}
	return source, nil, nil
}

func resolveDefaultImageSource(container string) (string, error) {
	if container == "" {
		return "", fmt.Errorf("image source is required")
	}
	local := filepath.Join("local", container)
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	return "", fmt.Errorf("could not resolve default image source for %q", container)
}

func imageCacheName(source string) string {
	sum := sha1.Sum([]byte(source))
	return "fulltest-" + hex.EncodeToString(sum[:8])
}
