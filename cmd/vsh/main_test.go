package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestSplitShellFields(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: `@image alpine`, want: []string{"@image", "alpine"}},
		{input: `cd "two words"`, want: []string{"cd", "two words"}},
		{input: `@vm use 'work vm'`, want: []string{"@vm", "use", "work vm"}},
		{input: `cd a\ b`, want: []string{"cd", "a b"}},
	}
	for _, tt := range tests {
		got, err := splitShellFields(tt.input)
		if err != nil {
			t.Fatalf("splitShellFields(%q) error = %v", tt.input, err)
		}
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Fatalf("splitShellFields(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
	}
}

func TestSplitShellFieldsErrors(t *testing.T) {
	for _, input := range []string{`"unterminated`, `abc\`} {
		if _, err := splitShellFields(input); err == nil {
			t.Fatalf("splitShellFields(%q) error = nil, want error", input)
		}
	}
}

func TestParseCD(t *testing.T) {
	target, ok, err := parseCD(`cd "hello world"`)
	if err != nil {
		t.Fatalf("parseCD() error = %v", err)
	}
	if !ok || target != "hello world" {
		t.Fatalf("parseCD() = %q, %v; want hello world, true", target, ok)
	}

	if _, ok, err := parseCD(`echo cd`); err != nil || ok {
		t.Fatalf("parseCD(non-cd) = _, %v, %v; want false, nil", ok, err)
	}

	if _, ok, err := parseCD(`cd one two`); !ok || err == nil {
		t.Fatalf("parseCD(extra args) = _, %v, %v; want true and error", ok, err)
	}
}

func TestPromptShowsModeAndImage(t *testing.T) {
	sh := &shellState{
		context: commandContext{Mode: modeVM, VMID: "alpha", Image: "alpine"},
		hostCWD: "/tmp/work",
	}
	if got := sh.prompt(); got != colorGreen+"➜"+colorReset+"  "+colorCyan+"work"+colorReset+" "+colorMagenta+"vm:"+colorReset+colorYellow+"(alpine:alpha)"+colorReset+" " {
		t.Fatalf("prompt() = %q", got)
	}
}

func TestHostPromptMatchesArrowStyle(t *testing.T) {
	sh := &shellState{
		context: commandContext{Mode: modeHost, VMID: "default"},
		hostCWD: "/tmp/work",
	}
	if got := sh.prompt(); got != colorGreen+"➜"+colorReset+"  "+colorCyan+"work"+colorReset+" "+colorBlue+"host"+colorReset+" " {
		t.Fatalf("prompt() = %q", got)
	}
}

func TestPersistentHostShellPreservesState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("host pty smoke test is unix-only")
	}
	dir := t.TempDir()
	session, err := startPersistentHostShell(dir, os.Environ(), 80, 24, "")
	if err != nil {
		t.Fatalf("startPersistentHostShell() error = %v", err)
	}
	defer session.close()

	var stdout, stderr bytes.Buffer
	if err := session.run("printf 'first-host\\n'", &stdout, &stderr); err != nil {
		t.Fatalf("run(first) error = %v; stderr=%q", err, stderr.String())
	}
	if got := normalizeTerminalOutput(stdout.String()); got != "first-host\n" {
		t.Fatalf("first output = %q, want first-host", got)
	}

	stdout.Reset()
	stderr.Reset()
	if err := session.run("alias hp='echo host-persist'", &stdout, &stderr); err != nil {
		t.Fatalf("run(alias) error = %v; stderr=%q", err, stderr.String())
	}
	if err := session.run("hp", &stdout, &stderr); err != nil {
		t.Fatalf("run(alias use) error = %v; stderr=%q", err, stderr.String())
	}
	if got := normalizeTerminalOutput(stdout.String()); !strings.Contains(got, "host-persist\n") {
		t.Fatalf("alias output = %q, want host-persist", got)
	}

	stdout.Reset()
	stderr.Reset()
	if err := session.run("cd /tmp", &stdout, &stderr); err != nil {
		t.Fatalf("run(cd) error = %v; stderr=%q", err, stderr.String())
	}
	if got := session.cwd(); got != "/tmp" {
		t.Fatalf("cwd after cd = %q, want /tmp", got)
	}
}

func normalizeTerminalOutput(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func TestParseAtLineTargetOptionsAndCommand(t *testing.T) {
	got, err := parseAtLine(`@ubuntu:24.04 --vm work --memory 2g --cpus=4 pytest -q --maxfail=1`)
	if err != nil {
		t.Fatalf("parseAtLine() error = %v", err)
	}
	if got.Target != "ubuntu:24.04" || got.Options.VMID != "work" || got.Options.MemoryMB != 2048 || got.Options.CPUs != 4 {
		t.Fatalf("parseAtLine() = %#v", got)
	}
	if got.Command != "pytest -q --maxfail=1" {
		t.Fatalf("command = %q", got.Command)
	}
}

func TestParseAtLineCurrentContextOptions(t *testing.T) {
	got, err := parseAtLine(`@ --vm work --cwd /src`)
	if err != nil {
		t.Fatalf("parseAtLine() error = %v", err)
	}
	if got.Target != "" || got.Options.VMID != "work" || got.Options.CWD != "/src" || got.Command != "" {
		t.Fatalf("parseAtLine() = %#v", got)
	}
}

func TestParseAtLineSudoOption(t *testing.T) {
	got, err := parseAtLine(`@ --sudo apt update`)
	if err != nil {
		t.Fatalf("parseAtLine() error = %v", err)
	}
	if !got.Options.Sudo || got.Command != "apt update" {
		t.Fatalf("parseAtLine() = %#v", got)
	}
}

func TestBareOCISelectsCurrentContextAndPreparesImage(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "default", Status: "stopped"}}
	sh := &shellState{api: api, context: defaultContext("default", ""), hostCWD: t.TempDir()}
	if err := sh.eval(`@alpine`, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("eval(@alpine) error = %v", err)
	}
	if sh.context.Mode != modeVM || sh.context.Image != "alpine" {
		t.Fatalf("context = %#v, want vm/alpine", sh.context)
	}
	if len(api.starts) != 0 {
		t.Fatalf("starts = %d, want 0", len(api.starts))
	}
}

func TestBareOCIPullsMissingImageWithoutBooting(t *testing.T) {
	api := &fakeVSHAPI{
		status:      client.InstanceState{ID: "default", Status: "stopped"},
		missingImgs: map[string]bool{"alpine": true},
		pullEvents: []client.ProgressEvent{{
			Status:             "downloading",
			Artifact:           "alpine",
			Blob:               "rootfs",
			BytesDownloaded:    1024,
			BytesTotal:         2048,
			FilesDownloaded:    1,
			FilesTotal:         2,
			RateBytesPerSecond: 512,
			ETASeconds:         2,
		}},
	}
	sh := &shellState{api: api, context: defaultContext("default", ""), hostCWD: t.TempDir()}
	var stderr bytes.Buffer
	if err := sh.eval(`@alpine`, &bytes.Buffer{}, &stderr); err != nil {
		t.Fatalf("eval(@alpine) error = %v", err)
	}
	if len(api.pulls) != 1 || api.pulls[0].name != "alpine" {
		t.Fatalf("pulls = %#v, want alpine", api.pulls)
	}
	if len(api.starts) != 0 {
		t.Fatalf("starts = %d, want 0", len(api.starts))
	}
	if sh.context.Mode != modeVM || sh.context.Image != "alpine" {
		t.Fatalf("context = %#v, want vm/alpine", sh.context)
	}
	gotStatus := stderr.String()
	for _, want := range []string{
		"Pull alpine\n",
		"  status: downloading\n",
		"  blob: rootfs\n",
		"  bytes: 1.0 KB / 2.0 KB (50.0%)\n",
		"  files: 1 / 2 (50.0%)\n",
		"  rate: 512 B/s\n",
		"  eta: 2s\n",
	} {
		if !strings.Contains(gotStatus, want) {
			t.Fatalf("pull status = %q, missing %q", gotStatus, want)
		}
	}
}

func TestScriptSendsLinesThroughCurrentContext(t *testing.T) {
	dir := t.TempDir()
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: defaultContext("default", ""), hostCWD: dir}
	script := strings.NewReader(`
# ignored
@alpine --vm work --memory 512
echo hello --flag
@host
`)
	if err := sh.runScript(script, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 1 {
		t.Fatalf("streams = %d, want 1", len(api.streams))
	}
	run := api.streams[0]
	if run.id != "work" || run.req.Image != "alpine" || run.req.MemoryMB != 512 {
		t.Fatalf("run = %#v", run)
	}
	if run.req.Network == nil || !run.req.Network.Enabled || !run.req.Network.AllowInternet {
		t.Fatalf("network = %#v, want enabled internet", run.req.Network)
	}
	if run.req.User != defaultGuestUser {
		t.Fatalf("user = %q, want %q", run.req.User, defaultGuestUser)
	}
	if len(run.req.Command) != 3 || run.req.Command[0] != "sh" || run.req.Command[1] != "-lc" || !strings.HasSuffix(run.req.Command[2], "echo hello --flag") {
		t.Fatalf("command = %#v", run.req.Command)
	}
	for _, want := range []string{"HOME=/home/cc", "USER=cc", "LOGNAME=cc"} {
		if !envContains(run.req.Env, want) {
			t.Fatalf("env = %#v, missing %q", run.req.Env, want)
		}
	}
	if len(run.req.Shares) != 1 {
		t.Fatalf("shares = %#v", run.req.Shares)
	}
	if run.req.Shares[0].Source != string(filepath.Separator) || run.req.Shares[0].Mount != guestHostMount {
		t.Fatalf("host share = %#v, want root at /host", run.req.Shares[0])
	}
	if !run.req.Shares[0].MapOwner || run.req.Shares[0].OwnerUID != defaultGuestUID || run.req.Shares[0].OwnerGID != defaultGuestGID {
		t.Fatalf("host share owner = %#v, want mapped default guest user", run.req.Shares[0])
	}
	wantWorkDir := path.Join(guestHostMount, strings.TrimPrefix(filepath.ToSlash(dir), "/"))
	if run.req.WorkDir != wantWorkDir {
		t.Fatalf("workdir = %q, want %q", run.req.WorkDir, wantWorkDir)
	}
	if sh.context.Mode != modeHost {
		t.Fatalf("mode = %q, want host", sh.context.Mode)
	}
}

func TestGuestRunsAsRootWithSudoOption(t *testing.T) {
	dir := t.TempDir()
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true}, hostCWD: dir}
	if err := sh.runScript(strings.NewReader("@ --sudo id -u\n@sudo id -u\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(api.streams))
	}
	for _, run := range api.streams {
		if run.req.User != "root" {
			t.Fatalf("user = %q, want root", run.req.User)
		}
		for _, want := range []string{"HOME=/root", "USER=root", "LOGNAME=root"} {
			if !envContains(run.req.Env, want) {
				t.Fatalf("env = %#v, missing %q", run.req.Env, want)
			}
		}
	}
}

func TestGuestRunRequestsUseStreamingPath(t *testing.T) {
	dir := t.TempDir()
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine"}, hostCWD: dir}
	if err := sh.runScript(strings.NewReader("ls\nuname -a\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(api.streams))
	}
	for _, run := range api.streams {
		if run.id != "work" || run.req.Image != "alpine" {
			t.Fatalf("stream = %#v", run)
		}
	}
}

func TestNoNetworkOptionDisablesGuestNetwork(t *testing.T) {
	dir := t.TempDir()
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeHost, VMID: "work", Network: true}, hostCWD: dir}
	if err := sh.runScript(strings.NewReader("@alpine --no-network echo hi\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 1 {
		t.Fatalf("streams = %d, want 1", len(api.streams))
	}
	if api.streams[0].req.Network != nil {
		t.Fatalf("network = %#v, want nil", api.streams[0].req.Network)
	}
}

func TestGuestBootUsesContextNetwork(t *testing.T) {
	dir := t.TempDir()
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "stopped"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true}, hostCWD: dir}
	if err := sh.runScript(strings.NewReader("echo hi\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(api.starts))
	}
	if api.starts[0].req.Network == nil || !api.starts[0].req.Network.Enabled || !api.starts[0].req.Network.AllowInternet {
		t.Fatalf("start network = %#v, want enabled internet", api.starts[0].req.Network)
	}
}

func TestStreamGuestRunWritesEventsAndExit(t *testing.T) {
	api := &fakeVSHAPI{
		streamEvents: []client.ExecEvent{
			{Kind: "stdout", Data: []byte("Linux\n")},
			{Kind: "stderr", Output: "warn\n"},
			{Kind: "exit", ExitCode: 0},
		},
	}
	sh := &shellState{api: api}
	var stdout, stderr bytes.Buffer
	err := sh.streamGuestRun("work", client.RunRequest{Image: "ubuntu", Command: []string{"uname", "-a"}}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("streamGuestRun() error = %v", err)
	}
	if stdout.String() != "Linux\n" || stderr.String() != "warn\n" || sh.lastCode != 0 {
		t.Fatalf("stdout=%q stderr=%q code=%d", stdout.String(), stderr.String(), sh.lastCode)
	}
}

func TestStreamGuestRunRecordsNonzeroExitWithoutLog(t *testing.T) {
	api := &fakeVSHAPI{
		streamEvents: []client.ExecEvent{
			{Kind: "exit", ExitCode: 42},
		},
	}
	sh := &shellState{api: api}
	var stdout, stderr bytes.Buffer
	err := sh.streamGuestRun("work", client.RunRequest{Image: "ubuntu", Command: []string{"false"}}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("streamGuestRun() error = %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" || sh.lastCode != 42 {
		t.Fatalf("stdout=%q stderr=%q code=%d", stdout.String(), stderr.String(), sh.lastCode)
	}
}

func TestScriptStopsOnErrors(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "default", Status: "running"}}
	sh := &shellState{api: api, context: defaultContext("default", ""), hostCWD: t.TempDir()}
	err := sh.runScript(strings.NewReader("@ --bogus\n@alpine echo nope\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown vsh option") {
		t.Fatalf("runScript() error = %v, want unknown option", err)
	}
	if len(api.streams) != 0 {
		t.Fatalf("streams = %d, want 0", len(api.streams))
	}
}

func TestLoopRequiresInteractiveTerminal(t *testing.T) {
	sh := &shellState{context: defaultContext("default", ""), hostCWD: t.TempDir()}
	err := sh.loop(strings.NewReader("echo nope\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("loop() error = %v, want interactive terminal error", err)
	}
}

func TestShouldSaveHistory(t *testing.T) {
	for _, line := range []string{"ls", "  @ubuntu echo hi  "} {
		if !shouldSaveHistory(line) {
			t.Fatalf("shouldSaveHistory(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"", "   ", "# comment", "  # comment"} {
		if shouldSaveHistory(line) {
			t.Fatalf("shouldSaveHistory(%q) = true, want false", line)
		}
	}
}

func TestTerminalEnvForwardsColorAndSize(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("LS_COLORS", "di=34")
	got := terminalEnv(120, 40)
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LS_COLORS=di=34",
		"CLICOLOR=1",
		"COLUMNS=120",
		"LINES=40",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("terminalEnv() = %#v, missing %q", got, want)
		}
	}
}

func TestTerminalEnvDefaultsTERM(t *testing.T) {
	t.Setenv("TERM", "")
	got := terminalEnv(0, 0)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "TERM=") {
		t.Fatalf("terminalEnv() = %#v, missing TERM", got)
	}
}

func TestExportPersistsIntoGuestCommands(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true}, hostCWD: t.TempDir()}
	if err := sh.runScript(strings.NewReader("export FOO=bar\nprintenv FOO\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 1 || !envContains(api.streams[0].req.Env, "FOO=bar") {
		t.Fatalf("stream env = %#v, want FOO=bar", api.streams)
	}
}

func TestVMCDUsesGuestCWD(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true}, hostCWD: t.TempDir()}
	if err := sh.runScript(strings.NewReader("cd /\npwd\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if len(api.streams) != 1 || api.streams[0].req.WorkDir != "/" {
		t.Fatalf("workdir = %#v, want /", api.streams)
	}
}

func TestVMCDUsesGuestHome(t *testing.T) {
	tests := []struct {
		name  string
		ctx   commandContext
		input string
		want  string
	}{
		{
			name:  "ubuntu default user",
			ctx:   commandContext{Mode: modeVM, VMID: "work", Image: "ubuntu", Network: true},
			input: "cd\npwd\n",
			want:  "/home/ubuntu",
		},
		{
			name:  "ubuntu tilde child",
			ctx:   commandContext{Mode: modeVM, VMID: "work", Image: "ubuntu", Network: true},
			input: "cd ~/src\npwd\n",
			want:  "/home/ubuntu/src",
		},
		{
			name:  "created default user",
			ctx:   commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true},
			input: "cd ~\npwd\n",
			want:  "/home/cc",
		},
		{
			name:  "root user",
			ctx:   commandContext{Mode: modeVM, VMID: "work", Image: "ubuntu", User: "root", Network: true},
			input: "cd\npwd\n",
			want:  "/root",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
			sh := &shellState{api: api, context: tt.ctx, hostCWD: t.TempDir()}
			if err := sh.runScript(strings.NewReader(tt.input), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatalf("runScript() error = %v", err)
			}
			if len(api.streams) != 1 || api.streams[0].req.WorkDir != tt.want {
				t.Fatalf("workdir = %#v, want %s", api.streams, tt.want)
			}
		})
	}
}

func TestPrintVMsIsHumanReadableWhenEmpty(t *testing.T) {
	sh := &shellState{api: &fakeVSHAPI{}}
	var out bytes.Buffer
	if err := sh.printVMs(&out); err != nil {
		t.Fatalf("printVMs() error = %v", err)
	}
	if strings.TrimSpace(out.String()) != "No VMs" {
		t.Fatalf("printVMs() = %q, want No VMs", out.String())
	}
}

func TestBackgroundJobIsTracked(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "alpine", Network: true}, hostCWD: t.TempDir()}
	var out bytes.Buffer
	if err := sh.eval("echo hi &", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("eval() error = %v", err)
	}
	if !strings.Contains(out.String(), "[1] running echo hi") {
		t.Fatalf("background output = %q", out.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sh.jobsMu.Lock()
		done := len(sh.jobs) == 1 && sh.jobs[0].Done
		sh.jobsMu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job did not complete: %#v", sh.jobs)
}

func TestCompleterSuggestsAtCommandsAndPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "alpha dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cache, "images", "ubuntu"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh := &shellState{hostCWD: dir, rootCache: cache}
	completer := newVSHCompleter(sh)
	got, _ := completer.Do([]rune("@st"), 3)
	if !completionContains(got, "atus") || !completionContains(got, "art") || !completionContains(got, "op") {
		t.Fatalf("@ completions = %#v", got)
	}
	got, _ = completer.Do([]rune("@ub"), 3)
	if !completionContains(got, "untu") {
		t.Fatalf("image completions = %#v", got)
	}
	got, _ = completer.Do([]rune("@ubuntu --s"), 11)
	if !completionContains(got, "udo") {
		t.Fatalf("option completions = %#v", got)
	}
	got, _ = completer.Do([]rune("ec"), 2)
	if !completionContains(got, "ho") {
		t.Fatalf("command completions = %#v", got)
	}
	got, _ = completer.Do([]rune("@ubuntu ec"), 10)
	if !completionContains(got, "ho") {
		t.Fatalf("@ command completions = %#v", got)
	}
	got, _ = completer.Do([]rune("cd al"), 5)
	if !completionContains(got, `pha\ dir/`) {
		t.Fatalf("path completions = %#v", got)
	}
}

func TestCompleterMapsGuestHostPaths(t *testing.T) {
	root := t.TempDir()
	hostDir := filepath.Join(root, "tmp", "vsh host")
	if err := os.MkdirAll(filepath.Join(hostDir, "project one"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh := &shellState{
		hostCWD: root,
		context: commandContext{
			Mode: modeVM,
			CWD:  guestHostMount + filepath.ToSlash(hostDir),
		},
	}
	completer := newVSHCompleter(sh)
	got, _ := completer.Do([]rune("cd pro"), 6)
	if !completionContains(got, `ject\ one/`) {
		t.Fatalf("guest /host path completions = %#v", got)
	}
}

func TestCompleterPagesLargeCandidateSets(t *testing.T) {
	t.Setenv("VSH_COMPLETION_PAGE_SIZE", "12")
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("item-%02d", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sh := &shellState{hostCWD: dir}
	completer := newVSHCompleter(sh)

	got, _ := completer.Do([]rune("cat i"), 5)
	if len(got) != 12 {
		t.Fatalf("completion count = %d, want page size 12: %#v", len(got), got)
	}
	if !completionContains(got, "... 9 more matches; keep typing") {
		t.Fatalf("paged completions = %#v, missing more marker", got)
	}
}

func TestCompleterSortsPathsBeforePaging(t *testing.T) {
	t.Setenv("VSH_COMPLETION_PAGE_SIZE", "12")
	dir := t.TempDir()
	for _, name := range []string{"zz-file", "aa-file", "src", "bin"} {
		path := filepath.Join(dir, name)
		if name == "src" || name == "bin" {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sh := &shellState{hostCWD: dir}
	completer := newVSHCompleter(sh)

	got, _ := completer.Do([]rune("cat "), 4)
	if len(got) < 2 || string(got[0]) != "bin/" || string(got[1]) != "src/" {
		t.Fatalf("path ordering = %#v, want directories first", got)
	}
}

func TestHostShellCommandUsesCapturedPreludeForTTY(t *testing.T) {
	got := hostShellCommand("ls", true, "alias ll='ls -l'\n")
	if len(got) != 3 || got[1] != "-lc" || !strings.Contains(got[2], "alias ls=") || !strings.HasSuffix(got[2], "eval 'ls'") {
		t.Fatalf("hostShellCommand(tty) = %#v, want non-interactive command with color prelude", got)
	}
	if !strings.Contains(got[2], "alias ll='ls -l'") {
		t.Fatalf("hostShellCommand(tty) = %#v, want captured prelude", got)
	}
	got = hostShellCommand("ls", false, "ignored\n")
	if len(got) != 3 || got[1] != "-lc" || got[2] != "ls" {
		t.Fatalf("hostShellCommand(non-tty) = %#v, want login command", got)
	}
}

func TestHostShellPreludeDefinesColorAlias(t *testing.T) {
	got := hostShellPrelude()
	if !strings.Contains(got, "alias ls=") {
		t.Fatalf("hostShellPrelude() = %q, want ls alias", got)
	}
}

func TestColorPreludeQuotesAliasCommands(t *testing.T) {
	got := colorPrelude("ls --color=auto", "ls -G", true)
	for _, want := range []string{"shopt -s expand_aliases", "alias ls='ls --color=auto'", "alias ls='ls -G'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("colorPrelude() = %q, missing %q", got, want)
		}
	}
}

func TestMergedEnvOverridesValues(t *testing.T) {
	got := mergedEnv([]string{"TERM=dumb", "PATH=/bin"}, []string{"TERM=xterm-256color", "COLUMNS=120"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"TERM=xterm-256color", "PATH=/bin", "COLUMNS=120"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mergedEnv() = %#v, missing %q", got, want)
		}
	}
	if strings.Contains(joined, "TERM=dumb") {
		t.Fatalf("mergedEnv() = %#v, kept old TERM", got)
	}
}

func envContains(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func completionContains(items [][]rune, want string) bool {
	for _, item := range items {
		if string(item) == want {
			return true
		}
	}
	return false
}

func TestSendGuestInputBytesSplitsInterrupts(t *testing.T) {
	done := make(chan struct{})
	out := make(chan client.ExecInput, 4)
	sendGuestInputBytes(out, done, []byte("ab\x03cd"))
	close(out)
	var got []client.ExecInput
	for input := range out {
		got = append(got, input)
	}
	if len(got) != 3 {
		t.Fatalf("inputs = %#v, want stdin/signal/stdin", got)
	}
	if got[0].Kind != "stdin" || string(got[0].Data) != "ab" {
		t.Fatalf("first input = %#v", got[0])
	}
	if got[1].Kind != "signal" || got[1].Signal != "INT" {
		t.Fatalf("second input = %#v", got[1])
	}
	if got[2].Kind != "stdin" || string(got[2].Data) != "cd" {
		t.Fatalf("third input = %#v", got[2])
	}
}

func TestGuestCommandPullsMissingImageBeforeRun(t *testing.T) {
	api := &fakeVSHAPI{
		status:      client.InstanceState{ID: "default", Status: "running"},
		missingImgs: map[string]bool{"ubuntu": true},
	}
	sh := &shellState{api: api, context: defaultContext("default", ""), hostCWD: t.TempDir()}
	if err := sh.eval("@ubuntu echo hi", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("eval() error = %v", err)
	}
	if len(api.pulls) != 1 || api.pulls[0].name != "ubuntu" || api.pulls[0].source != "ubuntu" {
		t.Fatalf("pulls = %#v, want ubuntu from ubuntu", api.pulls)
	}
	if len(api.streams) != 1 || api.streams[0].req.Image != "ubuntu" {
		t.Fatalf("streams = %#v, want ubuntu run", api.streams)
	}
}

func TestGuestCommandCachesImageAndRunningVMState(t *testing.T) {
	api := &fakeVSHAPI{status: client.InstanceState{ID: "work", Status: "running"}}
	sh := &shellState{api: api, context: commandContext{Mode: modeVM, VMID: "work", Image: "ubuntu", Network: true}, hostCWD: t.TempDir()}
	if err := sh.runScript(strings.NewReader("true\ntrue\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}
	if api.imageGets != 1 {
		t.Fatalf("image gets = %d, want 1", api.imageGets)
	}
	if api.statusGets != 1 {
		t.Fatalf("status gets = %d, want 1", api.statusGets)
	}
	if len(api.streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(api.streams))
	}
}

func TestPersistentGuestShellPreservesState(t *testing.T) {
	inputs := make(chan client.ExecInput, 8)
	events := make(chan client.ExecEvent, 8)
	done := make(chan error, 1)
	session := &persistentGuestShell{
		inputs:  inputs,
		events:  events,
		done:    done,
		lastCWD: "/work",
	}
	go func() {
		cwd := "/work"
		alias := false
		for input := range inputs {
			if input.Kind == "stdin_close" {
				break
			}
			line := strings.TrimSpace(string(input.Data))
			switch {
			case strings.HasPrefix(line, "alias gp="):
				alias = true
			case line == "gp" && alias:
				events <- client.ExecEvent{Kind: "stdout", Data: []byte("guest-persist\n")}
			case line == "cd /tmp":
				cwd = "/tmp"
			case line == "pwd":
				events <- client.ExecEvent{Kind: "stdout", Data: []byte(cwd + "\n")}
			}
			events <- client.ExecEvent{Kind: "stdout", Data: []byte("__VSH_DONE__:0:" + cwd + "\n")}
		}
		close(events)
		done <- nil
	}()
	var out bytes.Buffer
	if err := session.run("alias gp='echo guest-persist'", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("alias run error = %v", err)
	}
	if err := session.run("gp", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("gp run error = %v", err)
	}
	if err := session.run("cd /tmp", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("cd run error = %v", err)
	}
	if err := session.run("pwd", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("pwd run error = %v", err)
	}
	session.close()
	if got := out.String(); got != "guest-persist\n/tmp\n" {
		t.Fatalf("output = %q, want guest-persist and /tmp", got)
	}
	if got := session.cwd(); got != "/tmp" {
		t.Fatalf("cwd = %q, want /tmp", got)
	}
}

func TestPersistentGuestShellConsumesSplitMarker(t *testing.T) {
	session := &persistentGuestShell{}
	before, _, _, ok := session.consumeOutput("hello\n__VSH_DONE__:")
	if ok {
		t.Fatalf("consumeOutput partial marker ok=true")
	}
	if before != "hello\n" {
		t.Fatalf("before = %q, want hello output", before)
	}
	before, code, cwd, ok := session.consumeOutput("7:/tmp\n")
	if !ok || before != "" || code != 7 || cwd != "/tmp" {
		t.Fatalf("consumeOutput marker = before %q code %d cwd %q ok %t", before, code, cwd, ok)
	}
}

func TestGuestCommandUsesColorPreludeForTTY(t *testing.T) {
	got := guestCommand("ls 'two words'", true)
	if len(got) != 3 || got[0] != "sh" || got[1] != "-lc" {
		t.Fatalf("guestCommand(tty) = %#v", got)
	}
	if strings.Contains(got[2], "bash -ic") || strings.Contains(got[2], "exec sh -lc") {
		t.Fatalf("guestCommand(tty) shell = %q, should not use interactive shell", got[2])
	}
	if !strings.Contains(got[2], "awk -F:") || !strings.Contains(got[2], "ls --color=always -C --width=${COLUMNS:-80}") || !strings.HasSuffix(got[2], "ls 'two words'") {
		t.Fatalf("guestCommand(tty) shell = %q, missing color prelude or command", got[2])
	}
}

func TestGuestPersistentCommandUsesColorPrelude(t *testing.T) {
	got := guestPersistentCommand()
	if len(got) != 3 || got[0] != "sh" || got[1] != "-lc" {
		t.Fatalf("guestPersistentCommand() = %#v", got)
	}
	if !strings.Contains(got[2], "ls --color=always -C --width=${COLUMNS:-80}") {
		t.Fatalf("guestPersistentCommand() shell = %q, missing ls color prelude", got[2])
	}
}

func TestGuestCommandUsesPlainShellWithoutTTY(t *testing.T) {
	got := guestCommand("echo hi", false)
	if len(got) != 3 || got[0] != "sh" || got[1] != "-lc" || !strings.Contains(got[2], "awk -F:") || !strings.HasSuffix(got[2], "echo hi") {
		t.Fatalf("guestCommand(non-tty) = %#v", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote(`a'b`); got != `'a'"'"'b'` {
		t.Fatalf("shellQuote() = %q", got)
	}
}

