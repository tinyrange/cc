# Device Framework Implementation Plan

Reference: OpenVMM codebase at `~/dev/org/openvmm`

---

## 1. Device Framework (Chipset Builder Pattern)

**Goal**: Add a unified chipset builder similar to OpenVMM's `ChipsetBuilder` that manages device registration, PIO/MMIO/PCI wiring, and interrupt line allocation.

**Reference Files**:
- `vmm_core/vmotherboard/src/chipset/builder/mod.rs` - ChipsetBuilder pattern
- `vmm_core/vmotherboard/src/base_chipset.rs` - Base chipset device trait

**Current State** (cc):
- Devices are registered individually via `vm.AddDevice(dev)`
- Dispatch handled in `internal/hv/kvm/kvm_amd64.go` by type assertion
- No unified device registry or builder

### Implementation Checklist

- [x] **Create `internal/chipset/device.go`** - Define unified device interface
  - [x] Add `ChipsetDevice` interface combining PIO, MMIO, Poll capabilities
  - [x] Add `SupportsPortIO() *PortIOIntercept` method
  - [x] Add `SupportsMmio() *MmioIntercept` method
  - [x] Add `SupportsPollDevice() *PollDevice` method
  - [x] Add `ChangeDeviceState` interface (Start, Stop, Reset)

- [x] **Create `internal/chipset/builder.go`** - Chipset builder
  - [x] Add `ChipsetBuilder` struct with device registry
  - [x] Add `RegisterDevice(name string, dev ChipsetDevice)` method
  - [x] Add `WithPioPort(port uint16, handler PortIOHandler)` method
  - [x] Add `WithMmioRegion(base, size uint64, handler MmioHandler)` method
  - [x] Add `WithInterruptLine(line uint8, sink InterruptSink)` method
  - [x] Add `Build() (*Chipset, error)` to finalize registration
  - [x] Add port/region conflict detection

- [x] **Create `internal/chipset/chipset.go`** - Runtime chipset
  - [x] Add `Chipset` struct holding device map and dispatch tables
  - [x] Add `HandlePIO(port uint16, data []byte, isWrite bool) error`
  - [x] Add `HandleMMIO(addr uint64, data []byte, isWrite bool) error`
  - [x] Add `Poll(ctx context.Context)` for poll-based devices
  - [x] Add `Start()`, `Stop()`, `Reset()` lifecycle methods

- [x] **Update `internal/hv/kvm/kvm_amd64.go`**
  - [x] Replace manual device dispatch with chipset dispatch
  - [x] Update `addX86Devices()` to use builder pattern

---

## 2. Interrupt Fabric (Line Targets, EOI, Poll)

**Goal**: Implement shared interrupt line infrastructure with EOI callback support, matching OpenVMM's `LineInterrupt` and `LineSet` patterns.

**Reference Files**:
- `vm/chipset_device/src/interrupt.rs` - LineInterrupt trait
- `vmm_core/vmotherboard/src/chipset/backing/arc_mutex/services.rs` - Interrupt services

**Current State** (cc):
- `internal/devices/amd64/chipset/sink.go` defines `readySink` and `irqLine` interfaces
- Devices connect via callback functions
- No EOI broadcast mechanism

### Implementation Checklist

- [x] **Enhance `internal/devices/amd64/chipset/sink.go`**
  - [x] Add `LineInterrupt` interface:
    ```go
    type LineInterrupt interface {
        SetLevel(high bool)
        PulseInterrupt()  // For edge-triggered
    }
    ```
  - [x] Add `LineInterruptDetached()` factory for noop line
  - [x] Add `LineInterruptFromFunc(fn func(bool)) LineInterrupt` adapter

- [x] **Create `internal/chipset/lineset.go`** - Interrupt line set
  - [x] Add `LineSet` struct managing multiple interrupt lines
  - [x] Add `AllocateLine(irq uint8) LineInterrupt` method
  - [x] Add `RegisterEOICallback(line uint8, fn func())` for EOI notification
  - [x] Add `BroadcastEOI(vector uint8)` method called from LAPIC
  - [x] Wire EOI to IOAPIC's `HandleEOI(vector uint32)` method

- [x] **Update IOAPIC** (`internal/devices/amd64/chipset/ioapic.go`)
  - [x] Expose `HandleEOI(vector uint32)` publicly (already exists)
  - [x] Add stats for EOI events

