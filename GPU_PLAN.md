# **Designing High-Fidelity Paravirtualized Interfaces: A Comprehensive Architecture for Custom Virtual Machine Monitors**

## **1\. Introduction: The Architecture of Guest Integration**

The development of a custom Virtual Machine Monitor (VMM) presents a unique set of challenges that diverge significantly from the configuration of established hypervisors like QEMU or VMware. When the objective is to host a Linux guest operating system with a high degree of integration—specifically targeting fluid display performance, precise input response, and seamless data interchange—without the crutch of hardware-accelerated GPU passthrough, the architectural burden shifts entirely to the efficiency of the paravirtualized interfaces.

The constraints defined for this analysis—a Linux compatible frame buffer without GPU acceleration, absolute mouse movement, and clipboard sharing—point toward a specific class of virtualization problems: the optimization of software-defined peripherals. In this context, "without GPU acceleration" does not imply a disregard for performance; rather, it necessitates a rigorous implementation of zero-copy mechanisms and interrupt-driven I/O to ensure that the guest’s software rendering pipeline is not bottlenecked by the transport layer to the host.

This report conducts an exhaustive technical evaluation of the available standards and implementation paths for these three subsystems. It posits that the **Virtio** family of devices—specifically **Virtio-GPU (2D)**, **Virtio-Input**, and **Virtio-Serial**—constitutes the only viable, forward-looking architecture for a modern custom VMM. While legacy interfaces such as VGA, PS/2, and USB emulation offer historical compatibility, they impose unacceptable latency penalties and complexity costs that undermine the goal of a seamless guest experience. By leveraging the native support for Virtio within the Linux kernel, a custom VMM can achieve "native-like" responsiveness through standardized, maintainable, and high-performance protocols.

### **1.1 The Paravirtualization Advantage**

In a purely emulated environment, the guest operating system is unaware it is running inside a virtual machine. It drives "hardware" by writing to specific memory addresses that trigger traps in the VMM. This approach, exemplified by legacy VGA adapters or PS/2 keyboard controllers, is computationally expensive due to the high frequency of VM exits.

Paravirtualization alters this contract. The guest OS is aware of the virtualization layer and cooperates with the VMM. The **Virtio** standard (Virtual I/O) provides a common ABI (Application Binary Interface) for this cooperation. It abstracts devices into **Virtqueues**—ring buffers in shared memory that allow the guest to batch requests (such as "display this buffer" or "send this keystroke") and notify the host via a single lightweight "kick" (IOEVENTFD).

For the subsystems in question:

* **Display:** Instead of emulating the complex register state of a Cirrus Logic or Bochs graphics card, Virtio-GPU allows the guest to simply allocate pages of RAM and tell the host, "Here is my framebuffer."  
* **Input:** Instead of simulating the electrical signalling of a USB bus (usb-tablet) or the interrupt storms of a PS/2 mouse, Virtio-Input allows the host to inject Linux native input\_event structures directly into the guest kernel.  
* **Clipboard:** Instead of complex network overlays, Virtio-Serial provides a dedicated pipe for userspace agents to negotiate data formats.

This report will dissect the implementation details of these three components, referencing the Linux kernel source and Virtio specifications to provide a blueprint for a custom VMM implementation.

## ---

**2\. The Graphics Subsystem: Virtio-GPU 2D Implementation**

The primary challenge in virtualizing a display without hardware acceleration is the efficient management of the framebuffer memory. The guest operating system must render its desktop environment (X11 or Wayland compositor) into system RAM using a software rasterizer (like LLVMpipe), and the VMM must present this memory to the user on the host.

While alternatives exist, the **Virtio-GPU** device (Device ID 16\) in its 2D configuration is the industry-standard solution for this use case. It is superior to legacy VGA (which is resolution-limited and slow) and simple framebuffers (simpledrm), offering dynamic resizing and multi-monitor support.

### **2.1 Architectural Comparison: Why Virtio-GPU 2D?**

To understand why Virtio-GPU 2D is the optimal choice, one must compare it against the alternatives available to a VMM developer.

#### **2.1.1 The Limitations of simpledrm**

The simpledrm driver in Linux creates a framebuffer based on a pre-allocated memory region defined by the system firmware (UEFI GOP or Device Tree).1