func TestGuestHostPaths(t *testing.T) {
	hostRoot, guestCWD, err := guestHostPaths(filepath.Join(string(filepath.Separator), "Users", "me", "src"))
	if err != nil {
		t.Fatalf("guestHostPaths() error = %v", err)
	}
	if hostRoot != string(filepath.Separator) {
		t.Fatalf("hostRoot = %q", hostRoot)
	}
	if guestCWD != "/host/Users/me/src" {
		t.Fatalf("guestCWD = %q", guestCWD)
	}
}

func TestParsePortForwardSpec(t *testing.T) {
	forward, err := parsePortForwardSpec("8080:80")
	if err != nil {
		t.Fatalf("parsePortForwardSpec() error = %v", err)
	}
	if forward.Protocol != "tcp" || forward.HostAddr != "127.0.0.1" || forward.HostPort != 8080 || forward.GuestPort != 80 {
		t.Fatalf("forward = %#v, want tcp 127.0.0.1:8080 -> 80", forward)
	}
}

func TestResolveCCVMPathHonorsExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-ccvm")
	if got, err := resolveCCVMPath(path); err != nil || got != path {
		t.Fatalf("resolveCCVMPath(explicit) = %q, %v; want %q, nil", got, err, path)
	}
}

func TestWriteReadDaemonState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccvm.json")
	if err := writeDaemonState(path, daemonState{Addr: "127.0.0.1:1234"}); err != nil {
		t.Fatalf("writeDaemonState() error = %v", err)
	}
	state, err := readDaemonState(path)
	if err != nil {
		t.Fatalf("readDaemonState() error = %v", err)
	}
	if state.Addr != "127.0.0.1:1234" {
		t.Fatalf("daemon addr = %q", state.Addr)
	}
	if _, err := readDaemonState(filepath.Join(t.TempDir(), "missing")); !os.IsNotExist(err) {
		t.Fatalf("readDaemonState(missing) error = %v, want not exist", err)
	}
}

