# Vision

`ccx3` is a cross-platform, unprivileged microVM runtime for running OCI-backed Linux workloads behind a simple HTTP API.

The project goal is not to expose raw VM plumbing. The goal is to let callers start from an OCI image, create an isolated execution environment, run many commands inside that environment, attach host-backed shares, manage networking, and snapshot or restore state, without ever having to provide a kernel, assemble a disk image, or rely on elevated host privileges.

At its core, the system is built around a small set of ideas:

- One managed Linux kernel per running VM.
- One OCI image environment bound to that VM.
- Multiple commands may execute inside that live environment over time.
- Networking, shares, and snapshots are first-class runtime features.
- The runtime remains entirely unprivileged to implement on Windows, macOS, and Linux hosts.

## Thesis

The thesis of `ccx3` is:

> Run OCI-derived Linux sandboxes in dedicated microVMs, with multi-exec session semantics, runtime-owned networking and shares, and first-class snapshots, through a portable HTTP API and without any privileged host dependencies.

This puts `ccx3` in a different category than both container runtimes and Firecracker-style low-level VMM APIs.

It is not a generic container runtime because the isolation boundary is a VM, not a container.

It is not just a thin Firecracker wrapper because the caller does not manage kernels, boot sources, block devices, or low-level machine configuration directly.

It is not merely a developer convenience layer because the same abstraction is intended to hold across Windows, macOS, and Linux.

## Product Shape

The user-facing model should stay workload-centric rather than hypervisor-centric.

The key resources are:

- `Image`: an OCI-derived execution environment template.
- `VM` or `Instance`: a live microVM bound to exactly one managed kernel and one image environment.
- `Exec`: a command launched inside that running VM.
- `Share`: a runtime-managed filesystem attachment backed by a host path or host file provider.
- `Network`: a runtime-managed virtual network attachment.
- `Snapshot`: a runtime-owned checkpoint of VM state.

The current codebase uses `kernel`, `image`, and `vm` terminology. That is a reasonable starting point. Over time, the external API may evolve toward more explicit workload-oriented names if that improves clarity, but the semantics should remain the same.

## Semantic Model

The fundamental execution model is:

- A caller provides an OCI image reference.
- The runtime prepares an image-backed Linux environment.
- The runtime boots a managed guest kernel inside a microVM.
- The VM becomes a long-lived execution environment.
- The caller may launch multiple commands inside that same VM over its lifetime.

This is not "one process and the VM dies." It is one VM per image-backed environment, with many exec operations inside it.

That split creates two distinct lifecycles.

### VM lifecycle

- Create
- Prepare image and guest environment
- Boot
- Become ready
- Accept exec requests
- Snapshot
- Stop
- Destroy

### Exec lifecycle

- Start command
- Stream stdout and stderr
- Accept stdin and signals
- Report exit status
- Detach or terminate

The VM is the isolation boundary. The image is the filesystem and runtime boundary. `exec` is an operation against an already-booted environment.

## Session Semantics

`ccx3` is designed around session-style semantics.

Successive execs in the same VM are expected to interact with the same live environment, subject to explicit policy. That means the system should be clear about what persists across execs:

- running processes
- writable filesystem state
- temporary files
- mounted shares
- current network attachments
- runtime-created state inside the guest

The default mental model should be:

> An instance is a live, image-backed Linux execution environment in a dedicated microVM. An exec is a process launched inside that environment. A snapshot freezes that environment for fast later restoration.

This is important because ambiguity here would make snapshots, background processes, and repeated execs hard to reason about.

## Unprivileged By Design

One of the defining properties of `ccx3` is that it is unprivileged to implement, not just unprivileged to use.

That means:

- No root or administrator requirement at install time.
- No privileged helper daemons.
- No dependence on elevated OS capabilities for instance creation, networking, sharing, snapshotting, or teardown.
- No hidden escape hatch where a privileged subsystem quietly performs the hard parts.

Every major subsystem should preserve this invariant:

- virtualization
- networking
- filesharing
- image preparation
- snapshotting
- lifecycle management

This constraint is not incidental. It is one of the project's core differentiators and should shape all technical decisions.

## Cross-Platform Contract