* **Mechanism:** The VMM allocates a chunk of RAM, tells the guest kernel its address and dimensions (e.g., 1920x1080), and the guest writes pixels to it.  
* **The Flaw:** This framebuffer is **static**. The resolution is fixed at boot time. If the user resizes the VMM window on the host, the guest has no mechanism to learn about the new size or re-negotiate the memory layout without a reboot.3  
* **Use Case:** This is adequate for embedded systems or servers, but unacceptable for a desktop VMM where window resizing is a basic user expectation.

#### **2.1.2 The Complexity of QXL**

QXL was the previous standard for SPICE-based virtualization.4

* **Mechanism:** It presents a paravirtualized GPU with 2D acceleration commands (fill, copy, composite).  
* **The Flaw:** Modern desktop environments (GNOME, KDE) rely on the GPU for compositing. They do not use the specific 2D acceleration primitives QXL provides. Consequently, QXL often falls back to acting as a dumb framebuffer, but with a much more complex driver stack than Virtio-GPU. It is widely considered deprecated for new VMM designs.

#### **2.1.3 The Virtio-GPU 2D Solution**

Virtio-GPU allows the guest to perform software rendering but provides a **control channel** to manage the display lifecycle.6

* **Dynamic Resizing:** The host can trigger an interrupt to notify the guest of a configuration change (new window size).  
* **Standard Driver:** The virtio-gpu.ko driver is mainline in Linux (since kernel 4.4).5 No guest tools installation is required for basic display output.  
* **Zero-Copy Potential:** The protocol supports passing scatter-gather lists of guest memory, allowing the VMM to read directly from the guest's rendering surface without forcing the guest to copy data to a specific aperture.

### **2.2 Virtio-GPU 2D Protocol Specification**

Implementing Virtio-GPU 2D requires the VMM to handle specific commands on the **Control Virtqueue**. The interaction model is request-response: the guest places a command in the queue, notifies the host, and the host processes it and writes a response.

#### **2.2.1 Device Initialization and Feature Negotiation**

The Virtio-GPU device ID is 16\. Upon startup, the guest driver reads the configuration space to determine the number of supported scanouts (virtual monitors).

* **Struct virtio\_gpu\_config:**  
  * num\_scanouts: The number of supported displays (typically 1 for a basic VMM).  
  * events\_read / events\_clear: Used for hot-plug events.  
* **Feature Bits:** The VMM should negotiate VIRTIO\_GPU\_F\_EDID if it intends to support extended display identification data, though strictly 2D mode functions without it.7

#### **2.2.2 The Command Lifecycle**

The 2D protocol revolves around managing **Resources** (surfaces) and **Scanouts** (displays).

Step 1: Resource Creation (VIRTIO\_GPU\_CMD\_RESOURCE\_CREATE\_2D)  
The guest requests the creation of a 2D surface.

* **Request Data:** resource\_id, format (e.g., VIRTIO\_GPU\_FORMAT\_B8G8R8A8\_UNORM), width, height.  
* **VMM Responsibility:** The VMM must allocate a tracking structure for this resource\_id. Crucially, **no pixel memory is transferred yet**. The VMM merely acknowledges that ID 1 now refers to a 1920x1080 surface of a specific format.8

Step 2: Memory Attachment (VIRTIO\_GPU\_CMD\_RESOURCE\_ATTACH\_BACKING)  
The guest allocates physical RAM pages to store the pixel data. Since these pages are allocated by the OS kernel, they are likely not contiguous in Guest Physical Address (GPA) space.

* **Request Data:** resource\_id, nr\_entries, and an array of virtio\_gpu\_mem\_entry structs.  
* **Struct virtio\_gpu\_mem\_entry:** Contains addr (GPA) and length.  
* **VMM Responsibility:** The VMM must parse this scatter-gather list and store the mapping. It does not read the data yet. It essentially builds a map: "Lines 0-10 of the image are at GPA 0x1000, Lines 11-20 are at GPA 0x5000", etc..8 This "Backing Store" concept is central to Virtio-GPU—the VMM never owns the memory; it only reads from the guest's pages.

Step 3: Setting the Scanout (VIRTIO\_GPU\_CMD\_SET\_SCANOUT)  
The guest links a resource to a display output.

* **Request Data:** scanout\_id (e.g., 0 for the first monitor), resource\_id, r (rectangle geometry).  
* **VMM Responsibility:** The VMM binds the resource to the window. If the resource size matches the host window size, this is a 1:1 mapping. If they differ, the VMM may need to scale the output or center it.

