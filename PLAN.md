# ccx3 implementation plan

## Goal

Implement:

- `cmd/ccvm/main.go` as the minimal HTTP runtime
- `cmd/cc/main.go` as the minimal frontend

using `~/dev/projects/cc` as a source of truth for behavior, not as code to migrate wholesale.

The design target is:

- one OCI image
- one managed kernel
- one running VM
- many execs inside that VM
- a small HTTP API
- a very thin CLI

## Correction to the previous plan

The previous plan leaned too hard on copying the old runtime internals. That is the wrong shape for `ccx3`.

The new plan is:

- rewrite the implementation in smaller packages
- consult the old codebase for semantics, algorithms, formats, and edge cases
- only copy tiny isolated snippets when rewriting them is pointless
- avoid importing the old package structure into `ccx3`

That means no broad migration of:

- `internal/api/*`
- `internal/initx/*`
- `internal/vfs/*`
- `internal/netstack/*`
- `internal/devices/*`

unless a very specific file turns out to be the minimal sensible unit.

## Scope for v1

### Keep

- `GET /healthz`
- `POST /shutdown`
- `GET /kernel`
- `POST /kernel/download`
- `GET /image`
- `GET /image/{image}`
- `POST /image/{image}`
- `GET /vm/supported`
- `POST /vm`
- `POST /vm/shutdown`
- `/vm/run` websocket

### Cut

- bundles
- Dockerfile build support
- GPU
- snapshots
- filesystem API over HTTP
- advanced networking API
- background daemon mode
- multi-VM management
- cross-architecture emulation in the first cut unless it falls out naturally

## High-level strategy

Build `ccx3` around a few narrow packages:

- `internal/kernel/alpine`
  - ensure a managed kernel exists on disk
  - expose metadata and local paths
- `internal/oci`
  - pull/cache OCI images
  - expose image config and unpacked rootfs access
- `internal/hv`
  - expose only the small hypervisor surface `ccx3` actually needs
- `internal/vm`
  - boot a VM from kernel + OCI rootfs
  - manage one live VM
  - run commands inside it
  - stream stdio
- `client`
  - typed HTTP/websocket client for the frontend

The old repo is reference material for each package, but the package boundaries should be chosen for `ccx3`, not inherited.

## Old repo as source of truth

Use the old repo to answer these questions:

- how OCI images are resolved and cached
- how runtime config is derived from OCI metadata
- how the kernel is downloaded and selected
- how the guest is booted on supported hosts
- how exec I/O and exit codes behave
- how terminal raw mode and signal forwarding should work

The old CLI and runtime are especially useful for:

- command defaulting rules
- env merging
- workdir handling
- exit status semantics
- interactive terminal behavior

They are not the target architecture.

## Proposed backend design

### `cmd/ccvm/main.go`

`ccvm` should own:

- cache paths
- image alias mapping
- kernel state
- one active VM
- the HTTP/websocket surface

Internally it should have a small `server` struct:

- `kernelManager`
- `imageStore`
- `vmManager`
- `mu sync.Mutex`

### `internal/kernel/alpine`

Rewrite this package to do only:

- determine cache location
- detect whether the kernel is present
- download it if missing
- report status/version/source

Use the old codebase to preserve:

- source URLs
- archive layout knowledge
- host architecture mapping

but keep the new API small.

### `internal/oci`

Rewrite this package to provide:

- `Pull(ctx, ref, opts) (*Image, error)`
- `Open(alias) (*Image, error)`
- `List() []ImageStatus`
- `ResolveConfig()`

The important outputs for `ccx3` are:

- unpacked rootfs
- image metadata
- status/progress reporting

The package should be runtime-oriented, not library-general.

### `internal/vm`

This is the main new package.

It should own:

- VM boot
- lifecycle state
- one active exec at a time
- stdin/stdout/stderr streaming
- signal forwarding

Suggested API:

- `Supports() error`
- `Start(ctx, Config) (*Machine, error)`
- `(*Machine) Exec(ctx, ExecRequest) (*ExecSession, error)`
- `(*Machine) Shutdown(ctx) error`
- `(*Machine) Status() VMStatus`

This package is where the most care is needed. The old repo should be used to derive:

- boot sequence
- guest setup expectations
- console/vsock decisions
- exit code and signal behavior

But the code should be rewritten around `ccx3`'s smaller lifecycle.

## Proposed frontend design

### `cmd/cc/main.go`

The frontend should remain simple:

```sh
cc [flags] <image> [command] [args...]
```

Flags for the first cut:

- `-ccvm <path>`
- `-memory <mb>`
- `-cpus <n>`
- `-workdir <dir>`
- `-user <uid[:gid]>`
- `-env KEY=value`
- `-mount tag[:hostpath[:rw]]`
- `-dmesg`

Flow:

1. locate and start `ccvm`
2. read the hello JSON
3. health-check it
4. ensure the image is present
5. start the VM
6. open `/vm/run` websocket
7. bridge terminal I/O and signals
8. propagate exit status
9. shut down VM and `ccvm`

This keeps `cc` as transport and UX glue only.

## Implementation phases

### Phase 1: define the small runtime contracts

Before writing substantial code, define the narrow interfaces and types for:

- kernel status
- image status
- VM config
- exec request
- exec event stream

This prevents the old codebase structure from leaking into `ccx3`.

### Phase 2: implement kernel management from scratch

Write `internal/kernel/alpine` fresh, using the old implementation only to validate:

- where the kernel comes from
- how versioning should work
- what file layout is needed on disk

### Phase 3: implement OCI handling from scratch

Write `internal/oci` around the exact needs of `ccvm`:

- pull
- unpack
- list
- resolve config

No attempt to recreate the old public OCI abstraction layers.

### Phase 4: implement VM boot and exec

Write `internal/vm` to support:

- boot from managed kernel + prepared rootfs
- command execution in a live VM
- stdio streaming
- exit codes
- shutdown

This is the part that may still require selectively lifting low-level logic from the old repo if the alternative is re-deriving complex guest/bootstrap details from scratch. But the target remains a new package, not a transplanted subsystem.

### Phase 5: wire `cmd/ccvm/main.go`

Implement the real handlers and websocket endpoint.

### Phase 6: write `cmd/cc/main.go`

Implement the frontend around the new client package and terminal handling.

### Phase 7: trim further

Once end-to-end works, reduce anything that drifted beyond the minimal shape.

## Task list

### Phase 0: repo shaping

1. Confirm the minimal package layout and commit to the rewrite-first boundaries:
   - `internal/kernel/alpine`
   - `internal/oci`
   - `internal/hv`
   - `internal/vm`
   - `client`
2. Remove or ignore placeholder assumptions in current stubs that imply missing old subsystems will be dropped in unchanged.
3. Define the cache/config directory policy for:
   - kernel artifacts
   - OCI image artifacts
   - image alias metadata
4. Decide where shared JSON request/response types live so `cmd/ccvm`, `cmd/cc`, and `client` do not drift.

### Phase 1: protocol and type contracts

1. Define backend state types:
   - `KernelState`
   - `ImageState`
   - `VMState`
2. Define HTTP request/response types for:
   - image pull
   - VM start
   - VM status
   - shutdown
3. Define websocket message types for:
   - exec request
   - stdout event
   - stderr event
   - exit event
   - stdin input
   - close stdin
   - signal
   - optional resize
4. Define the internal `vm.Config`, `vm.ExecRequest`, and `vm.ExecEvent` contracts before any VM code is written.
5. Extend `client/client.go` to match the full planned protocol surface, even if handlers are stubbed temporarily.

### Phase 2: kernel manager rewrite

1. Implement cache path resolution for the managed kernel.
2. Implement host architecture detection needed for kernel selection.
3. Reconstruct the kernel source/version rules from the old repo.
4. Implement `Status()` for:
   - missing
   - downloaded
   - downloading
   - error
5. Implement `Ensure(ctx)` or equivalent to download and prepare the kernel.
6. Expose the final local kernel paths needed by `internal/vm`.
7. Add focused tests for:
   - status detection
   - cache layout
   - version/source reporting

### Phase 3: OCI/image store rewrite

1. Define the on-disk image store layout.
2. Define the alias metadata format mapping image name to source reference.
3. Implement image listing from the local store.
4. Implement image lookup by alias.
5. Implement pull/download flow with progress callbacks.
6. Implement image unpack/preparation to the exact form needed by `internal/vm`.
7. Implement runtime config extraction from OCI metadata:
   - env
   - cmd
   - entrypoint
   - working dir
   - user
8. Implement image status reporting:
   - missing
   - downloading
   - downloaded
   - error
9. Add focused tests for:
   - alias persistence
   - config extraction
   - list/get behavior

### Phase 4: hypervisor surface cleanup

1. Define the minimal `internal/hv` interfaces required by the rewritten VM package.
2. Strip the interface down to what `internal/vm` actually uses.
3. Validate the initial target path for macOS arm64 support.
4. Identify any missing low-level functionality that must be added before VM boot can work.
5. Add a simple `Supports()` check that returns a user-facing error for unsupported hosts or unavailable virtualization.

### Phase 5: VM manager rewrite

1. Define the `internal/vm` state machine:
   - idle
   - starting
   - running
   - stopping
   - error
2. Implement VM start using:
   - managed kernel
   - prepared rootfs
   - requested memory/cpu config
3. Reconstruct the minimum guest bootstrap needed to reach a reusable multi-exec environment.
4. Implement a persistent command-execution channel inside the running VM.
5. Implement exec start with:
   - command
   - env overrides
   - workdir
   - user
