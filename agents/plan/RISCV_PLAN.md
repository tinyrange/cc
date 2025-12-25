# RISC-V Emulator Integration Plan

## Objective

Make the `local/ccvm` RISC-V64 emulator a selectable backend under `internal/hv/riscv`, keeping the existing interpreter largely intact. The compiler/assembler lives in a separate codebase for now. Initial focus: add RISC-V versions of the early `cmd/quest` steps; Linux/Alpine bringup follows after quest parity.

## Steps

- **Baseline constraints**
  - Treat `ccvm` interpreter as a near-unchanged engine; wrap/plug it into `internal/hv/riscv` instead of refactoring core decode/execute right away.
- **Quest parity first**
  - Create RISC-V variants of the initial `cmd/quest` steps (mirroring existing flow) and wire them to run on the `ccvm` backend.
  - Use a minimal assembler/IR sufficient for these steps; expand it incrementally as quests grow.
- **Device integration**
  - Replace `ccvm` virtio devices with project versions; start with virtio-console and stub out block/net until ready.
  - Ensure IRQ plumbing matches PLIC/CLINT expectations and console I/O routes through existing abstractions.
- **Firmware/kernel handling**
  - Embed the system firmware for the Linux bringup.
  - Plan to fetch Alpine RISCV64 kernel/modules for full bringup; keep a tiny initramfs for quick tests.
- **Memory and I/O safety**
  - Add bounds checks for block I/O and tighten descriptor validation in virtio paths; keep logging gated but useful.
- **Backend wiring**
  - Expose `hv/riscv` backend selection and configuration flags. This should be exposed via `factory.NewWithArchitecture`.
- **Testing and validation**
  - Add unit tests for decode/CSR/device glue; integrate with `./tools/build.go -test`.
  - Work on linux kernel bring-up including expanding the ir and assembler for all features required by `initx`.
