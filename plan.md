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

- [ ] **Create `internal/chipset/device.go`** - Define unified device interface
  - [ ] Add `ChipsetDevice` interface combining PIO, MMIO, Poll capabilities
  - [ ] Add `SupportsPortIO() *PortIOIntercept` method
  - [ ] Add `SupportsMmio() *MmioIntercept` method
  - [ ] Add `SupportsPollDevice() *PollDevice` method
  - [ ] Add `ChangeDeviceState` interface (Start, Stop, Reset)

- [ ] **Create `internal/chipset/builder.go`** - Chipset builder
  - [ ] Add `ChipsetBuilder` struct with device registry
  - [ ] Add `RegisterDevice(name string, dev ChipsetDevice)` method
  - [ ] Add `WithPioPort(port uint16, handler PortIOHandler)` method
  - [ ] Add `WithMmioRegion(base, size uint64, handler MmioHandler)` method
  - [ ] Add `WithInterruptLine(line uint8, sink InterruptSink)` method
  - [ ] Add `Build() (*Chipset, error)` to finalize registration
  - [ ] Add port/region conflict detection

- [ ] **Create `internal/chipset/chipset.go`** - Runtime chipset
  - [ ] Add `Chipset` struct holding device map and dispatch tables
  - [ ] Add `HandlePIO(port uint16, data []byte, isWrite bool) error`
  - [ ] Add `HandleMMIO(addr uint64, data []byte, isWrite bool) error`
  - [ ] Add `Poll(ctx context.Context)` for poll-based devices
  - [ ] Add `Start()`, `Stop()`, `Reset()` lifecycle methods

- [ ] **Update `internal/hv/kvm/kvm_amd64.go`**
  - [ ] Replace manual device dispatch with chipset dispatch
  - [ ] Update `addX86Devices()` to use builder pattern

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

- [ ] **Enhance `internal/devices/amd64/chipset/sink.go`**
  - [ ] Add `LineInterrupt` interface:
    ```go
    type LineInterrupt interface {
        SetLevel(high bool)
        PulseInterrupt()  // For edge-triggered
    }
    ```
  - [ ] Add `LineInterruptDetached()` factory for noop line
  - [ ] Add `LineInterruptFromFunc(fn func(bool)) LineInterrupt` adapter

- [ ] **Create `internal/chipset/lineset.go`** - Interrupt line set
  - [ ] Add `LineSet` struct managing multiple interrupt lines
  - [ ] Add `AllocateLine(irq uint8) LineInterrupt` method
  - [ ] Add `RegisterEOICallback(line uint8, fn func())` for EOI notification
  - [ ] Add `BroadcastEOI(vector uint8)` method called from LAPIC
  - [ ] Wire EOI to IOAPIC's `HandleEOI(vector uint32)` method

- [ ] **Update IOAPIC** (`internal/devices/amd64/chipset/ioapic.go`)
  - [ ] Expose `HandleEOI(vector uint32)` publicly (already exists)
  - [ ] Add stats for EOI events

- [ ] **Update PIC** (`internal/devices/amd64/chipset/pic.go`)
  - [ ] Add `LineInterrupt` output for INT pin
  - [ ] Add `AcknowledgeHook` interface for in-kernel PIC integration

- [ ] **Update KVM integration** (`internal/hv/kvm/kvm_irq.go`)
  - [ ] Wire LAPIC EOI to `LineSet.BroadcastEOI()`
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

- [ ] **Update `internal/devices/amd64/chipset/pic.go`**
  - [ ] Add `picStats` struct:
    ```go
    type picStats struct {
        spuriousInterrupts uint64
        acknowledges       uint64
        perIRQ             [8]uint64
    }
    ```
  - [ ] Track spurious IRQ7/IRQ15 delivery
  - [ ] Add per-line interrupt counters
  - [ ] Implement rotate-on-auto-EOI (OCW2 R=1, SL=0, EOI=1)
  - [ ] Implement special mask mode (OCW3 SMM/ESMM bits)
  - [ ] Add poll command implementation (OCW3 P bit) - partially done
  - [ ] Add `Init(vm hv.VirtualMachine) error` with reset logic
  - [ ] Add snapshot/restore support (`DeviceSnapshotter` interface)

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

