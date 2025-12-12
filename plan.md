OpenVMM reference base: ~/dev/org/openvmm

Checklist to bring devices toward OpenVMM parity (with reference implementations):
[ ] Device framework: add chipset-style builder for PIO/MMIO/PCI wiring, line sets, and lifecycle akin to `vmm_core/vmotherboard/src/base_chipset.rs` and `vmm_core/vmotherboard/src/chipset/builder/mod.rs` in ~/dev/org/openvmm.
[ ] Interrupt fabric: introduce shared line targets/EOI/poll plumbing similar to `vmm_core/vmotherboard/src/chipset/backing/arc_mutex/services.rs` and `vm/chipset_device/src/interrupt.rs`.
[ ] PIC: align DualPIC behavior (ELCR, stats, cascade sync, acknowledge hook) with `vm/devices/chipset/src/pic.rs`.
[ ] IOAPIC: implement MSI translation and remote-IRR/level semantics matching `vm/devices/chipset/src/ioapic.rs`.
[ ] PIT/timers: move to virtual-time/poll model with correct port 0x61 handling as in `vm/devices/chipset/src/pit.rs`.
[ ] i8042: replace stub with full controller (keyboard/mouse, A20 gate, reset trigger) modeled on `vm/devices/chipset/src/i8042/mod.rs`.
[ ] CMOS/RTC/PM: flesh out CMOS/RTC plus PM/battery/watchdog/PSP wiring following `vm/devices/chipset/src/cmos_rtc.rs` and power-related devices in `vmm_core/vmotherboard/src/base_chipset.rs`.
[ ] Serial: add 16550 MMIO/IO variants with buffered async I/O and OUT2 gating like `vm/devices/serial/serial_16550/src/lib.rs`.
[ ] Virtio core: factor common queue/device logic, add PCI transport, doorbells, and broaden devices using `vm/devices/virtio/virtio/src/common.rs`, `.../transport/mmio.rs`, and `.../transport/pci.rs` as guides.
[ ] Snapshot/inspect: implement uniform save/restore and inspect hooks mirroring the patterns in `vm/chipset_device/src/lib.rs` and device Inspect implementations (e.g., `vm/devices/chipset/src/pic.rs`).
[ ] Graphics/input extras: plan VGA/framebuffer/input parity referencing `vm/devices/vga` and `vm/devices/framebuffer` directories.
[ ] Tests/bringup: add focused device tests and a bringup quest similar in spirit to `vm/devices/virtio/virtio/src/tests.rs` and OpenVMM’s `cmd/quest` equivalent.