6. Implement stdout/stderr streaming from the guest process.
7. Implement stdin forwarding and close-stdin handling.
8. Implement signal forwarding.
9. Implement exit code reporting.
10. Implement single-exec-at-a-time enforcement.
11. Implement clean VM shutdown.
12. Implement `Status()` reporting for the active machine.
13. Add focused tests for:
   - state transitions
   - exec lifecycle
   - shutdown behavior

### Phase 6: `cmd/ccvm` backend wiring

1. Replace the current stub server state with a real `server` struct.
2. Wire `GET /healthz`.
3. Wire `POST /shutdown`.
4. Wire `GET /kernel`.
5. Wire `POST /kernel/download`.
6. Wire `GET /image`.
7. Wire `GET /image/{image}`.
8. Wire `POST /image/{image}` with streamed progress responses.
9. Wire `GET /vm/supported`.
10. Wire `POST /vm`.
11. Wire `POST /vm/shutdown`.
12. Add `GET /vm/status` if retained.
13. Implement `/vm/run` websocket upgrade and session bridging.
14. Add request validation and clean error responses for invalid state transitions.
15. Ensure backend shutdown cleans up any running VM.

### Phase 7: frontend client implementation

1. Expand `client/client.go` beyond health/shutdown.
2. Implement typed methods for:
   - kernel status
   - kernel download
   - image list/get/pull
   - VM supported
   - VM start/status/shutdown
   - exec websocket session
3. Add helpers for streamed progress decoding.
4. Add helpers for websocket exec event handling.
5. Add focused tests for protocol decoding where practical.

### Phase 8: `cmd/cc` frontend rewrite

1. Replace the current demo entrypoint with a real CLI.
2. Implement flag parsing for the agreed v1 flags.
3. Implement `ccvm` binary discovery.
4. Implement backend process launch and hello decoding.
5. Implement backend health check.
6. Implement image ensure/pull behavior.
7. Implement VM start request construction.
8. Implement exec websocket connection.
9. Implement terminal mode detection.
10. Implement raw terminal mode for interactive sessions.
11. Implement stdin pumping to websocket.
12. Implement stdout/stderr rendering from websocket events.
13. Implement signal forwarding from host process to exec session.
14. Implement exit code propagation back to the local shell.
15. Implement cleanup:
   - close stdin
   - stop VM
   - stop `ccvm`
   - restore terminal state

### Phase 9: verification and reduction

1. Verify the primary flow:
   - start backend
   - pull image
   - boot VM
   - run command
   - run second command in same VM
   - shut down cleanly
2. Verify failure modes:
   - unsupported hypervisor
   - missing image
   - image pull failure
   - VM start failure
   - exec failure
   - interrupted terminal session
3. Verify that frontend exit codes match guest exit codes.
4. Remove code paths that only existed to mirror old behavior but are no longer part of the v1 scope.
5. Simplify any package API that grew beyond the minimal contracts.

## Deliverables

### Deliverable 1

- stable protocol types
- rewritten kernel manager

### Deliverable 2

- rewritten OCI/image store
- image alias persistence

### Deliverable 3

- rewritten VM package capable of boot + multi-exec

### Deliverable 4

- fully functional `cmd/ccvm/main.go`

### Deliverable 5

- fully functional `cmd/cc/main.go`

### Deliverable 6

- end-to-end verification of the v1 command flow

## Specific design constraints

### Single VM

For v1:

- only one running VM
- only one active exec at a time

This keeps the runtime state machine simple.

### One-shot backend process

For v1:

- `cc` starts a fresh `ccvm`
- `ccvm` exits when `cc` is done

That avoids prematurely designing daemon persistence.

### Supported host target

Given the current repo contents, the first serious target is:

- macOS arm64

The API should still be designed portably, but the implementation plan should not pretend Linux and Windows are immediate.

## Main risks

### Guest bootstrap complexity

The biggest danger is underestimating the amount of logic needed to go from:

- kernel
- rootfs
- hypervisor

to a usable multi-exec guest environment.

This is where the old repo is most valuable as a reference.

### Accidental re-expansion

If the rewrite starts inheriting old abstractions, `ccx3` will bloat quickly. The new package boundaries need to stay narrow.

### Terminal quality

Interactive terminal behavior is a core user-facing feature. Raw mode, stdin closure, and signal forwarding should be treated as real functionality, not cleanup work.

## Basic strategy summary

The implementation strategy should be:

1. define the minimal `ccx3` runtime contracts
2. rewrite kernel management to fit those contracts
3. rewrite OCI handling to fit those contracts
4. rewrite VM boot/exec around a single-VM runtime
5. implement `ccvm` on top
6. implement `cc` as a thin frontend

The old codebase should be used constantly during this work, but as specification and reference, not as the default implementation source.

## Review points

Please confirm whether this matches your intent:

1. rewrite-first, reference-only, no broad code migration
2. single VM and single exec at a time for v1
3. one-shot `ccvm` process per `cc` invocation
4. macOS arm64 as the initial real target
