# PyNeurodesk Shell Hook Plan

## Goal

Add Python-side shell integration to `pyneurodesk` so a user can activate a shell session, load one or more Neurodesk containers, and invoke container-exported commands from normal host shell scripts while reusing the shared VM.

The target workflow is:

```sh
source <(neurodesk activate)
nd load niimath
niimath -help > hello.txt
```

This should use the existing runtime model:

- one shared VM
- images imported into the daemon as needed
- per-image virtiofs mounts added on demand
- processes chrooted into the target image root before exec

## Design Summary

The shell integration should be implemented entirely from the `pyneurodesk` side for the first pass.

The design has three main pieces:

1. `neurodesk activate`
   Emits shell code for the current shell session.

2. `nd load <image>`
   Resolves and imports the image if needed, ensures the shared VM exists, discovers exported commands, and generates wrapper scripts for those commands.

3. Command wrappers in a session-specific `bin/`
   Wrapper scripts appear on `PATH` and forward host command invocations into the shared VM using the image-aware mount-and-chroot execution path.

This keeps activation lightweight and lets command use trigger real runtime work only when needed.

## CLI Shape

### `neurodesk activate`

This command should print shell code, not execute activation itself.

Recommended invocation:

```sh
source <(neurodesk activate)
```

`activate` should:

- create or reuse a session directory
- export session environment variables
- prepend a session-specific wrapper `bin/` directory to `PATH`
- define a shell function `nd`
- define a deactivation function

It should not eagerly boot the VM by default.

Optional later extension:

- `neurodesk activate --boot`

That variant could ensure the shared daemon and VM exist immediately, but it should not be the default behavior.

### `nd load <image>`

This is the main explicit user action after activation.

Responsibilities:

- resolve the requested container reference
- import the image into the shared daemon if needed
- ensure the shared VM is booted if not already running
- discover exported commands for that image
- generate wrapper scripts into the session `bin/`
- record the loaded image and exported commands in session state

Expected behavior:

- safe to run more than once
- should not duplicate wrappers or corrupt session state
- should work for multiple images in the same shell session

### `nd unload <image>`

Not required for the first pass, but the design should leave room for it.

Responsibilities later:

- remove or deactivate wrappers owned by that image
- update session state
- leave the shared VM running unless explicitly shut down

### `nd list`

Useful early command.

Responsibilities:

- list images loaded in the current shell session
- optionally show the wrapper count or exported commands

### `nd exec <image> -- <command...>`

Useful as an explicit fallback even when wrappers exist.

Responsibilities:

- run a command inside the target image through `pyneurodesk`
- bypass wrapper discovery issues
- support scriptability for advanced users

## Activation Model

Activation should be session-local, not global across all terminals.

Each activation should create a session directory under something like:

```text
~/.cache/pyneurodesk/shell/<session-id>/
```

Recommended contents:

- `state.json`
  Tracks loaded images and generated wrappers.
- `bin/`
  Holds generated wrapper scripts placed on `PATH`.
- `logs/`
  Optional diagnostic logs for wrapper execution and session actions.

Recommended environment variables:

- `PYNEURODESK_SHELL_SESSION`
- `PYNEURODESK_SHELL_ROOT`
- `PYNEURODESK_SHELL_BIN`

Activation shell code should:

- export those variables
- save the original `PATH`
- prepend the session `bin/`
- define helper functions

Example activation payload:

```sh
export _PYNEURODESK_OLD_PATH="$PATH"
export PYNEURODESK_SHELL_SESSION="abc123"
export PYNEURODESK_SHELL_ROOT="$HOME/.cache/pyneurodesk/shell/abc123"
export PYNEURODESK_SHELL_BIN="$PYNEURODESK_SHELL_ROOT/bin"
export PATH="$PYNEURODESK_SHELL_BIN:$PATH"

nd() {
  command neurodesk shell "$@"
}

neurodesk_deactivate() {
  export PATH="$_PYNEURODESK_OLD_PATH"
  unset _PYNEURODESK_OLD_PATH
  unset PYNEURODESK_SHELL_SESSION
  unset PYNEURODESK_SHELL_ROOT
  unset PYNEURODESK_SHELL_BIN
  unset -f nd
  unset -f neurodesk_deactivate
}
```

This is only representative shell code. The real implementation should generate strictly shell-safe output.

## Wrapper Model

The wrapper model should be per-command and per-image, not a single global active image.

That means:

- `nd load niimath` generates wrappers for commands exported by `niimath`
- `nd load fsl` generates wrappers for commands exported by `fsl`
- both sets can coexist in the same shell session

This is a better match for the current runtime architecture because:

- all commands can share one VM
- each command can target a different image
- no shell-global “current image” switch is required

Each wrapper should know:

- which image it belongs to
- which exported command it should invoke
- how to call back into `pyneurodesk`

Example generated wrapper:

```sh
#!/bin/sh
exec python3 -m pyneurodesk.shell run-wrapper \
  --session "$PYNEURODESK_SHELL_SESSION" \
  --image "niimath" \
  --command "niimath" \
  -- "$@"
```

The wrapper should be a small stable shim. All real logic should live in Python.

## Runtime Execution Path

Wrapper execution should:

1. load the shell session state
2. resolve the image and command metadata
3. connect to the shared daemon
4. import the image if needed
5. ensure the shared VM is running
6. invoke the command through the image-aware mount-and-chroot path

This should reuse the work already implemented in the runtime:

- the first container triggers VM boot
- subsequent images mount into the running VM
- execs are chrooted into the correct image root