`ccx3` targets Windows, macOS, and Linux hosts.

Cross-platform should mean a stable control plane and stable semantics, not necessarily identical host implementation details. The caller should interact with the same resource model and nearly the same behavior on every host OS.

Where platforms differ, the runtime should expose capabilities explicitly rather than leaking backend details through inconsistent APIs.

Examples of platform-reported capabilities may include:

- supported snapshot classes
- supported network modes
- share consistency guarantees
- resource limit support
- performance characteristics

The API contract should remain portable even when a platform ships with a narrower capability set.

## OCI As Ingress, Not Identity

OCI support is central, but OCI should be treated as the ingress format for environments, not as the complete semantic model of the runtime.

OCI contributes:

- filesystem contents
- metadata defaults
- environment defaults
- command and entrypoint defaults

After that, the runtime owns the execution semantics.

In particular:

- the guest supervisor should be the stable control surface, not the image entrypoint
- the instance lifecycle should not be defined by container-runtime assumptions
- snapshots should describe runtime state, not just container state

This keeps the system aligned with microVM execution semantics instead of inheriting container abstractions that do not fit.

## API Direction

The HTTP API should be inspired by the simplicity of Firecracker's control plane, but it should remain workload-shaped rather than machine-plumbing-shaped.

Good top-level operations look like:

- pull an image
- create a VM from an image
- start a VM
- exec a command inside a VM
- inspect VM state
- attach shares
- attach or configure networking
- create a snapshot
- restore from a snapshot
- stop or destroy a VM

The caller should not need to think in terms of:

- kernel selection details
- boot source wiring
- raw guest disks
- low-level device topology
- host-specific privilege setup

The transport may stay close to Firecracker-style JSON over HTTP because that makes integration from many languages straightforward. The resource model, however, should be defined by `ccx3`, not copied directly from Firecracker.

## Networking And Shares

Networking and filesystem sharing should be runtime-owned abstractions, not thin wrappers over host-native privileged mechanisms.

For networking, the API should expose logical concepts such as:

- isolated networks
- VM attachment
- egress configuration
- DNS configuration
- port publication

For shares, the API should expose explicit semantics such as:

- read-only or read-write
- path mapping
- consistency guarantees
- snapshot participation

The important point is that callers interact with portable runtime objects, not host-specific mount or bridge mechanics.

## Snapshots

Snapshots are a first-class part of the product, not an add-on.

They should support several distinct use cases:

- boot snapshots for avoiding repeated cold boot
- warm snapshots after environment preparation
- workflow snapshots after partial application or runtime initialization

Snapshot correctness depends on explicit compatibility rules. At minimum, snapshot metadata should capture:

- guest kernel version
- guest supervisor version
- image identity or digest
- VM resource shape
- share and network configuration relevant to restore

Users should be able to reason clearly about what a snapshot preserves and under what conditions it can be restored safely.

## Initial Focus

A strong early version of `ccx3` should stay narrow and prove the core abstraction:

- Linux guest environment
- portable HTTP API
- one managed guest platform
- OCI pull and prepare
- create and start VM from image
- run multiple commands in one live VM
- stdout, stderr, stdin, signals, and exit status
- read-only and read-write shares
- basic runtime networking
- boot and warm snapshots

That is already enough to validate the product thesis without taking on every orchestration problem up front.

## Non-Goals

The project should avoid drifting into these traps early:

- exposing raw hypervisor internals as the main product surface
- copying container-runtime behavior where it conflicts with the VM session model
- building a full orchestration platform before the runtime contract is solid
- overfitting the API to a single host OS
- sacrificing the unprivileged invariant for convenience

## Why This Matters

There is a real gap between containers and low-level VMMs.

Many workloads want:

- stronger isolation than containers
- faster startup than traditional VM workflows
- OCI compatibility
- multiple commands in one reusable environment
- snapshot and restore
- portable host integration
- no admin rights

`ccx3` exists to fill that gap with a coherent execution substrate rather than a pile of VM plumbing.

The project vision can be stated simply:

> `ccx3` makes OCI-backed Linux sandboxes feel like a portable, high-level runtime primitive, while preserving microVM isolation, multi-exec session semantics, and a fully unprivileged implementation model.