- [ ] **Update `internal/devices/amd64/chipset/ioapic.go`**
  - [ ] Add delivery mode statistics:
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
  - [ ] Add delivery status bit (bit 12) read support
  - [ ] Implement polarity inversion (bit 13) for active-low
  - [ ] Add destination shorthand handling for broadcast
  - [ ] Fix level-triggered re-assert after EOI when line still high
  - [ ] Add `Debug()` method for inspection
  - [ ] Verify remote-IRR clear on write (should be read-only)

- [ ] **Add unit tests** (`internal/devices/amd64/chipset/ioapic_test.go`)
  - [ ] Test edge-triggered interrupt delivery
  - [ ] Test level-triggered with remote-IRR
  - [ ] Test mask/unmask behavior
  - [ ] Test EOI clearing remote-IRR
  - [ ] Test redirection entry programming

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

- [ ] **Update `internal/devices/amd64/chipset/pit.go`**
  - [ ] Implement `PollDevice` interface for timer callbacks
  - [ ] Add `VmTime` abstraction:
    ```go
    type VmTime interface {
        Now() time.Duration
        SetTimeout(deadline time.Duration)
        CancelTimeout()
    }
    ```
  - [ ] Implement mode 0 (interrupt on terminal count)
  - [ ] Implement mode 2 (rate generator) - currently present
  - [ ] Implement mode 3 (square wave generator)
  - [ ] Implement mode 4 (software triggered strobe)
  - [ ] Add readback command (0xC2 on port 0x43)
  - [ ] Add status latch command
  - [ ] Fix counter latch for 16-bit reads

- [ ] **Implement Port 0x61** (`internal/devices/amd64/chipset/port61.go`)
  - [ ] Create `Port61` device struct
  - [ ] Add timer 2 gate control (bit 0)
  - [ ] Add speaker data control (bit 1)
  - [ ] Add refresh request status (bit 4) - toggle for timing loops
  - [ ] Add timer 2 output status (bit 5)
  - [ ] Wire to PIT channel 2

- [ ] **Add unit tests** (`internal/devices/amd64/chipset/pit_test.go`)
  - [ ] Test mode 2 periodic timer
  - [ ] Test mode 0 one-shot
  - [ ] Test counter latch and readback
  - [ ] Test port 0x61 integration

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

- [ ] **Enhance `internal/devices/amd64/input/i8042.go`**
  - [ ] Add controller state machine:
    ```go
    type i8042State struct {
        commandFlag     CommandFlag
        dataPortTarget  DataPortTarget  // keyboard/mouse/controller
        outputBuffer    byte
        outputBufferState OutputBufferState
        a20Gate         bool
        memory          [32]byte  // internal RAM
    }
    ```
  - [ ] Implement controller commands (0x20-0xFF):
    - [ ] 0x20: Read command byte
    - [ ] 0x60: Write command byte
    - [ ] 0xA1: Unknown (return 0)
    - [ ] 0xA7/0xA8: Disable/enable aux interface
    - [ ] 0xA9: Test aux interface
    - [ ] 0xAA: Self-test (return 0x55)
    - [ ] 0xAB: Test keyboard interface
    - [ ] 0xAD/0xAE: Disable/enable keyboard
    - [ ] 0xD0: Read output port
    - [ ] 0xD1: Write output port (A20/reset)
    - [ ] 0xD4: Write to aux device
    - [ ] 0xFE: Pulse reset (CPU reset)
  - [ ] Add output buffer state management (empty/controller/keyboard/mouse)
  - [ ] Add command byte (interrupts enable, translate, disable flags)

- [ ] **Create `internal/devices/amd64/input/ps2keyboard.go`**
  - [ ] Add PS/2 keyboard state machine
  - [ ] Implement keyboard commands (0xFF reset, 0xED LEDs, 0xF0 scancode set)
  - [ ] Add scancode set 2 translation
  - [ ] Add input queue for key events
  - [ ] Wire to `LineInterrupt` for IRQ1