Step 4: The Transfer (VIRTIO\_GPU\_CMD\_TRANSFER\_TO\_HOST\_2D)  
This is the "Draw" command. The guest software rasterizer has finished drawing a frame (or a portion of it) into the backing pages.

* **Request Data:** resource\_id, rect (x, y, w, h), offset.  
* **VMM Responsibility:** This is the performance-critical step. The VMM must:  
  1. Identify the region of the resource being updated.  
  2. Walk the scatter-gather list stored in Step 2 to find the GPAs corresponding to this rectangle.  
  3. Perform a memcpy from Guest RAM to the Host's internal surface/texture.  
     Note: This transfer is a "pull" model. The host pulls data from guest RAM into its own domain (e.g., into an OpenGL texture or a shared memory buffer for the window manager).9

**Step 5: The Flush (VIRTIO\_GPU\_CMD\_RESOURCE\_FLUSH)**

* **Request Data:** resource\_id, rect.  
* **VMM Responsibility:** The data transfer is complete. The VMM should now trigger a repaint of the host window to present the new frame to the user.

### **2.3 Handling Dynamic Resizing**

One of the distinct advantages of Virtio-GPU is the ability to handle host-initiated resizing.

1. **Event:** User resizes VMM window to 1280x720.  
2. **Notification:** The VMM raises a configuration change interrupt (usually VIRTIO\_PCI\_ISR\_CONFIG).  
3. **Guest Query:** The Linux guest receives the interrupt and issues VIRTIO\_GPU\_CMD\_GET\_DISPLAY\_INFO.9  
4. **Response:** The VMM responds with the new geometry (pmodes.r.width \= 1280, etc.).  
5. **Re-allocation:** The guest DRM driver destroys the old resource, allocates a new 1280x720 resource, attaches new backing, and sets the new scanout. The desktop environment (GNOME/KDE) automatically adjusts the layout.

This sequence allows the guest desktop to "snap" to the host window size, a critical feature for usability that static framebuffers lack.

## ---

**3\. The Input Subsystem: Precision with Virtio-Input**

For a custom VMM, the choice of input device emulation determines whether the user feels like they are "fighting" the mouse or interacting naturally. The key requirement is **Absolute Mouse Movement** (often called Tablet Mode).

### **3.1 The Problem with Relative Mice (PS/2)**

Standard mice report movement as deltas (+5 x, \-2 y). The guest OS applies an acceleration curve to these deltas to move its cursor. The host OS also applies an acceleration curve to the physical mouse. Because these two curves never match perfectly, the guest cursor drifts away from the host cursor. To fix this, VMMs must "capture" the mouse, hiding the host cursor and locking input to the window. This is a poor user experience.

### **3.2 The Virtio-Input Solution**

**Virtio-Input** acts as a bridge for the Linux Input Subsystem (evdev). Instead of emulating hardware electrical signals (like usb-tablet, which requires simulating USB polling frequencies and consumes significant CPU), Virtio-Input allows the VMM to inject input\_event structures directly into the guest kernel.11

#### **3.2.1 Configuration Space: Defining a Tablet**

To make the Linux guest recognize the device as a tablet, the VMM must populate the virtio\_input\_config space correctly during the device probe. The guest driver queries this space to build the device capabilities bitmap.

The VMM must respond to virtinput\_cfg\_select queries with specific data 13:

1\. Event Types (VIRTIO\_INPUT\_CFG\_EV\_BITS)  
The device must report support for:

* EV\_KEY (0x01): To support buttons.  
* EV\_ABS (0x03): To support absolute coordinates.  
* EV\_SYN (0x00): For synchronization packets.

2\. Key Capabilities (EV\_KEY)  
The bitmap must include:

* BTN\_LEFT (0x110), BTN\_RIGHT (0x111), BTN\_MIDDLE (0x112).  
* **Critical:** BTN\_TOUCH (0x14a). Many Linux input drivers (like libinput) use the presence of BTN\_TOUCH to classify a device as a touchscreen or tablet rather than a joystick or generic absolute device.15 Without this, the cursor may not move or click correctly.

3\. Absolute Axis Configuration (VIRTIO\_INPUT\_CFG\_ABS\_INFO)  
This is the most critical configuration. The VMM must define the logical coordinate space of the tablet.

