# ccx3 cleanup plan

## Goal

Prepare `ccx3` for feature work by cleaning up the parts of the codebase that currently fight the product shape in `VISION.md`.

The main objective is not to add new capabilities yet. It is to make the next capabilities land on the right abstractions without drifting from the documented public API shape already established in git history.

That public API shape should remain:

- `GET /vm/supported`
- `GET /vm/status`
- `POST /vm`
- `POST /vm/shutdown`
- `/vm/run` as the exec transport

The cleanup should improve the internals and make `/vm/run` truly support repeated execs inside one live VM. It should not rename the public resources to `/instance` unless the documented API is intentionally changed later.

The architectural objective is to make the next capabilities land on the right abstractions:

- long-lived VM or instance lifecycle
- separate exec lifecycle
- portable workload-shaped API
- explicit image identity
- reliable build and test paths

## Guiding constraints

This cleanup should preserve the core intent from `VISION.md`:

- the VM is the isolation boundary
- an image is the environment template, not the whole runtime model
- multiple execs may run inside the same live VM over time
- networking, shares, and snapshots are runtime features, not afterthoughts
- the control plane should stay workload-centric rather than hypervisor-centric
- the system should stay unprivileged and portable across host platforms

## Current problems to fix first

### 1. VM and exec lifecycles are still conflated

Today the public and internal runtime APIs are still shaped around:

- `StartVMRequest`
- `RunVMResponse`
- `POST /vm`
- `POST /vm/run`

That makes the system behave more like a VM-backed single-command runner than a long-lived instance with many execs.

This is the biggest mismatch with `VISION.md`, and it should be fixed before adding:

- shares
- networking
- snapshots
- richer exec semantics

### 2. Runtime state is too shallow

The current VM manager tracks only a small amount of state and loses useful lifecycle information once a VM exits or is shut down.

Before adding more lifecycle-heavy features, the runtime needs durable state for:

- creating
- booting
- ready
- exec running
- stopping
- stopped
- failed

It also needs explicit shutdown behavior and better error retention.

### 3. Image identity is not explicit enough

The image layer still defaults silently to mutable tags like `latest`, and the runtime state does not expose a resolved digest.

That is tolerable for a prototype pull path, but it is the wrong basis for:

- snapshots
- warm boot caches
- reproducible exec environments
- compatibility checks

### 4. Build and test paths are too host-toolchain-sensitive

Some runtime paths and tests still rely on building guest-side components on the host at runtime.

That increases flakiness and makes it harder to trust the system as the codebase grows.

## Cleanup phases

## Phase 1: split instance lifecycle from exec lifecycle behind the existing `/vm` API

Introduce a clear runtime model centered on:

- `Image`
- `Instance`
- `Exec`

Concrete work:

- keep the public `/vm` and `/vm/run` API shape documented in history
- separate instance-create and exec semantics internally even if the public request types remain VM-named for compatibility
- redesign `internal/vm` around a live instance handle instead of `Start` plus `Run`
- add an internal API roughly shaped like:
  - `CreateInstance(ctx, req) (*Instance, error)`
  - `(*Instance) Start(ctx) error`
  - `(*Instance) Exec(ctx, req) (*ExecSession, error)`
  - `(*Instance) Shutdown(ctx) error`
  - `(*Instance) Status() InstanceState`
- stop treating “boot until first command begins” as equivalent to “VM is ready”
- implement `/vm/run` as the repeated exec path against the running VM rather than a one-shot replacement for VM lifecycle

Desired outcome:

- the runtime can describe a booted VM without tying that state to a specific command
- exec becomes an operation against a running instance, not a side effect of creation

## Phase 2: align the HTTP behavior to the documented `/vm` surface

Once the internal model is separated, make the existing public API behave according to the documented design.

Concrete work:

- keep `POST /vm` as the public VM start or create endpoint
- keep `GET /vm/status` and `POST /vm/shutdown` as the public lifecycle endpoints
- make `/vm/run` the real exec transport for repeated commands inside the running VM
- wire the existing `ExecRequest`, `ExecInput`, and `ExecEvent` concepts into `/vm/run`
- avoid introducing parallel public routes that duplicate the documented API