- [ ] **Create `internal/devices/amd64/input/ps2mouse.go`** (optional)
  - [ ] Add PS/2 mouse protocol
  - [ ] Wire to `LineInterrupt` for IRQ12

- [ ] **Add unit tests**
  - [ ] Test self-test command
  - [ ] Test A20 gate control
  - [ ] Test keyboard enable/disable

---

## 7. CMOS/RTC/PM

**Goal**: Enhance CMOS RTC with proper timer support and add PM/battery/watchdog stubs.

**Reference Files**:
- `vm/devices/chipset/src/cmos_rtc.rs` - Rtc implementation

**Current State** (cc):
- `internal/devices/amd64/chipset/cmos.go` has basic RTC
- Missing: periodic/alarm interrupts, proper BCD, timer polling

### Implementation Checklist

- [ ] **Update `internal/devices/amd64/chipset/cmos.go`**
  - [ ] Add Status Register A:
    - [ ] Periodic timer rate (bits 0-3)
    - [ ] Oscillator control (bits 4-6)
    - [ ] Update in progress (bit 7) - pulse every second
  - [ ] Add Status Register B:
    - [ ] DST enable (bit 0)
    - [ ] 24-hour mode (bit 1)
    - [ ] BCD disable (bit 2)
    - [ ] Square wave enable (bit 3)
    - [ ] Update IRQ enable (bit 4)
    - [ ] Alarm IRQ enable (bit 5)
    - [ ] Periodic IRQ enable (bit 6)
    - [ ] Set mode (bit 7)
  - [ ] Add Status Register C (interrupt flags, read clears)
  - [ ] Add Status Register D (VRT bit = 1 always)
  - [ ] Implement periodic timer using `PollDevice`:
    ```go
    func (r *Rtc) PollDevice(ctx context.Context) {
        // Check periodic timer
        // Check alarm timer
        // Check update timer (1 second)
    }
    ```
  - [ ] Implement alarm with wildcard support (0xFF = don't care)
  - [ ] Add BCD encode/decode functions
  - [ ] Add 12/24 hour conversion
  - [ ] Wire IRQ8 to `LineInterrupt`

- [ ] **Create `internal/devices/amd64/chipset/pm.go`** (stub)
  - [ ] Add ACPI PM1a control/status registers
  - [ ] Add PM timer (port 0x408)
  - [ ] Add sleep state handling (stub)

- [ ] **Add unit tests** (`internal/devices/amd64/chipset/cmos_test.go`)
  - [ ] Test time register read/write
  - [ ] Test BCD encoding
  - [ ] Test alarm matching
  - [ ] Test status register C clear-on-read

---

## 8. Serial 16550

**Goal**: Add MMIO variant and async buffered I/O for Serial16550.

**Reference Files**:
- `vm/devices/serial/serial_16550/src/lib.rs` - Serial16550

**Current State** (cc):
- `internal/devices/amd64/serial/serial.go` has PIO 16550
- Missing: MMIO transport, proper FIFO trigger levels, modem status

### Implementation Checklist

- [ ] **Update `internal/devices/amd64/serial/serial.go`**
  - [ ] Add FIFO trigger level support (1/4/8/14 bytes)
  - [ ] Add modem control register (MCR):
    - [ ] DTR (bit 0)
    - [ ] RTS (bit 1)
    - [ ] OUT1 (bit 2)
    - [ ] OUT2 (bit 3) - interrupt gate
    - [ ] Loopback (bit 4)
  - [ ] Add modem status register (MSR):
    - [ ] CTS change (bit 0)
    - [ ] DSR change (bit 1)
    - [ ] RI trailing edge (bit 2)
    - [ ] DCD change (bit 3)
    - [ ] CTS (bit 4)
    - [ ] DSR (bit 5)
    - [ ] RI (bit 6)
    - [ ] DCD (bit 7)
  - [ ] Implement OUT2 interrupt gating (interrupt = pending && OUT2)
  - [ ] Add `PollDevice` interface for async TX/RX
  - [ ] Add RX/TX statistics

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