type fakeVSHAPI struct {
	status       client.InstanceState
	statuses     []client.InstanceState
	streams      []fakeRun
	starts       []fakeStart
	streamEvents []client.ExecEvent
	pullEvents   []client.ProgressEvent
	pulls        []fakePull
	missingImgs  map[string]bool
	imageGets    int
	statusGets   int
}

type fakeRun struct {
	id  string
	req client.RunRequest
}

type fakeStart struct {
	id  string
	req client.StartInstanceRequest
}

type fakePull struct {
	name   string
	source string
}

func (f *fakeVSHAPI) HealthCheck() error { return nil }

func (f *fakeVSHAPI) GetImage(name string) (client.ImageState, error) {
	f.imageGets++
	if f.missingImgs != nil && f.missingImgs[name] {
		return client.ImageState{}, fmt.Errorf("missing image")
	}
	return client.ImageState{Name: name, Status: "available"}, nil
}

func (f *fakeVSHAPI) PullImageStream(name string, req client.PullImageRequest, onEvent func(client.ProgressEvent) error) error {
	source, err := req.SourceString()
	if err != nil {
		return err
	}
	f.pulls = append(f.pulls, fakePull{name: name, source: source})
	if f.missingImgs != nil {
		f.missingImgs[name] = false
	}
	if onEvent != nil {
		events := f.pullEvents
		if len(events) == 0 {
			events = []client.ProgressEvent{{Status: "downloaded", Artifact: name}}
		}
		for _, event := range events {
			if err := onEvent(event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *fakeVSHAPI) StartInstanceStreamWithID(id string, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	f.starts = append(f.starts, fakeStart{id: id, req: req})
	f.status = client.InstanceState{ID: id, Status: "running"}
	return f.status, nil
}

func (f *fakeVSHAPI) ShutdownInstanceWithID(id string) error {
	f.status = client.InstanceState{ID: id, Status: "stopped"}
	return nil
}

func (f *fakeVSHAPI) InstanceStatusOf(id string) (client.InstanceState, error) {
	f.statusGets++
	if f.status.ID == "" {
		return client.InstanceState{ID: id, Status: "stopped"}, nil
	}
	return f.status, nil
}

func (f *fakeVSHAPI) InstanceStatuses() ([]client.InstanceState, error) {
	return f.statuses, nil
}

func (f *fakeVSHAPI) AddPortForwardTo(string, client.PortForward) error {
	return nil
}

func (f *fakeVSHAPI) CreateWatchdogLease(client.WatchdogLeaseRequest) (client.WatchdogLeaseResponse, error) {
	return client.WatchdogLeaseResponse{LeaseID: "test-lease", TimeoutSeconds: 10}, nil
}

func (f *fakeVSHAPI) FeedWatchdogLease(string) error {
	return nil
}

func (f *fakeVSHAPI) ReleaseWatchdogLease(string) error {
	return nil
}

func (f *fakeVSHAPI) RunStreamIn(id string, req client.RunRequest, onEvent func(client.ExecEvent) error) error {
	return f.RunStreamInContext(context.Background(), id, req, onEvent)
}

func (f *fakeVSHAPI) RunStreamInContext(ctx context.Context, id string, req client.RunRequest, onEvent func(client.ExecEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.RunInteractiveStreamIn(id, req, nil, onEvent)
}

func (f *fakeVSHAPI) RunInteractiveStreamIn(id string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	f.streams = append(f.streams, fakeRun{id: id, req: req})
	events := f.streamEvents
	if len(events) == 0 {
		events = []client.ExecEvent{{Kind: "exit", ExitCode: 0}}
	}
	for _, event := range events {
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return err
			}
		}
	}
	return nil
}