* **Axes:** ABS\_X (0x00) and ABS\_Y (0x01).  
* **Struct virtio\_input\_absinfo:**  
  * min: 0  
  * max: 32767 (0x7FFF). This is a standard high-resolution range used by devices like vjoy and generic HID tablets.17  
  * fuzz: 0\. Virtual devices are precise; there is no signal noise to filter.  
  * flat: 0\.  
  * res: 0 (or a calculated value if physical dimensions are simulated, but 0 usually suffices for virtual pointers).

**Implementation Insight:** The max value of 32767 is arbitrary but standard. It allows the VMM to normalize coordinates. The VMM does *not* need to change this value when the window resizes. It simply scales the host coordinates to this fixed range.

### **3.3 The Injection Protocol**

When the VMM receives a mouse move event from the host OS, it must translate and inject it.

Step 1: Coordinate Normalization  
Given Host Window Width ($W$), Height ($H$), and Mouse Position ($x, y$):

$$GuestX \= \\frac{x}{W} \\times 32767$$

$$GuestY \= \\frac{y}{H} \\times 32767$$

Note: The VMM must clamp these values between 0 and 32767 to prevent out-of-bounds events if the mouse strays slightly outside the window before the VMM catches it.  
Step 2: Event Batching  
The VMM constructs a sequence of virtio\_input\_event structs and places them in the Event Virtqueue.14

1. **X-Axis Event:** type=EV\_ABS, code=ABS\_X, value=GuestX  
2. **Y-Axis Event:** type=EV\_ABS, code=ABS\_Y, value=GuestY  
3. **Sync Event:** type=EV\_SYN, code=SYN\_REPORT, value=0

The SYN\_REPORT is mandatory. The Linux kernel buffers the axis updates and only applies them (moving the cursor) when it sees the SYN event. This ensures atomic updates so the cursor doesn't "stutter" by moving X then Y separately.

### **3.4 Keyboard Integration**

For the keyboard, **Virtio-Input** is also the preferred method over PS/2.

* **Config:** Advertise EV\_KEY and the full bitmap of standard PC keys (KEY\_Q, KEY\_W, etc.).  
* **Events:** Host key press \-\> type=EV\_KEY, code=LINUX\_KEYCODE, value=1 (press) or 0 (release).  
* **Advantage:** This bypasses the legacy i8042 controller emulation, which is slow and interrupt-heavy. It also allows injecting keys that might be difficult to map via PS/2 scancodes.

## ---

**4\. Clipboard Sharing: The SPICE Agent Ecosystem**

While display and input are kernel-level device interactions, clipboard sharing is a user-space problem. The kernel knows nothing about "text" or "images"; only the display server (X11 or Wayland) manages the clipboard selections. Therefore, an **Agent** running inside the guest is required to bridge the gap.

The industry standard for this is the **SPICE Agent** (spice-vdagent), communicating over a **Virtio-Serial** transport.

### **4.1 Transport Layer: Virtio-Serial**

Clipboard data is essentially a stream of bytes. The **Virtio-Serial** device (Device ID 3, virtio-console) provides a simple, high-throughput pipe between host and guest.19

**Configuration:**

* The VMM must expose a Virtio-Serial device with the VIRTIO\_CONSOLE\_F\_MULTIPORT feature.  
* It must define a specific named port: com.redhat.spice.0.  
* **Guest Recognition:** The Linux kernel detects this name and creates a character device at /dev/virtio-ports/com.redhat.spice.0. The spice-vdagentd daemon (standard in almost all Linux distributions) automatically looks for this path to establish communication.20

### **4.2 The SPICE Agent Protocol**

The communication over this serial port follows the SPICE Agent Protocol. It is a binary protocol that packets data into messages.

#### **4.2.1 Message Structure**

Every packet sent over the serial port must be encapsulated in a VDIChunkHeader followed by the message data.20

C

struct VDIChunkHeader {  
    uint32\_t port; // VDP\_CLIENT\_PORT (1) or VDP\_SERVER\_PORT (2)  
    uint32\_t size; // Size of the following message  
};

struct VDAgentMessage {  
    uint32\_t protocol; // Always 1  
    uint32\_t type;     // e.g., VD\_AGENT\_CLIPBOARD  
    uint64\_t opaque;   // Request ID  
    uint32\_t size;     // Data size  
    uint8\_t data;   // Payload  
};