The wrapper should not need to understand these details directly. It should call a Python API helper that already knows how to do the right thing.

## Command Discovery

The main unresolved design question is where wrapper command names come from.

The first pass should keep this simple and pragmatic.

Possible sources:

1. Image metadata
   Best if the image already exposes a command/export list.

2. Filesystem scan
   Inspect common executable directories such as:
   - `/usr/local/bin`
   - `/usr/bin`
   - `/bin`
   - `/opt/...` if needed later

3. Explicit user command list
   As a fallback:
   - `nd load niimath --command niimath`

Recommended first pass:

- implement a filesystem scan of common bin directories
- only wrap executable regular files or symlinked executables
- provide an override flag for manual additions if scan results are incomplete

This is good enough to validate the shell UX without blocking on richer metadata.

## Python API Surface

The shell integration should live in `pyneurodesk`, not in the Go CLI.

Recommended additions:

- a small CLI entry point for shell operations
- session state helpers
- wrapper generation helpers
- command discovery helpers

Likely module split:

- `pyneurodesk.shell`
  CLI and orchestration
- `pyneurodesk.shell_state`
  session directory and `state.json` management
- `pyneurodesk.shell_wrap`
  wrapper generation and cleanup
- `pyneurodesk.shell_scan`
  exported command discovery

These names are only illustrative. The implementation can stay smaller if it remains clear.

## State Model

Session state should be explicit and versioned enough to evolve safely.

Suggested `state.json` structure:

```json
{
  "version": 1,
  "session_id": "abc123",
  "images": {
    "niimath": {
      "reference_path": "/containers/niimath_1.0.20250804_20251016",
      "commands": ["niimath"]
    }
  },
  "wrappers": {
    "niimath": {
      "image": "niimath",
      "command": "niimath"
    }
  }
}
```

The implementation should assume multiple terminals may have different session roots and should avoid any global mutable state beyond the shared daemon/VM itself.

## TTY and Scripting Behavior

This feature is specifically meant to work in regular shell scripts on the host.

That means it must support:

- stdout redirection
- stderr passthrough
- exit-code propagation
- stdin forwarding
- interactive TTY allocation when a terminal is attached
- terminal resize handling for interactive commands

Wrapper execution should preserve host shell semantics as closely as possible.

For a first pass:

- non-interactive execution and exit-code correctness are highest priority
- interactive TTY behavior should still be implemented, but can follow after the non-interactive path is solid

## Path and File Sharing

This plan should not overreach into automatic host-path sharing in v1.

The initial shell hook should focus on command invocation.

Non-goals for the first pass:

- automatic translation of arbitrary host paths into shared mounts
- transparent rewriting of all script arguments that happen to be paths
- a general-purpose bind-mount inference engine

Those can be added later, but they should not block the base shell-hook feature.

## Shell Support

The first pass should explicitly target:

- `bash`
- `zsh`

Later:

- `fish`

`activate` should either:

- auto-detect the current shell where practical
- or accept an explicit `--shell bash|zsh`

For early correctness, explicit shell selection is acceptable.

## Safety and Idempotency

The design should be robust when commands are repeated.

Requirements:

- `source <(neurodesk activate)` can be run multiple times safely
- `nd load niimath` can be repeated without corrupting wrappers
- wrapper regeneration should be atomic where practical
- stale sessions should be easy to clean up later

Wrapper generation should write temporary files and rename into place rather than editing wrappers in place.

## Suggested Implementation Order

1. Add a Python CLI entry point for shell operations.
2. Implement `neurodesk activate` that emits `bash`/`zsh` shell code.
3. Add session directory creation and `state.json` management.
4. Implement `nd load <image>` using the existing shared-daemon container/runtime behavior.
5. Implement command discovery by scanning common executable directories inside the image filesystem.
6. Generate wrapper scripts into the session `bin/`.
7. Implement wrapper execution back into `pyneurodesk`.
8. Add `nd list` for current-session visibility.
9. Add tests for activation, wrapper generation, and non-interactive shell-script execution.
10. Add interactive TTY coverage and polish.

## Testing Plan

Testing should cover:

- activation output shape for supported shells
- session state creation
- idempotent `nd load`
- wrapper generation for discovered commands
- correct image selection when multiple images are loaded
- wrapper execution exit code propagation
- stdout/stderr redirection from shell scripts
- reuse of the shared VM across multiple wrapped commands

Initial test layers:

- unit tests for activation payload generation
- unit tests for session state read/write
- unit tests for wrapper generation
- mocked client tests for `nd load`
- integration-style tests that execute generated wrappers in a subprocess shell

## Open Questions

These should be answered during implementation, not before starting:

1. What is the exact command discovery source of truth for exported commands?
2. Should `activate` auto-detect shell type or require `--shell` initially?
3. Should `nd load` eagerly boot the VM or only ensure the daemon and image are ready?
4. How should wrapper conflicts be handled when two images export the same command name?

Recommended initial answers:

1. Filesystem scan with a manual override flag.
2. Support `bash` and `zsh`, allow explicit `--shell`, and default sensibly.
3. Boot the VM during `nd load` so wrapper use is predictable afterward.
4. Fail clearly on command-name conflicts unless the user explicitly overrides.

## Non-Goals

Do not take on these in the first version:

- Windows shell integration
- fish shell support
- automatic host path rewrite/mounting
- global cross-terminal active-image state
- advanced wrapper conflict resolution
- shell completions
- remote shell session synchronization

The first success criterion is:

- a user can run `source <(neurodesk activate)`
- `nd load niimath`
- and then invoke `niimath` from the host shell through the shared VM using Python-managed wrappers