- [x] **Update PIC** (`internal/devices/amd64/chipset/pic.go`)
  - [x] Add `LineInterrupt` output for INT pin
  - [x] Add `AcknowledgeHook` interface for in-kernel PIC integration

- [ ] **Update KVM integration** (`internal/hv/kvm/kvm_irq.go`)
  - [x] Wire LAPIC EOI to `LineSet.BroadcastEOI()`
  - [ ] Add KVM IRQFD support for fast interrupt injection (optional)

---

## 3. PIC Enhancements

**Goal**: Align DualPIC with OpenVMM's `Dual8259Pic` behavior including ELCR stats, cascade sync, and acknowledge hooks.

**Reference Files**:
- `vm/devices/chipset/src/pic.rs` - Dual8259Pic implementation

**Current State** (cc):
- `internal/devices/amd64/chipset/pic.go` has working cascaded 8259A
- Missing: poll mode, rotate priority, special mask mode stats

### Implementation Checklist

- [x] **Update `internal/devices/amd64/chipset/pic.go`**
  - [x] Add `picStats` struct:
    ```go
    type picStats struct {
        spuriousInterrupts uint64
        acknowledges       uint64
        perIRQ             [8]uint64
    }
    ```
  - [x] Track spurious IRQ7/IRQ15 delivery
  - [x] Add per-line interrupt counters
  - [x] Implement rotate-on-auto-EOI (OCW2 R=1, SL=0, EOI=1)
  - [x] Implement special mask mode (OCW3 SMM/ESMM bits)
  - [x] Add poll command implementation (OCW3 P bit) - partially done
  - [x] Add `Init(vm hv.VirtualMachine) error` with reset logic
  - [x] Add snapshot/restore support (`DeviceSnapshotter` interface)

---

## 4. IOAPIC Enhancements

**Goal**: Improve IOAPIC to match OpenVMM's implementation with proper level-triggered semantics and remote-IRR handling.

**Reference Files**:
- `vm/devices/chipset/src/ioapic.rs` - IoApic implementation

**Current State** (cc):
- `internal/devices/amd64/chipset/ioapic.go` is mostly complete
- Has remote-IRR, level/edge detection
- Missing: destination shorthand, delivery status bit, stats per delivery mode

### Implementation Checklist

- [x] **Update `internal/devices/amd64/chipset/ioapic.go`**
  - [x] Add delivery mode statistics:
    ```go
    type ioapicStats struct {
        interrupts     uint64
        perIRQ         []uint64
        fixedDelivery  uint64
        lowPriDelivery uint64
        smiDelivery    uint64
        nmiDelivery    uint64
        initDelivery   uint64
        extintDelivery uint64
    }
    ```
  - [x] Add delivery status bit (bit 12) read support
  - [x] Implement polarity inversion (bit 13) for active-low
  - [x] Add destination shorthand handling for broadcast
  - [x] Fix level-triggered re-assert after EOI when line still high
  - [x] Add `Debug()` method for inspection
  - [x] Verify remote-IRR clear on write (should be read-only)

- [x] **Add unit tests** (`internal/devices/amd64/chipset/ioapic_test.go`)
  - [x] Test edge-triggered interrupt delivery
  - [x] Test level-triggered with remote-IRR
  - [x] Test mask/unmask behavior
  - [x] Test EOI clearing remote-IRR
  - [x] Test redirection entry programming

---

## 5. PIT/Timers (Virtual Time/Poll Model)

**Goal**: Move PIT to virtual-time model with poll-based callbacks, implement port 0x61 speaker gate properly.

**Reference Files**:
- `vm/devices/chipset/src/pit.rs` - ProgrammableIntervalTimer

**Current State** (cc):
- `internal/devices/amd64/chipset/pit.go` uses Go `time.Ticker`
- Port 0x61 partially implemented
- Missing: latch status, readback, proper oneshot

### Implementation Checklist

- [x] **Update `internal/devices/amd64/chipset/pit.go`**
  - [x] Implement `PollDevice` interface for timer callbacks
  - [x] Add `VmTime` abstraction:
    ```go
    type VmTime interface {
        Now() time.Duration
        SetTimeout(deadline time.Duration)
        CancelTimeout()
    }
    ```
  - [x] Implement mode 0 (interrupt on terminal count)
  - [x] Implement mode 2 (rate generator) - periodic pulse
  - [x] Implement mode 3 (square wave generator) - 50% duty cycle
  - [x] Implement mode 4 (software triggered strobe) - one-shot pulse
  - [x] Add readback command (0xC2 on port 0x43)
  - [x] Add status latch command
  - [x] Fix counter latch for 16-bit reads