**Implementation Criticality:** The "Chunk" header allows large messages (like an image paste) to be split across multiple Virtio buffers. The VMM must implement reassembly logic to handle fragmented messages.

#### **4.2.2 The Handshake**

Before any clipboard data flows, capabilities must be negotiated.

1. **Connect:** Guest agent opens the port.  
2. **Announce:** Both Host and Guest send VD\_AGENT\_ANNOUNCE\_CAPABILITIES.  
3. **Caps:** The critical capability is VD\_AGENT\_CAP\_CLIPBOARD\_SELECTION. This informs the VMM that the guest supports the X11 distinction between CLIPBOARD (Ctrl+C) and PRIMARY (Selection).20

#### **4.2.3 Clipboard State Machine**

The clipboard synchronization is event-driven and ownership-based.

**Scenario A: Guest Copy \-\> Host Paste**

1. **Guest Event:** User selects text in a guest app. The spice-vdagent (running in X11/Wayland) detects the ownership change.  
2. **Grab:** Guest sends VD\_AGENT\_CLIPBOARD\_GRAB to the VMM.  
   * *Payload:* A list of supported MIME types (e.g., UTF8\_STRING, text/plain, image/png).  
   * *VMM Action:* The VMM essentially "claims" the host OS clipboard. It does *not* have the data yet. It advertises the available formats to the host OS.  
3. **Host Event:** User presses Ctrl+V in a host app.  
4. **Request:** The Host OS asks the VMM for data. The VMM sends VD\_AGENT\_CLIPBOARD\_REQUEST to the guest via the serial port, requesting a specific type (e.g., UTF8\_STRING).  
5. **Data:** The guest agent reads the selection from the X server and sends VD\_AGENT\_CLIPBOARD containing the bytes.  
6. **Delivery:** The VMM receives the packet and hands the data to the host OS to complete the paste.

Scenario B: Host Copy \-\> Guest Paste  
The process is symmetrical. When the host clipboard changes, the VMM sends VD\_AGENT\_CLIPBOARD\_GRAB to the guest. When the guest user pastes, the agent sends a REQUEST, and the VMM replies with the DATA.

### **4.3 The Wayland Complexity**

In 2025, modern Linux guests predominantly use Wayland. The traditional X11 method of "snooping" the clipboard is blocked by Wayland's security model.

* **GNOME/Mutter:** The GNOME project has integrated a private protocol for spice-vdagent, allowing it to function correctly on Wayland sessions.  
* **Other Compositors:** On bare-bones Wayland compositors (like Sway), spice-vdagent may fail or require specific plugins (like wl-clipboard wrappers).  
* **Recommendation:** For a custom VMM, implementing the server-side (Host) of the SPICE protocol is the safest bet. It works perfectly with X11 guests and works with Wayland guests that run standard desktop environments (Ubuntu, Fedora, RHEL). While virtio-wl is a newer, Wayland-specific alternative (proxying the Wayland protocol directly), it requires a specialized guest kernel and proxy binary (sommelier), making it less compatible with generic Linux distributions than the established SPICE agent.22

## ---

**5\. Architectural Synthesis and Implementation Roadmap**

To build this system effectively, the development should follow a layered approach, validating each paravirtualized subsystem sequentially.

### **5.1 Layer 1: Transport Foundation**

The VMM must implement the **Virtio-PCI** transport layer. This is the bedrock.

* **Configuration Space:** Maps BARs (Base Address Registers) for device config.  
* **Virtqueues:** Implements the ring buffer logic (vring) for data exchange.  
* **Interrupts:** Implements MSI-X injection to notify the guest of events.  
* *Verification:* A guest kernel should boot and identify the Virtio devices via lspci.

### **5.2 Layer 2: Display (Virtio-GPU)**

1. **Device:** ID 16\.  
2. **Queue:** Implement the Control Queue handler.  
3. **Logic:**  
   * Handle RESOURCE\_CREATE\_2D (track ID).  
   * Handle ATTACH\_BACKING (store scatter-gather map).  
   * Handle TRANSFER\_TO\_HOST\_2D (execute memcpy from Guest RAM to Host Buffer).  
   * Handle FLUSH (trigger Host UI redraw).  
4. *Verification:* You should see the guest login screen. Resizing the window should update the guest resolution (requires handling GET\_DISPLAY\_INFO).

