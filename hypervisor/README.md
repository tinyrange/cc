# Caller-constructed ARM64 machines

`hypervisor.NewARM64(ctx)` exposes native execution without imposing a kernel,
firmware, disk-image format or host-path convention. The initial implementation
uses macOS Hypervisor.framework on Apple Silicon with its native GIC. It builds
with `CGO_ENABLED=0`; an entitled executable is required.

1. Map caller-selected RAM ranges with `MapRAM` (16 KiB alignment on HVF).
2. Fill the returned bytes and `Restore` an `ARM64State` containing general,
   SIMD, PC/PSTATE, floating-point control and architectural system registers.
3. Call `Run(ctx)`, inspect `Exit` and dispatch only the devices/services that
   the caller has installed. `EmulateMMIO` implements AArch64 load/store abort
   register semantics, including sign extension and XZR.
4. Close the VM when finished. RAM slices become invalid on close.

Register indices 0–30 select X0–X30; use `RegisterPC`, `RegisterPState`,
`RegisterFPCR` and `RegisterFPSR` for the other exposed registers. System
registers use architectural op0:op1:CRn:CRm:op2 encodings. Unsupported host
registers return errors. `Restore` does not claim to restore GIC, device,
virtual-counter offsets or a complete migration snapshot.

Methods must be serialized, except `Cancel`, which may interrupt `Run` from
another goroutine. Device interrupts can produce `ExitCanceled`; callers should
resume these when their own context is still live. Exception metadata is valid
only for `ExitException`. `Counter` reports HVF's virtual-counter clock and Hz.

`NewNVMePCI` binds caller-owned random-access storage to an ECAM bus and one
32-bit memory BAR. Interrupt numbers are architectural GIC IDs. The caller must
provide matching firmware declarations and dispatch the returned MMIO device.
`devices/ramfb` is a separate portable fw_cfg DMA device using a caller-provided
physical-memory interface and the public `display.FramebufferUpdate` format.

`AddInputPCI` adds modern virtio keyboard or absolute-pointer functions to that
bus before guest enumeration. Its `InputDevice` accepts Linux key codes, guest
pixel coordinates, buttons and wheel events. The transport uses guest-owned
split virtqueues and INTx; firmware must describe each function's interrupt.
`Stats` gives a bounded view of queue readiness and event consumption.

`display/gowin` is a native presenter for any `display.Session`, using
`github.com/tinyrange/gowin` with cgo disabled. Start it on the initial OS thread.
`display.StartRunner` adapts a cooperatively interruptible VM to a display
session whose requests serialize with execution on a worker. The VM continues
running during rendering; snapshots must own their returned pixel data.

The optional Windows debug handling and UEFI runtime policy live in trex, not
in this package. The low-level backend provides debug trapping and architectural
BRK delivery without knowing a guest operating system.

The entitled integration test exercises general/SIMD state restoration, the
native cycle counter and debug exception delivery. Build the test executable
with cgo disabled, sign with `tools/entitlements.xml`, and run
`CC_HVF_TEST=1 <test-executable> -test.run TestARM64Restore`.