- [x] **Implement Port 0x61** (`internal/devices/amd64/chipset/port61.go`)
  - [x] Create `Port61` device struct
  - [x] Add timer 2 gate control (bit 0)
  - [x] Add speaker data control (bit 1)
  - [x] Add refresh request status (bit 4) - toggle for timing loops
  - [x] Add timer 2 output status (bit 5)
  - [x] Wire to PIT channel 2

- [x] **Add unit tests** (`internal/devices/amd64/chipset/pit_timer_test.go`)
  - [x] Test mode 2 periodic timer
  - [x] Test mode 0 one-shot
  - [x] Test counter latch and readback
  - [x] Test port 0x61 integration

---

## 6. i8042 Keyboard Controller

**Goal**: Replace stub i8042 with full controller supporting keyboard/mouse input.

**Reference Files**:
- `vm/devices/chipset/src/i8042/mod.rs` - I8042Device
- `vm/devices/chipset/src/i8042/ps2keyboard.rs` - PS/2 keyboard
- `vm/devices/chipset/src/i8042/spec.rs` - Controller commands

**Current State** (cc):
- `internal/devices/amd64/input/i8042.go` is a minimal stub
- Handles A20 gate and CPU reset
- No keyboard scancode translation

### Implementation Checklist

- [x] **Enhance `internal/devices/amd64/input/i8042.go`**
  - [x] Add controller state machine with full state tracking
  - [x] Implement controller commands (0x20-0xFF):
    - [x] 0x20: Read command byte
    - [x] 0x60: Write command byte
    - [x] 0xA7/0xA8: Disable/enable aux interface
    - [x] 0xA9: Test aux interface
    - [x] 0xAA: Self-test (return 0x55)
    - [x] 0xAB: Test keyboard interface
    - [x] 0xAD/0xAE: Disable/enable keyboard
    - [x] 0xC0: Read input port
    - [x] 0xD0: Read output port
    - [x] 0xD1: Write output port (A20/reset)
    - [x] 0xD4: Write to aux device
    - [x] 0xFE: Pulse reset (CPU reset)
  - [x] Add output buffer state management (empty/controller/keyboard/mouse)
  - [x] Add command byte (interrupts enable, translate, disable flags)
  - [x] Implement ChipsetDevice interface
  - [x] Wire interrupts to LineInterrupt for IRQ1/IRQ12

- [x] **Create `internal/devices/amd64/input/ps2keyboard.go`**
  - [x] Add PS/2 keyboard state machine
  - [x] Implement keyboard commands (0xFF reset, 0xED LEDs, 0xF0 scancode set, etc.)
  - [x] Add scancode set 1 to set 2 translation
  - [x] Add key event handling (SendKey method)
  - [x] Wire to `LineInterrupt` for IRQ1

- [ ] **Create `internal/devices/amd64/input/ps2mouse.go`** (optional)
  - [ ] Add PS/2 mouse protocol
  - [ ] Wire to `LineInterrupt` for IRQ12

- [x] **Add unit tests**
  - [x] Test self-test command
  - [x] Test A20 gate control
  - [x] Test keyboard enable/disable
  - [x] Test scancode translation

---

## 7. CMOS/RTC/PM

**Goal**: Enhance CMOS RTC with proper timer support and add PM/battery/watchdog stubs.

**Reference Files**:
- `vm/devices/chipset/src/cmos_rtc.rs` - Rtc implementation

**Current State** (cc):
- `internal/devices/amd64/chipset/cmos.go` has basic RTC
- Missing: periodic/alarm interrupts, proper BCD, timer polling

### Implementation Checklist