### **5.3 Layer 3: Input (Virtio-Input)**

1. **Device:** ID 18\.  
2. **Config Space:** Implement the virtinput\_cfg\_select logic to advertise ABS\_X/ABS\_Y (0-32767) and BTN\_TOUCH.  
3. **Event Loop:** Hook the host window's "Mouse Move" event.  
4. **Injection:** Transform host coordinates to 0-32767 space and push input\_event packets to the queue.  
5. *Verification:* Run evtest in the guest. Moving the mouse should show absolute coordinate updates. The cursor should track perfectly without capture.

### **5.4 Layer 4: Integration (Clipboard)**

1. **Device:** ID 3 (Virtio-Serial).  
2. **Port:** Name it com.redhat.spice.0.  
3. **Protocol:** Implement the SPICE state machine.  
   * Parse VDIChunkHeader.  
   * Handle GRAB (update host clipboard state).  
   * Handle REQUEST (fetch/send data).  
4. *Verification:* Copy text in Guest gedit, paste in Host Notepad.

### **5.5 Summary of Recommendations**

The following table summarizes the recommended architectural choices for a custom VMM targeting Linux guests in 2025\.

| Feature | Recommended Standard | Host Implementation | Guest Requirement | Why? |
| :---- | :---- | :---- | :---- | :---- |
| **Framebuffer** | **Virtio-GPU (2D)** | Control Queue processor, Software Blitter | Kernel virtio-gpu (Standard) | Dynamic resizing, standard support, efficient memory mapping. |
| **Keyboard** | **Virtio-Input** | Event Queue Injector | Kernel virtio-input (Standard) | Low latency, no legacy I/O port overhead. |
| **Mouse/Tablet** | **Virtio-Input** | Absolute Tablet Config (0-32767) | Kernel virtio-input (Standard) | Precise tracking, no cursor capture needed, lower CPU than USB. |
| **Clipboard** | **SPICE Agent** | Virtio-Serial \+ Protocol State Machine | spice-vdagent package | Robust negotiation, binary safe, supports X11 & Wayland (GNOME). |

## **6\. Conclusion**

For a developer crafting a custom VMM, the path to a high-quality Linux guest experience lies in adopting the **Virtio** standard comprehensively. By implementing **Virtio-GPU 2D** for display, **Virtio-Input** for tablet-based pointer control, and **Virtio-Serial** carrying the **SPICE protocol** for clipboard integration, one creates a system that is performant, compliant with upstream Linux kernels, and capable of delivering the seamless interoperability users expect from modern virtualization platforms. This architecture avoids the pitfalls of legacy emulation and provides a solid foundation for future enhancements.

#### **Works cited**

