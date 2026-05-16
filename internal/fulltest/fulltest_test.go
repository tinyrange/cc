package fulltest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestLoadSuiteParsesFulltestFormat(t *testing.T) {
	recipe := writeRecipe(t, `
name: demo
container: demo_1.0.simg
default_timout: 17
env_setup: export A=B
test_data:
  rel: data/input.nii.gz
setup:
  host_script: echo host
  script: echo guest
cleanup:
  host_script: echo clean-host
  script: echo clean-guest
tests:
  - name: one
    command: echo ok
    depends_on: setup
    expected_output_contains: ok
  - name: two
    command: false
    expected_exit_code: 1
    ignore_exit_code: true
`)

	suite, err := LoadSuite(recipe)
	if err != nil {
		t.Fatalf("LoadSuite() error = %v", err)
	}
	if suite.Name != "demo" || suite.Container != "demo_1.0.simg" || suite.DefaultTimeout != 17 {
		t.Fatalf("suite = %#v", suite)
	}
	if suite.Setup.HostScript != "echo host" || suite.Cleanup.Script != "echo clean-guest" {
		t.Fatalf("scripts not parsed: %#v %#v", suite.Setup, suite.Cleanup)
	}
	if len(suite.Tests) != 2 {
		t.Fatalf("tests = %d, want 2", len(suite.Tests))
	}
	if got := suite.Tests[0].ExpectedOutputContains; len(got) != 1 || got[0] != "ok" {
		t.Fatalf("ExpectedOutputContains = %#v", got)
	}
	if got := suite.Tests[0].DependsOn; len(got) != 1 || got[0] != "setup" {
		t.Fatalf("DependsOn = %#v", got)
	}
	if !suite.Tests[1].IgnoreExitCode || suite.Tests[1].ExpectedExitCode != 1 {
		t.Fatalf("test two = %#v", suite.Tests[1])
	}
}

func TestRunPullsStartsAndRunsSuite(t *testing.T) {
	recipe := writeRecipe(t, `
name: demo
container: demo_1.0.simg
default_timeout: 9
test_data:
  input: data/input.txt
  scratch: /tmp/demo
setup:
  script: echo setup $input $scratch
cleanup:
  script: echo cleanup
tests:
  - name: pass
    command: echo hello $input
    expected_output_contains: hello /work/data/input.txt
  - name: skipped
    command: echo never
    depends_on: missing
`)
	api := &fakeAPI{
		run: func(req client.RunRequest) client.ExecResponse {
			return client.ExecResponse{ExitCode: 0, Output: strings.Join(req.Command, " ")}
		},
	}

	result, err := Run(context.Background(), api, Options{
		Recipe:      recipe,
		WorkDir:     t.TempDir(),
		MemoryMB:    512,
		CPUs:        2,
		Progress:    nil,
		Filter:      "",
		ImageName:   "demo-image",
		ImageSource: "/containers/demo",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if api.pullName != "demo-image" {
		t.Fatalf("pullName = %q", api.pullName)
	}
	if api.pullReq.SourceRef == nil || api.pullReq.SourceRef.Type != "cvmfs" || api.pullReq.SourceRef.Path != "/containers/demo" {
		t.Fatalf("pullReq = %#v", api.pullReq)
	}
	if api.startReq.Image != "demo-image" || api.startReq.MemoryMB != 512 || api.startReq.CPUs != 2 {
		t.Fatalf("startReq = %#v", api.startReq)
	}
	if len(api.startReq.Shares) != 1 || api.startReq.Shares[0].Mount != "/work" || !api.startReq.Shares[0].Writable {
		t.Fatalf("shares = %#v", api.startReq.Shares)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(result.Results))
	}
	if !result.Results[0].Passed {
		t.Fatalf("first result = %#v", result.Results[0])
	}
	if !result.Results[1].Passed {
		t.Fatalf("second result = %#v", result.Results[1])
	}
	if api.shutdownID == "" {
		t.Fatal("shutdown was not called")
	}
}

func TestRunSkipsFailedDependencies(t *testing.T) {
	recipe := writeRecipe(t, `
name: deps
container: deps.simg
tests:
  - name: fail
    command: echo bad
    expected_output_contains: missing
  - name: child
    command: echo child
    depends_on: fail
`)
	api := &fakeAPI{run: func(req client.RunRequest) client.ExecResponse {
		return client.ExecResponse{ExitCode: 0, Output: "bad"}
	}}
	result, err := Run(context.Background(), api, Options{Recipe: recipe, WorkDir: t.TempDir(), MemoryMB: 512, CPUs: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Results) != 2 || result.Results[0].Passed || !result.Results[1].Skipped {
		t.Fatalf("results = %#v", result.Results)
	}
	if api.runCount != 1 {
		t.Fatalf("runCount = %d, want 1", api.runCount)
	}
}

func TestBuildPullRequestSupportsDockerArchive(t *testing.T) {
	req := buildPullRequest(Suite{Container: "ignored.simg"}, "docker-archive:C:/tmp/tool.tar#tool:latest", Options{})
	if req.Source != "docker-archive:C:/tmp/tool.tar#tool:latest" {
		t.Fatalf("Source = %q", req.Source)
	}
}

func writeRecipe(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fulltest.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

type fakeAPI struct {
	pullName   string
	pullReq    client.PullImageRequest
	startID    string
	startReq   client.CreateInstanceRequest
	shutdownID string
	runCount   int
	run        func(client.RunRequest) client.ExecResponse
}

func (f *fakeAPI) PullImageStream(name string, req client.PullImageRequest, _ func(client.ProgressEvent) error) error {
	f.pullName = name
	f.pullReq = req
	return nil
}

func (f *fakeAPI) CreateInstanceStreamWithID(id string, req client.CreateInstanceRequest, _ func(client.BootEvent) error) (client.InstanceState, error) {
	f.startID = id
	f.startReq = req
	return client.InstanceState{ID: id, Status: "running", Image: req.Image}, nil
}

func (f *fakeAPI) RunIn(_ string, req client.RunRequest) (client.ExecResponse, error) {
	f.runCount++
	if f.run != nil {
		return f.run(req), nil
	}
	return client.ExecResponse{ExitCode: 0}, nil
}

func (f *fakeAPI) ShutdownInstanceWithID(id string) error {
	f.shutdownID = id
	return nil
}