- [x] **Update `internal/devices/amd64/chipset/cmos.go`**
  - [x] Add Status Register A:
    - [x] Periodic timer rate (bits 0-3)
    - [x] Oscillator control (bits 4-6)
    - [x] Update in progress (bit 7) - pulse every second
  - [x] Add Status Register B:
    - [x] DST enable (bit 0)
    - [x] 24-hour mode (bit 1)
    - [x] BCD disable (bit 2)
    - [x] Square wave enable (bit 3)
    - [x] Update IRQ enable (bit 4)
    - [x] Alarm IRQ enable (bit 5)
    - [x] Periodic IRQ enable (bit 6)
    - [x] Set mode (bit 7)
  - [x] Add Status Register C (interrupt flags, read clears)
  - [x] Add Status Register D (VRT bit = 1 always)
  - [x] Implement periodic timer using `PollDevice`:
    ```go
    func (r *Rtc) PollDevice(ctx context.Context) {
        // Check periodic timer
        // Check alarm timer
        // Check update timer (1 second)
    }
    ```
  - [x] Implement alarm with wildcard support (0xFF = don't care)
  - [x] Add BCD encode/decode functions
  - [x] Add 12/24 hour conversion
  - [x] Wire IRQ8 to `LineInterrupt`

- [x] **Create `internal/devices/amd64/chipset/pm.go`** (stub)
  - [x] Add ACPI PM1a control/status registers
  - [x] Add PM timer (port 0x408)
  - [x] Add sleep state handling (stub)

- [x] **Add unit tests** (`internal/devices/amd64/chipset/cmos_test.go`)
  - [x] Test time register read/write
  - [x] Test BCD encoding
  - [x] Test alarm matching
  - [x] Test status register C clear-on-read

---

## 8. Serial 16550

**Goal**: Add MMIO variant and async buffered I/O for Serial16550.

**Reference Files**:
- `vm/devices/serial/serial_16550/src/lib.rs` - Serial16550

**Current State** (cc):
- `internal/devices/amd64/serial/serial.go` has PIO 16550
- Missing: MMIO transport, proper FIFO trigger levels, modem status

### Implementation Checklist

- [x] **Update `internal/devices/amd64/serial/serial.go`**
  - [x] Add FIFO trigger level support (1/4/8/14 bytes)
  - [x] Add modem control register (MCR):
    - [x] DTR (bit 0)
    - [x] RTS (bit 1)
    - [x] OUT1 (bit 2)
    - [x] OUT2 (bit 3) - interrupt gate
    - [x] Loopback (bit 4)
  - [x] Add modem status register (MSR):
    - [x] CTS change (bit 0)
    - [x] DSR change (bit 1)
    - [x] RI trailing edge (bit 2)
    - [x] DCD change (bit 3)
    - [x] CTS (bit 4)
    - [x] DSR (bit 5)
    - [x] RI (bit 6)
    - [x] DCD (bit 7)
  - [x] Implement OUT2 interrupt gating (interrupt = pending && OUT2)
  - [x] Add `PollDevice` interface for async TX/RX
  - [x] Add RX/TX statistics
  - [x] Convert to ChipsetDevice interface

- [ ] **Create `internal/devices/serial/mmio.go`**
  - [ ] Add `Serial16550MMIO` wrapper
  - [ ] Implement `MMIORegions()` for configurable base/size
  - [ ] Add register width parameter (1/2/4 byte stride)
  - [ ] Map MMIO offsets to register indices

- [ ] **Add unit tests**
  - [ ] Test THR/RHR with FIFO
  - [ ] Test interrupt generation
  - [ ] Test modem control loopback
  - [ ] Test MMIO access patterns

---

## 9. Virtio Core

**Goal**: Factor common virtio queue logic, add PCI transport alongside MMIO.

**Reference Files**:
- `vm/devices/virtio/virtio/src/common.rs` - VirtioQueue, VirtioDevice
- `vm/devices/virtio/virtio/src/transport/mmio.rs` - VirtioMmioDevice
- `vm/devices/virtio/virtio/src/transport/pci.rs` - VirtioPciDevice

**Current State** (cc):
- `internal/devices/virtio/mmio.go` has MMIO transport
- `internal/devices/virtio/console.go` has virtio console
- Missing: PCI transport, common queue abstraction

### Implementation Checklist

- [ ] **Create `internal/devices/virtio/queue.go`** - Common queue logic
  - [ ] Add `VirtQueue` struct:
    ```go
    type VirtQueue struct {
        DescTableAddr  uint64
        AvailRingAddr  uint64
        UsedRingAddr   uint64
        Size           uint16
        Enabled        bool
        NotifyEvent    chan struct{}
    }
    ```
  - [ ] Add `VirtQueueDescriptor` struct (addr, len, flags, next)
  - [ ] Add `ReadDescriptor(mem GuestMemory, idx uint16) Descriptor`
  - [ ] Add `GetAvailableBuffer() (uint16, []VirtQueuePayload, bool)`
  - [ ] Add `PutUsedBuffer(idx uint16, len uint32)`
  - [ ] Add interrupt suppression (VIRTQ_USED_F_NO_NOTIFY)

- [ ] **Create `internal/devices/virtio/device.go`** - Device interface
  - [ ] Add `VirtioDevice` interface:
    ```go
    type VirtioDevice interface {
        DeviceID() uint16
        DeviceFeatures() uint64
        MaxQueues() uint16
        ReadConfig(offset uint16) uint32
        WriteConfig(offset uint16, val uint32)
        Enable(features uint64, queues []*VirtQueue)
        Disable()
    }
    ```

- [ ] **Update `internal/devices/virtio/mmio.go`**
  - [ ] Factor out common transport logic
  - [ ] Use new `VirtQueue` abstraction
  - [ ] Add config generation increment on feature negotiation
  - [ ] Add interrupt status register (used buffer/config change)

- [ ] **Create `internal/devices/virtio/pci.go`** - PCI transport
  - [ ] Add `VirtioPciDevice` struct
  - [ ] Implement PCI config space (vendor=0x1AF4, device=0x1000+device_id)
  - [ ] Add virtio PCI capabilities:
    - [ ] Common config (cap type 1)
    - [ ] Notify config (cap type 2)
    - [ ] ISR config (cap type 3)
    - [ ] Device config (cap type 4)
  - [ ] Add BAR0 for MMIO registers
  - [ ] Add MSI-X support (optional)

- [ ] **Add unit tests**
  - [ ] Test queue descriptor chain walking
  - [ ] Test used ring updates
  - [ ] Test feature negotiation

---

## File Reference Quick Links

### OpenVMM Reference Files
| Component | Path |
|-----------|------|
| Chipset Builder | `vmm_core/vmotherboard/src/chipset/builder/mod.rs` |
| Base Chipset | `vmm_core/vmotherboard/src/base_chipset.rs` |
| Interrupt Services | `vmm_core/vmotherboard/src/chipset/backing/arc_mutex/services.rs` |
| Line Interrupt | `vm/chipset_device/src/interrupt.rs` |
| PIC | `vm/devices/chipset/src/pic.rs` |
| IOAPIC | `vm/devices/chipset/src/ioapic.rs` |
| PIT | `vm/devices/chipset/src/pit.rs` |
| i8042 | `vm/devices/chipset/src/i8042/mod.rs` |
| CMOS RTC | `vm/devices/chipset/src/cmos_rtc.rs` |
| Serial 16550 | `vm/devices/serial/serial_16550/src/lib.rs` |
| Virtio Common | `vm/devices/virtio/virtio/src/common.rs` |
| Virtio MMIO | `vm/devices/virtio/virtio/src/transport/mmio.rs` |
| Virtio PCI | `vm/devices/virtio/virtio/src/transport/pci.rs` |

### CC Current Files
| Component | Path |
|-----------|------|
| HV Common | `internal/hv/common.go` |
| KVM AMD64 | `internal/hv/kvm/kvm_amd64.go` |
| KVM IRQ | `internal/hv/kvm/kvm_irq.go` |
| PIC | `internal/devices/amd64/chipset/pic.go` |
| IOAPIC | `internal/devices/amd64/chipset/ioapic.go` |
| PIT | `internal/devices/amd64/chipset/pit.go` |
| CMOS | `internal/devices/amd64/chipset/cmos.go` |
| IRQ Sink | `internal/devices/amd64/chipset/sink.go` |
| Serial | `internal/devices/amd64/serial/serial.go` |
| i8042 | `internal/devices/amd64/input/i8042.go` |
| Virtio MMIO | `internal/devices/virtio/mmio.go` |
| Virtio Console | `internal/devices/virtio/console.go` |

---

## Priority Order

1. **Interrupt Fabric** (Section 2) - Foundation for everything else
2. **IOAPIC Enhancements** (Section 4) - Fix current hang issues
3. **PIT/Timers** (Section 5) - Required for guest timing
4. **Device Framework** (Section 1) - Clean up device registration
5. **CMOS/RTC** (Section 7) - Guest time support
6. **PIC Enhancements** (Section 3) - Legacy support
7. **i8042** (Section 6) - Keyboard input
8. **Serial 16550** (Section 8) - Console improvements
9. **Virtio Core** (Section 9) - Future device support