1. (FINAL POST) Virtio-GPU: Venus running Resident Evil 7 Village : r/linux\_gaming \- Reddit, accessed December 24, 2025, [https://www.reddit.com/r/linux\_gaming/comments/1c4x6oh/final\_post\_virtiogpu\_venus\_running\_resident\_evil/](https://www.reddit.com/r/linux_gaming/comments/1c4x6oh/final_post_virtiogpu_venus_running_resident_evil/)  
2. Linux Framebuffer set resolution correctly \- Stack Overflow, accessed December 24, 2025, [https://stackoverflow.com/questions/34904763/linux-framebuffer-set-resolution-correctly](https://stackoverflow.com/questions/34904763/linux-framebuffer-set-resolution-correctly)  
3. Can the framebuffer be made resizable? \- virtualbox.org, accessed December 24, 2025, [https://forums.virtualbox.org/viewtopic.php?t=33272](https://forums.virtualbox.org/viewtopic.php?t=33272)  
4. QXL vs VirtIO GPU vs VirGL GPU \- trivial benchmark on my setup : r/Proxmox \- Reddit, accessed December 24, 2025, [https://www.reddit.com/r/Proxmox/comments/1auvdlg/qxl\_vs\_virtio\_gpu\_vs\_virgl\_gpu\_trivial\_benchmark/](https://www.reddit.com/r/Proxmox/comments/1auvdlg/qxl_vs_virtio_gpu_vs_virgl_gpu_trivial_benchmark/)  
5. QEMU/Guest graphics acceleration \- ArchWiki, accessed December 24, 2025, [https://wiki.archlinux.org/title/QEMU/Guest\_graphics\_acceleration](https://wiki.archlinux.org/title/QEMU/Guest_graphics_acceleration)  
6. virtio-gpu — QEMU 8.2.10 documentation, accessed December 24, 2025, [https://qemu.readthedocs.io/en/v8.2.10/system/devices/virtio-gpu.html](https://qemu.readthedocs.io/en/v8.2.10/system/devices/virtio-gpu.html)  
7. devices::virtio::gpu::protocol \- Rust, accessed December 24, 2025, [https://crosvm.dev/doc/devices/virtio/gpu/protocol/index.html](https://crosvm.dev/doc/devices/virtio/gpu/protocol/index.html)  
8. VIRTIO GPU Operation Highlights \- Confluence Mobile \- COVESA, accessed December 24, 2025, [https://wiki.covesa.global/x/UAG5](https://wiki.covesa.global/x/UAG5)  
9. \[PATCH v2 4/6\] dm: virtio-gpu: 2D mode support, accessed December 24, 2025, [https://lists.projectacrn.org/g/acrn-dev/message/35208](https://lists.projectacrn.org/g/acrn-dev/message/35208)  
10. Modernizing Virtio GPU | Kernel Recipes, accessed December 24, 2025, [https://kernel-recipes.org/en/2025/schedule/modernizing-virtio-gpu/](https://kernel-recipes.org/en/2025/schedule/modernizing-virtio-gpu/)  
11. Virtio-Input — Project ACRN™ 3.4-unstable documentation, accessed December 24, 2025, [https://projectacrn.github.io/latest/developer-guides/hld/virtio-input.html](https://projectacrn.github.io/latest/developer-guides/hld/virtio-input.html)  
12. Virtio-input — Project ACRN™ v 1.6 documentation, accessed December 24, 2025, [https://projectacrn.github.io/1.6/developer-guides/hld/virtio-input.html](https://projectacrn.github.io/1.6/developer-guides/hld/virtio-input.html)  
13. virtio-input.tex, accessed December 24, 2025, [https://docs.oasis-open.org/virtio/virtio/v1.2/cs01/tex/virtio-input.tex](https://docs.oasis-open.org/virtio/virtio/v1.2/cs01/tex/virtio-input.tex)  
14. input.h source code \[include/linux/input.h\] \- Codebrowser, accessed December 24, 2025, [https://codebrowser.dev/gtk/include/linux/input.h.html](https://codebrowser.dev/gtk/include/linux/input.h.html)  
15. Touch devices | Android Open Source Project, accessed December 24, 2025, [https://source.android.com/docs/core/interaction/input/touch-devices](https://source.android.com/docs/core/interaction/input/touch-devices)  
16. 2\. Input event codes \- The Linux Kernel documentation, accessed December 24, 2025, [https://docs.kernel.org/input/event-codes.html](https://docs.kernel.org/input/event-codes.html)  
17. Axis value range | vJoy \- ProBoards, accessed December 24, 2025, [https://vjoy.freeforums.net/thread/15/axis-value-range](https://vjoy.freeforums.net/thread/15/axis-value-range)  
18. virtio\_input.c source code \[linux/drivers/virtio/virtio\_input.c ..., accessed December 24, 2025, [https://codebrowser.dev/linux/linux/drivers/virtio/virtio\_input.c.html](https://codebrowser.dev/linux/linux/drivers/virtio/virtio_input.c.html)  
19. Features/VirtioSerial \- Fedora Project Wiki, accessed December 24, 2025, [https://fedoraproject.org/wiki/Features/VirtioSerial](https://fedoraproject.org/wiki/Features/VirtioSerial)  
20. Agent Protocol \- Spice, accessed December 24, 2025, [https://www.spice-space.org/agent-protocol.html](https://www.spice-space.org/agent-protocol.html)  
21. spice-protocol/spice/vd\_agent.h at master · flexVDI/spice-protocol ..., accessed December 24, 2025, [https://github.com/flexVDI/spice-protocol/blob/master/spice/vd\_agent.h](https://github.com/flexVDI/spice-protocol/blob/master/spice/vd_agent.h)  
22. talex5/wayland-proxy-virtwl: Allow guest VMs to open windows on the host \- GitHub, accessed December 24, 2025, [https://github.com/talex5/wayland-proxy-virtwl](https://github.com/talex5/wayland-proxy-virtwl)  
23. Clipboard sharing works when using qemu & SPICE: