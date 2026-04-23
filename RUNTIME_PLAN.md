# Runtime Plan

## Goal

Build a backend-neutral guest runtime layer that can be reused across:

- `linux/arm64`
- `linux/amd64`
- `windows/amd64`
- `windows/arm64`

without forcing non-arm64 backends through arm64 boot or device assumptions.

## Boundaries

### `internal/vmruntime`

Backend-neutral and architecture-neutral logic only:

- guest protocol markers
- transcript buffering and waiting
- boot event streaming
- boot failure detection and init timing parsing
- guest init config assembly that does not assume arm64 boot machinery
- shared runtime request/result types once they no longer depend on arm64 packages
- startup coordination helpers that operate on small backend interfaces

### `internal/arm64vm`

Arm64-specific logic only:

- kernel image boot layout
- FDT generation
- PSCI/GIC/timer description
- arm64 memory map constants
- arm64-specific wrappers around backend-neutral runtime helpers where needed

### backend packages

Backend-specific logic only:

- VM creation/destruction
- register programming
- interrupt injection
- run-loop slicing
- MMIO/exit translation into shared device handlers

## Implementation Steps

1. Extract backend-neutral helpers from `arm64vm` into `vmruntime`. Done.
2. Keep `arm64vm` as thin wrappers/aliases where arm64 call sites still depend on old names. Done.
3. Move shared runtime request/result and guest-init config types into `vmruntime` once references are untangled. Done.
4. Extract common startup orchestration:
   - initramfs construction
   - device assembly planning
   - transcript and ready-marker handling
   - vsock control acceptance
5. Define a small backend interface for startup coordination:
   - map memory
   - set initial registers
   - run one slice
   - dispatch device MMIO
6. Repoint HVF and KVM to the shared coordinator.
7. Add amd64-specific boot package separately rather than expanding `arm64vm`.

## Non-Goals

- No fake HTTP/control abstraction
- No single “universal VM” interface that bakes in arm64 register or FDT assumptions
- No forcing future amd64 backends through arm64 startup structures

## Current Status

- Shared virtio/serial devices already exist.
- Runtime protocol markers, transcript buffering, boot event streaming, env merging, guest-init config, initramfs construction, and request/result/share types now live in `internal/vmruntime`.
- `internal/arm64vm` now keeps arm64 boot layout/FDT/device helpers plus compatibility wrappers for arm64 call sites.
- HVF and KVM now share more initramfs/rootfs/vsock assembly without routing future amd64 or Windows backends through arm64 boot assumptions.
- Remaining duplication is mainly startup coordination and backend run-loop plumbing.