Desired outcome:

- the external API matches both the documented history and the thesis in `VISION.md`
- future shares, networking, and snapshots can attach to instances cleanly

## Phase 3: strengthen runtime state and lifecycle correctness

Refactor the manager and backend so state transitions are explicit and observable.

Concrete work:

- define a richer instance state model
- retain terminal failure information after exit
- make shutdown wait for a terminal state, not just fire cancellation
- stop dropping lifecycle details when the running pointer is cleared
- surface state transitions through typed status objects instead of ad hoc strings only
- audit context handling so shutdown and boot honor caller timeouts

Suggested status model:

- `creating`
- `booting`
- `ready`
- `exec_running`
- `stopping`
- `stopped`
- `failed`

Desired outcome:

- lifecycle-heavy features have a trustworthy base
- status and failure reporting become stable enough for API consumers

## Phase 4: make image identity explicit and reproducible

Refactor the OCI layer so runtime state is based on resolved identity, not just the caller’s original reference.

Concrete work:

- record the resolved manifest digest in image metadata
- expose digest and platform in `ImageState`
- stop silently depending on mutable `latest` semantics in runtime-facing paths
- keep the original source ref for UX, but separate it from the resolved identity
- key shared cache and future snapshot compatibility off resolved content identity where possible

Desired outcome:

- the runtime can reason about compatibility and reproducibility
- snapshot and restore design work has a safe foundation

## Phase 5: remove runtime host-build work from the hot path

The runtime should not need to rebuild guest helpers every time that functionality is used.

Concrete work:

- decide how guest init should be produced:
  - checked-in artifact
  - generated once and content-address cached
  - explicit build step in development workflows
- keep host toolchain probing out of normal runtime execution paths
- make integration tests depend on explicit prerequisites rather than implicit local compiler quirks
- separate “always-on correctness tests” from “opt-in host integration tests”

Desired outcome:

- more predictable runtime behavior
- less CI and local-env sensitivity
- clearer guarantees about what test coverage is always enforced

## Phase 6: clean package boundaries before adding features

After the lifecycle and identity refactors, tighten package responsibilities.

Target package roles:

- `client`
  - protocol types and client transport only
- `internal/oci`
  - image ingestion, metadata, resolved identity, rootfs preparation
- `internal/kernel/alpine`
  - managed kernel acquisition and metadata
- `internal/vm`
  - instance and exec lifecycle orchestration
- `internal/hv`
  - hypervisor-facing machinery only
- `internal/guestinit`
  - guest supervisor artifact management, not policy

Concrete work:

- remove request or response types that mix instance creation concerns with exec concerns
- move guest command resolution and policy decisions to the lifecycle layer that owns them
- keep `internal/hv/hvf` focused on guest machine execution rather than control-plane semantics

Desired outcome:

- future features land in predictable places
- the API layer stops leaking backend assumptions into product semantics

## Deferred until after cleanup

Do not start these until Phases 1 through 4 are in place:

- snapshots
- persistent share attachment APIs
- runtime networking APIs
- multiple concurrent exec sessions
- richer non-root user support
- multi-platform backend expansion beyond the current shape

These features depend on the lifecycle and identity work above. Shipping them first would harden the wrong abstractions.

## Implementation order

Recommended order:

1. split instance and exec models in `internal/vm` and related runtime code while preserving the public `/vm` API
2. make `cmd/ccvm` serve the documented `/vm` lifecycle and `/vm/run` exec behavior
3. preserve and expose richer instance state
4. record and expose resolved image identity
5. harden guest-init and host-toolchain handling
6. only then begin feature work for shares, networking, snapshots, and richer exec support

## Definition of done for cleanup

The cleanup is complete when:

- a booted instance exists independently from a command run inside it
- exec is a first-class operation against a running instance
- the public `/vm` API behaves according to the documented multi-exec design
- instance status survives failures and shutdowns with meaningful detail
- image metadata includes resolved identity suitable for compatibility checks
- normal runtime execution does not rely on incidental host build behavior
- always-on tests cover the stable core without depending on optional host toolchains
