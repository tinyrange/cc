# ccx3 KVM arm64 plan

## Goal

Add native Linux `arm64` KVM support to `ccx3` on systems like this Raspberry Pi, while keeping the current product model intact:

- managed guest kernel
- image-backed long-lived VMs
- multi-exec session semantics
- unprivileged runtime behavior

The target is not “support every hypervisor abstraction from day one.” The target is:

- keep the current runtime semantics
- add a Linux `arm64` KVM backend cleanly
- avoid hard-wiring more backend assumptions into the VM layer
- borrow the useful parts of `../cc` without importing its architectural sprawl

## Current state

`ccx3` already has several pieces needed for this work:

- arm64 Linux boot planning in [internal/linux/boot/arm64/plan.go](/home/joshua/dev/projects/ccx3/internal/linux/boot/arm64/plan.go:1)
- device-tree building in [internal/fdt/build.go](/home/joshua/dev/projects/ccx3/internal/fdt/build.go:1)
- userspace MMIO devices in:
  - [internal/virtio/console.go](/home/joshua/dev/projects/ccx3/internal/virtio/console.go:1)
  - [internal/virtio/fs.go](/home/joshua/dev/projects/ccx3/internal/virtio/fs.go:1)
  - [internal/virtio/vsock.go](/home/joshua/dev/projects/ccx3/internal/virtio/vsock.go:1)
  - [internal/serial/uart8250.go](/home/joshua/dev/projects/ccx3/internal/serial/uart8250.go:1)

The main limitation is architectural:

- the runtime backend in [internal/vm/backend_darwin_arm64.go](/home/joshua/dev/projects/ccx3/internal/vm/backend_darwin_arm64.go:31) is wired directly to `hvf`
- host support in [internal/hv/hv.go](/home/joshua/dev/projects/ccx3/internal/hv/hv.go:8) is hardcoded to `darwin/arm64`
- the HVF container runner currently mixes:
  - product/session logic
  - arm64 machine layout
  - backend-specific execution/trap handling

## Guiding constraints

This work should preserve:

- the external `pull`, `run`, and `vm-start` model
- the current guest-init control flow
- the session-oriented VM semantics in `VISION.md`

This work should avoid:

- importing the full `../cc` hypervisor architecture into `ccx3`
- designing snapshot/register infrastructure before native KVM boot works
- duplicating the HVF runtime as a second monolithic KVM-specific container runner

## Reference use from `../cc`

Use these patterns as references:

- build-tagged backend selection such as [../cc/internal/hv/factory/factory_linux_arm64.go](/home/joshua/dev/projects/cc/internal/hv/factory/factory_linux_arm64.go:1)
- ARM64 KVM register setup and run-loop conventions in [../cc/internal/hv/kvm/kvm_arm64.go](/home/joshua/dev/projects/cc/internal/hv/kvm/kvm_arm64.go:33)
- vGIC probing and v3-to-v2 fallback in [../cc/internal/hv/kvm/kvm_arm64_vgic.go](/home/joshua/dev/projects/cc/internal/hv/kvm/kvm_arm64_vgic.go:31)
- guest-visible IRQ encoding separated from KVM injection details in [../cc/internal/hv/kvm/kvm_irq_arm64.go](/home/joshua/dev/projects/cc/internal/hv/kvm/kvm_irq_arm64.go:10)

Do not copy these mistakes:

- too-large generic hypervisor surfaces before the product needs them
- broad register/snapshot abstraction work on the critical path to initial boot support
- mixing backend-specific execution and product logic in the same file

## Desired end state

After this plan:

- `ccx3` supports `linux/arm64` hosts via native KVM
- the VM layer no longer depends directly on HVF-only runtime types
- arm64 machine layout is shared across backends
- host support is capability-based instead of hardcoded to Darwin
- KVM and HVF each implement the same narrow internal runtime seam

## Phase 1: extract a backend-neutral arm64 runtime seam

Refactor the current HVF-driven runtime so `vm` depends on shared arm64 runtime types instead of directly on HVF container types.

Concrete work:

- introduce shared arm64 runtime request/result types
- move arm64 machine layout constants out of the HVF package
- rewire the Darwin runtime backend to build shared requests
- keep HVF as the only implementation initially

Desired outcome:

- the first backend seam exists without changing runtime behavior
- the next KVM work can plug into a shared request shape instead of an HVF-specific one

## Phase 2: split arm64 machine assembly from backend execution

Refactor the current HVF container runner into clearer layers.

Concrete work:

- separate product/session behavior from machine bring-up
- centralize arm64 machine topology:
  - RAM base
  - UART
  - virtio MMIO layout
  - GIC-visible address map
- isolate backend-specific vCPU run/trap handling

Desired outcome:

- KVM can reuse machine bring-up and product logic without copying the HVF runner wholesale

## Phase 3: add Linux arm64 host capability detection

Replace host support hardcoding with backend-aware capability checks.

Concrete work:

- detect `linux/arm64` KVM availability
- verify `/dev/kvm` access
- probe required KVM capabilities
- surface clear unsupported/permission errors

Desired outcome:

- `vm.Supports()` reports real host capability instead of just checking `darwin/arm64`

## Phase 4: implement a minimal Linux arm64 KVM backend

Bring up a basic Linux arm64 VM path.

Concrete work:

- open `/dev/kvm`
- create VM and vCPUs
- map guest RAM
- load kernel, initrd, and DTB via the existing arm64 boot planner
- set initial PC, PSTATE, stack, and DTB argument registers
- boot to serial output using the existing UART device

Desired outcome:

- Linux arm64 KVM can boot the managed guest kernel to first serial output

## Phase 5: wire userspace devices and IRQ delivery into KVM

Make the current runtime devices usable on the KVM backend.

Concrete work:

- route KVM MMIO exits into:
  - UART
  - virtio-console
  - virtio-fs
  - virtio-vsock
- implement backend IRQ injection for arm64 KVM
- configure vGIC and support v3 with v2 fallback where needed

Desired outcome:

- the current runtime stack works over KVM without changing guest semantics

## Phase 6: make DT generation backend-informed

Stop hardcoding backend assumptions into the generic arm64 boot planner.

Concrete work:

- feed GIC version and address ranges into boot planning
- emit DT for v2 or v3 as required
- centralize IRQ numbering and device ranges in one machine description

Desired outcome:

- arm64 boot planning reflects actual backend capabilities

## Phase 7: integration coverage on Linux arm64

Add a staged test ladder for the new backend.

Concrete work:

- unit tests for machine-layout-to-DT translation
- unit tests for KVM IRQ encoding helpers
- integration test for kernel boot to serial
- integration test for ready marker detection
- integration test for one-shot `run`
- integration test for persistent VM plus multi-exec
- integration test for share mounting over virtio-fs

Desired outcome:

- Linux arm64 KVM is validated at both boot and product-semantic layers

## Recommended implementation order

1. shared arm64 runtime seam
2. arm64 machine assembly split
3. Linux arm64 capability detection
4. minimal KVM boot path
5. MMIO and IRQ integration
6. backend-informed DT generation
7. Linux arm64 integration coverage
