# Repository tooling for tinyrange/cc. Installation requires user approval.
load("//stdlib/git.star", git_tools = "tools")
load("//stdlib/golang.star", go_tools = "tools", go_run = "run", go_test = "test", go_bench = "bench", go_fuzz = "fuzz")
load("//stdlib/github.star", github_tools = "tools", pr_create = "pr_create", pr_comment = "pr_comment", pr_merge = "pr_merge", workflow_run = "workflow_run", run_rerun = "run_rerun", run_cancel = "run_cancel", queue_enqueue = "queue_enqueue")

workspace = privileged.workspace(".", readonly = False)
git = git_tools
go = go_tools + module("go_execution", run = go_run, test = go_test, bench = go_bench, fuzz = go_fuzz)
github = github_tools + module("github_mutations", pr_create = pr_create, pr_comment = pr_comment, pr_merge = pr_merge, workflow_run = workflow_run, run_rerun = run_rerun, run_cancel = run_cancel, queue_enqueue = queue_enqueue)

_SUFFIX = ".exe" if platform.startswith("windows/") else ""
_BUILD = "build/staragent"
_CACHE = "local/staragent/cc-cache"

def _operand(value):
    if type(value) != "string" or not value or value.startswith("-"):
        fail("expected a nonempty operand not starting with '-'")
    return value

def _argv(values):
    if type(values) not in ["list", "tuple"] or not values:
        fail("command must be a nonempty list/tuple of strings")
    for value in values:
        if type(value) != "string":
            fail("command arguments must be strings")
    return list(values)

def guestinit():
    """Build embedded guest payloads before compilation or tests."""
    return go.run("./internal/cmd/build-guestinit", cwd = workspace, check = True, timeout_ms = 600000)

def _mkdirs(path):
    # workspace.mkdir creates one directory and rejects existing paths.
    parent = ""
    for part in path.split("/"):
        if part in ["", ".", ".."]:
            fail("expected a clean workspace-relative directory path")
        child = parent + "/" + part if parent else part
        if part not in workspace.list_dir(parent):
            workspace.mkdir(child)
        # Also reject an existing regular file instead of silently accepting it.
        workspace.list_dir(child)
        parent = child

def build(vmsh = False, glass = False):
    """Build this checkout, never a downloaded runtime; return build results."""
    _mkdirs(_BUILD)
    results = [guestinit()]
    for name in ["cc", "ccvm"] + (["glass"] if glass else []):
        results.append(go.build_process("./cmd/" + name, output = workspace.path(_BUILD + "/" + name + _SUFFIX), cwd = workspace, check = True, timeout_ms = 600000))
    if vmsh:
        results.append(go.build_process("./cmd/vmsh", output = workspace.path(_BUILD + "/vmsh" + _SUFFIX), cwd = workspace.path("frontends/vmsh"), check = True, timeout_ms = 600000))
    return results

def test(packages = ["./..."], vmsh = False, race = False):
    """Build payloads then run selected short tests; opt into vmsh/race."""
    results = [guestinit(), go.test(packages = packages, short = True, race = race, count = 1, timeout = "30m", timeout_ms = 1860000, cwd = workspace, check = True)]
    if vmsh:
        results.append(go.test("./...", short = True, count = 1, timeout = "3m", timeout_ms = 240000, cwd = workspace.path("frontends/vmsh"), check = True))
    return results

def cc(args, stdin = None, background = False, timeout_ms = 600000, check = False, env = None):
    """Run checkout cc with a dedicated cache. Returns an inspectable process.

    cc auto-starts its daemon. Background handles need explicit wait/kill/close.
    A client timeout is not proof the guest command or VM has stopped.
    """
    args = _argv(args)
    # cc creates the cache recursively and enforces private permissions.
    return privileged.run(workspace.path(_BUILD + "/cc" + _SUFFIX), cwd = workspace, stdin = stdin, background = background, timeout_ms = timeout_ms, check = check, env = env, output_limit = 4194304, *(["-ccvm", workspace.path(_BUILD + "/ccvm" + _SUFFIX), "-cache-dir", workspace.path(_CACHE)] + args))

def doctor():
    """Download/check the kernel and report virtualization availability."""
    return cc(["doctor"], check = True)

def images(name = None):
    return cc(["images"] + ([_operand(name)] if name != None else []), check = True)

def pull(name, source, timeout_ms = 1800000):
    """Import an OCI reference, local SIMG/SIF, or CVMFS source."""
    return cc(["pull", _operand(name), _operand(source)], timeout_ms = timeout_ms, check = True)

def start(name, image, network = False, shares = [], memory_mb = None, cpus = None, boot_timeout = "60s", init = None, default_user = None, vnc = False):
    """Start a named VM. Shares are explicit WRITABLE HOST:GUEST specs."""
    args = ["vm", "start", "--timeout=" + boot_timeout]
    if network:
        args.append("--network")
    if vnc:
        args.append("--vnc")
    for flag, value in [("memory-mb", memory_mb), ("cpus", cpus)]:
        if value != None:
            if type(value) != "int" or value <= 0:
                fail(flag + " must be a positive integer")
            args.append("--" + flag + "=" + str(value))
    for flag, value in [("init", init), ("default-user", default_user)]:
        if value != None:
            args.append("--" + flag + "=" + _operand(value))
    if type(shares) not in ["list", "tuple"]:
        fail("shares must be a list/tuple of explicit writable HOST:GUEST specs")
    for share in shares:
        args.append("--share=" + _operand(share))
    return cc(args + ["--", _operand(name), _operand(image)], check = True)

def vm_list():
    return cc(["vm", "list"], check = True)

def status(name):
    return cc(["vm", "status", _operand(name)], check = True)

def stop(name):
    """Stop only the specified VM; never stop unrelated instances."""
    return cc(["vm", "stop", _operand(name)], check = True)

def exec_vm(name, argv, stdin = None, background = False, timeout_ms = 600000, check = False):
    """Execute argv in a running VM without host-shell interpolation."""
    return cc(["vm", "run", _operand(name), "--"] + _argv(argv), stdin = stdin, background = background, timeout_ms = timeout_ms, check = check)

def shell(name, script, stdin = None, background = False, timeout_ms = 600000, check = False):
    """Explicit guest /bin/sh script execution; requires sh in the image."""
    return exec_vm(name, ["sh", "-lc", script], stdin = stdin, background = background, timeout_ms = timeout_ms, check = check)

def run_image(image, argv, stdin = None, timeout_ms = 600000, check = False):
    """Run via the default-instance CLI flow; prefer named VMs for isolation."""
    return cc(["run", _operand(image), "--"] + _argv(argv), stdin = stdin, timeout_ms = timeout_ms, check = check)

def forward(name, mapping):
    return cc(["vm", "forward", _operand(name), _operand(mapping)], check = True)

def open_pr(title, body, base, draft = False, remote = "origin"):
    """Push an already committed feature branch and open a PR. No auto-commit."""
    branch = git.current_branch(cwd = workspace)
    if not branch or branch in [base, "main", "master"]:
        fail("switch to a feature branch before opening a PR")
    if not git.is_clean(cwd = workspace):
        fail("review and commit changes before opening a PR")
    git.push(remote = remote, branch = branch, set_upstream = True, cwd = workspace, check = True)
    return github.pr_create(title, body, base = base, head = branch, draft = draft, cwd = workspace, check = True)

def merge_pr(number, expected_head, method = "squash", auto = False):
    """Merge an explicitly reviewed SHA; no admin bypass or branch deletion.

    Direct merges require successful reported checks. auto=True requests GitHub
    auto-merge subject to repository policy; it does not imply the PR is merged.
    """
    if not regexp.compile(r"^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$").matches(expected_head):
        fail("expected_head must be the full reviewed commit SHA")
    result = github.pr_view(str(number), json_fields = ["state", "isDraft", "headRefOid"], cwd = workspace, check = True)
    if result.stdout_truncated:
        fail("PR metadata was truncated")
    pr = json.decode(result.stdout)
    if pr["state"] != "OPEN" or pr["isDraft"] or pr["headRefOid"] != expected_head.lower():
        fail("PR must be open, ready, and still at the reviewed SHA")
    if not auto:
        github.pr_checks(str(number), cwd = workspace, check = True)
    return github.pr_merge(str(number), method = method, auto = auto, match_head_commit = expected_head.lower(), delete_branch = False, cwd = workspace, check = True)

vm = module("vm", cc = cc, doctor = doctor, images = images, pull = pull, start = start, list = vm_list, status = status, stop = stop, exec = exec_vm, shell = shell, run_image = run_image, forward = forward)
workflows = module("workflows", guestinit = guestinit, build = build, test = test, open_pr = open_pr, merge_pr = merge_pr)

def propose_agents_star(content):
    """Stage a configuration; only user approval installs it."""
    candidate = privileged.tempdir()
    path = candidate.path("candidate.star")
    privileged.write_file(path, content)
    return privileged.edit_agents_star(path)

_INSTRUCTIONS = """
This is tinyrange/cc, an experimental Go microVM runtime. Read local source and
.github/workflows/test.yml for authoritative flags/checks. README can lag source.
Key areas: cmd/cc CLI, cmd/ccvm daemon, client, internal/ccvmd API,
internal/vm orchestration, internal/hv backends, internal/oci image import.
frontends/vmsh is a separate Go module. Match the Go version in go.mod.
Tools: workspace, git, go (including run/test/bench/fuzz), github, vm, workflows.
Use help(tool) for signatures. Processes expose stdout/stderr/success/exit_code;
inspect failures and truncation. Do not print secrets from auth or environment.
Build guest-init before builds/tests (workflows.guestinit or workflows.build).
For local VM work: workflows.build(); vm.doctor(); vm.pull(name, source);
vm.start(unique_name, image); vm.exec(name, ["uname", "-a"]); vm.stop(name).
Use fixtures/alpine.simg on amd64 and fixtures/alpine-arm64.simg on arm64.
vm.shell(name, script) executes only inside the guest. No host shell is exposed.
VM binaries live in build/staragent; cache/daemon state in local/staragent/cc-cache.
Do not rebuild binaries under a running daemon and assume the daemon upgraded.
Named VMs persist: track and stop only instances you created, including on failure.
CLI timeout/cancellation is not guaranteed guest cleanup. Inspect status explicitly.
Never auto-enable network, writable host shares, or change host hypervisor permissions.
Writable shares let guest commands modify host files. Do not mount secrets or the
repository/configuration into untrusted guests. The process tools are not a sandbox.
Run focused checks for the change. CI includes vet, short tests, API contract,
targeted race checks and separate vmsh checks. Expensive KVM tests are opt-in.
Do not install dependencies or invoke release/deploy workflows without authorization.
PR flow: inspect git status/diff, create a feature branch, make focused changes,
run relevant checks, stage explicit paths and commit, then workflows.open_pr with
an explicit base. Review PR diff/checks and head SHA before workflows.merge_pr.
Only push/open/merge when authorized by the user's task. Never bypass protections.
Use github workflow/run inspection to diagnose CI; dispatch/rerun/cancel explicitly.
For merge queues use github.queue_enqueue with the reviewed SHA and verify status.
Auto-merge/queue acceptance is not proof of merge: re-read PR state before reporting.
Never edit AGENTS.star through file/process tools; use propose_agents_star approval.
"""

environment = {"workspace": workspace, "git": git, "go": go, "github": github, "vm": vm, "workflows": workflows, "propose_agents_star": propose_agents_star}
default_repl = repl(environment)
default = privileged.model("gpt-6-astra").create(default_repl, prompt_addons = [_INSTRUCTIONS])
