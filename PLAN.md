# Linux AMD64 Support Plan

## Goal

Add first-class `linux/amd64` host support to the VM runtime.

The codebase currently supports VM execution on:

- `darwin/arm64` via HVF
- `linux/arm64` via KVM

Everything else is routed to the unsupported backend through build tags. The target outcome is that an x86_64 Linux host with `/dev/kvm` can download the matching Alpine kernel, boot the guest runtime, mount images and shares, and run commands through the same public `cc` / `ccvm` APIs used by the existing backends.

## Current State

The main platform split is build-tag based:

- `internal/vm/backend_darwin_arm64.go` implements the Darwin ARM64 HVF runtime.
- `internal/vm/backend_linux_arm64.go` implements the Linux ARM64 KVM runtime.
- `internal/vm/backend_other.go` marks all other host platforms unsupported.
- `internal/hv/hv_linux_arm64.go` probes KVM only for Linux ARM64.
- `internal/hv/hv_other.go` marks all other hosts unsupported.

Some pieces are already partly architecture-aware:

- `internal/kernel/alpine/kernel.go` maps `GOARCH=amd64` to Alpine `x86_64`.
- OCI manifest selection prefers the native architecture and only adds `amd64` as a fallback on ARM64.
- The virtio filesystem, vsock, image filesystem, initramfs, OCI, and API layers are mostly host-architecture neutral.

The main blockers are:

- Guest init is always built or embedded as `linux/arm64`.
- The KVM implementation is strongly ARM64-specific.
- Boot planning only exists for ARM64 Linux images and device trees.
- The Linux runtime backend directly depends on `internal/arm64vm`.

## Implementation Plan

### 1. Add Architecture-Aware Guest Init

Change guest init from a hard-coded ARM64 artifact into a target-specific artifact.

Tasks:

- Update `internal/guestinit` so `Build` chooses the guest init binary for the current guest architecture.
- Add an embedded `guest-init-linux-amd64` payload alongside `guest-init-linux-arm64`.
- Update fallback builds to use `GOOS=linux` and the selected `GOARCH`, not always `GOARCH=arm64`.
- Update `tools/build.sh` to build and install both guest init payloads.
- Add tests or compile checks that verify the selected payload is an ELF for the intended architecture.

This should be the first milestone because an amd64 guest cannot boot an ARM64 `/init`.

### 2. Enable Linux AMD64 Host Support Gates

Add host support files for `linux/amd64`.

Tasks:

- Add `internal/hv/hv_linux_amd64.go` with KVM probing.
- Update `internal/hv/hv_other.go` build tags so `linux/amd64` is no longer unsupported.
- Add `internal/vm/backend_linux_amd64.go`.
- Update `internal/vm/backend_other.go` build tags so `linux/amd64` uses the real backend.
- Keep user-facing errors clear when `/dev/kvm` is missing, inaccessible, or unavailable under nested virtualization.

At this stage, the code should compile for `GOOS=linux GOARCH=amd64`, even if the runtime is not fully functional yet.

### 3. Introduce an AMD64 VM Layout Package

Create a package equivalent in role to `internal/arm64vm`, for example `internal/amd64vm`.

Tasks:

- Define guest memory base and default memory size.
- Define kernel, initrd, boot params, command line, and stack placement rules.
- Define serial device placement.
- Define virtio-mmio windows for rootfs, vsock, shares, and optional console.
- Reuse `vmruntime.RunRequest`, `vmruntime.DirectoryShare`, and existing virtio device types where possible.
- Port filesystem-device assembly helpers from `arm64vm` to an architecture-neutral layer if duplication becomes too high.

The first pass can duplicate small ARM64 helper shapes for clarity, then refactor common pieces after amd64 works.

### 4. Implement X86_64 Linux Boot Planning

Add an x86_64 boot package, likely `internal/linux/boot/amd64`.

Tasks:

- Parse or validate the Linux x86 boot protocol from the Alpine `vmlinuz-virt` bzImage.
- Build the zero page / boot params structure.
- Populate E820 memory map entries.
- Place kernel, initrd, command line, and boot params in guest memory.
- Set initrd metadata for the kernel.
- Build a command line with `rdinit=/init`, panic behavior, optional serial console flags, and virtio-mmio device descriptors.
- Add unit tests for layout, command line generation, initrd bounds, E820 entries, and invalid images.

Unlike ARM64, x86_64 should not use FDT nodes. The virtio-mmio devices should be described using kernel command-line arguments such as `virtio_mmio.device=...`.

### 5. Add Linux AMD64 KVM Support

Add amd64-specific files under `internal/hv/kvm`.

Tasks:

- Add KVM ABI structs for x86 registers, special registers, CPUID, and exits.
- Implement VM creation, memory mapping, VCPU creation, CPUID setup, and register setup for x86_64.
- Support `KVM_EXIT_MMIO` for virtio-mmio devices.
- Support `KVM_EXIT_IO` for legacy serial port handling, unless an MMIO serial strategy is chosen.
- Configure the in-kernel IRQ chip and any PIT/PIC/APIC setup needed for interrupts.
- Implement IRQ injection for virtio and serial devices.
- Keep the public KVM session functions parallel to the ARM64 implementation where practical.

The ARM64 KVM implementation relies on VGIC, one-reg access, and device trees, so this should be implemented as sibling amd64 files rather than by widening existing ARM64 build tags.

### 6. Port Runtime Backend Behavior

Bring the Linux AMD64 runtime to parity with the Linux ARM64 backend.

Tasks:

- Implement `Start`, `StartBlank`, `Run`, and `RunInInstance`.
- Build initramfs using the amd64 guest init payload.
- Plan and include required Alpine modules.
- Build rootfs and share virtio devices.
- Boot to the ready marker.
- Support one-shot command execution.
- Support persistent managed sessions through vsock.
- Support image mounts into an already-running instance.
- Preserve existing environment, working directory, root-user validation, and command resolution behavior.

The first implementation can mirror `backend_linux_arm64.go`, then factor out common Linux backend code after both backends are working.

### 7. Clarify Image Architecture Behavior

Define initial image architecture support explicitly.

Initial scope:

- On `linux/amd64`, run native `amd64` images directly.
- Reject or clearly error on non-amd64 guest images unless explicit emulation support is added.
- Keep the current ARM64 host behavior where amd64 images can use `qemu-x86_64`.

Follow-up option:

- Generalize `NeedsAMD64Emulation` into a broader foreign-architecture emulation layer.
- Add `qemu-aarch64` support later if ARM64 images need to run on amd64 hosts.

### 8. Add Tests and CI Coverage

Layer the verification so problems are easy to isolate.

Tests:

- `GOOS=linux GOARCH=amd64 go test ./...` compile coverage.
- Unit tests for `internal/linux/boot/amd64`.
- Unit tests for guest init target selection.
- KVM smoke test that probes `/dev/kvm`.
- Boot-to-serial-marker integration test.
- Boot-to-ready-marker integration test.
- One-shot command execution test.
- Root filesystem mount test.
- Share mount test.
- Vsock managed exec test.
- Persistent instance exec test.

CI:

- Add cross-compile jobs for `linux/amd64`, `linux/arm64`, and `darwin/arm64` where possible.
- Gate live KVM tests behind an explicit environment variable or runner label.
- Document local test requirements for `/dev/kvm`, permissions, and nested virtualization.

## Milestones

### Milestone 1: Compile Support

Expected outcome:

- `GOOS=linux GOARCH=amd64 go test ./...` compiles.
- Guest init selection supports amd64.
- Unsupported build tags no longer block `linux/amd64`.
- Runtime can return a clear "not implemented yet" error for incomplete KVM paths if needed.

### Milestone 2: Minimal Boot

Expected outcome:

- An x86_64 Alpine kernel and amd64 initramfs boot under KVM.
- Serial output reaches a known marker.
- No rootfs or vsock support is required yet.

### Milestone 3: Filesystem and One-Shot Exec

Expected outcome:

- Rootfs virtio-mmio device works.
- Init mounts the rootfs.
- `cc run <image> <cmd...>` works for an amd64 Linux image.

### Milestone 4: Managed Sessions

Expected outcome:

- Vsock control channel works.
- `vm-start`, `exec`, and `vm-stop` work.
- Shares and image mounts work in a running instance.

### Milestone 5: Product Polish

Expected outcome:

- User-facing errors explain KVM and platform requirements.
- Documentation describes supported hosts and image architecture behavior.
- CI includes compile coverage and optional live KVM integration coverage.
- Common Linux backend code is refactored only where it reduces maintenance risk.

## Risks and Open Questions

- X86_64 boot protocol support is the largest new area and should be implemented with focused unit tests.
- Interrupt delivery for virtio-mmio on x86 may require more infrastructure than the ARM64 VGIC path.
- Serial can be implemented through legacy I/O ports or MMIO 8250; the simpler and more reliable option should be validated early.
- Alpine module paths may differ between ARM64 and x86_64 packages, so module planning should be verified against the downloaded `linux-virt` package.
- The current Linux ARM64 backend supports only one CPU; amd64 should probably start with one CPU too, then add SMP later.
- Cross-architecture image support should not be expanded accidentally. Native amd64 first, emulation later.
