# Virtiofs Performance Plan

## Goal

Make the virtiofs path fast enough that it can stay the default host/guest filesystem boundary for vsh, with virtual block/rootfs work reserved for cases where native guest filesystem semantics are the right tradeoff.

The target is not just "less slow". We want enough measurement discipline to keep pushing until our virtiofs implementation can beat comparable QEMU setups on the workloads that matter for vsh, while defining Firecracker comparisons carefully because upstream Firecracker is not a direct virtiofs baseline.

## Model

- The command location is shell context, like the current working directory.
- `/host` is an explicit view of the host filesystem from the guest, not a magic unified namespace.
- `/host` should expose host truth. Cache policy must preserve ordinary user expectations unless a mode is clearly scoped to developer benchmarking.
- Guest rootfs state can later move to a virtual ext4 block device. Host interaction should remain virtiofs as long as virtiofs can provide good enough compatibility and speed.
- Everything should be zero config for normal users. Developer-only tuning knobs are acceptable for benchmarking and investigation.

## Workloads

Microbenchmarks should cover the vsh cases that will make or break the shell:

- prompt-time metadata: `stat`, `readdir`, path lookup, git status, autocomplete
- source tree scans: `rg`, `find`, Python/Go/Rust/Node dependency walks
- compiler reads from `/host`
- build output writes to guest-local overlay and explicit `/host` paths
- package manager metadata churn on guest rootfs
- large sequential reads and writes
- small random reads and writes
- concurrent metadata and I/O from multiple guest threads

## Measurement

The benchmark loop should capture:

- wall clock per workload
- host baseline for the same workload
- virtiofs request counts and per-op latency deltas from `/debug/virtiofs`
- request queue notify distribution, so we know whether multiqueue is actually being used
- queue harvest, dispatch, and completion timings
- optional Go CPU, mutex, and block profiles
- later: VM exit counters and guest-side tracing around FUSE waits

Results should be emitted as a compact table for humans and JSON for analysis tools. Do not dump raw JSON in normal user-facing shell output.

## VM Exit Findings

The vim install benchmark showed that individual FUSE operations are not the source of the worst latency spikes. With `CCX3_EXIT_TIMING=1`, the backend command timings were tiny compared with the raw virtiofs MMIO exit buckets:

- `data_abort.virtiofs.write`: about 1.48 million exits, 34.7s total, ~23.5us average, ~50ms max
- `data_abort.virtiofs.read`: about 669k exits, 15.3s total, ~22.8us average, ~133ms max
- FUSE `LOOKUP`: about 286k calls, 0.81s total, ~2.8us average
- FUSE `GETATTR`: about 320k calls, 0.47s total, ~1.5us average
- FUSE `RENAME`: about 4k calls, 0.42s total, ~102us average

That means the main problem is not pathological FUSE command execution. It is the combination of a very high exit count plus occasional host-side stalls around exit handling.

Online research lines up with this:

- Intel's VM workload guidance describes VM exits as expensive transitions and frames the two core levers as reducing transition cost and reducing exit frequency: https://www.intel.com/content/www/us/en/developer/articles/technical/improving-performance-vm-workloads-opt-poll-time.html
- The KVM virtio block latency notes measured `VIRTIO_PCI_QUEUE_NOTIFY` at over 22us, which is strikingly close to our ~23us average virtiofs MMIO exit cost: https://linux-kvm.org/page/Virtio/Block/Latency
- The virtio specification's `VIRTIO_F_EVENT_IDX` and packed virtqueues are designed to reduce notification traffic and ring overhead: https://docs.oasis-open.org/virtio/virtio/v1.2/virtio-v1.2.html
- Virtiofs DAX can avoid many FUSE data read/write round trips by mapping file contents into the guest, but it has host mutation/truncation semantics that need careful fit with `/host`: https://virtio-fs.gitlab.io/howto-qemu.html
- Apple HVF exposes `hv_vcpu_run` as a trap/exit loop and does not appear to expose KVM-style `ioeventfd`/`irqfd`, so on Darwin the most realistic path is fewer exits and a very lean userspace handling path: https://developer.apple.com/documentation/hypervisor/hv_vcpu_run%28_%3A%29

Implications:

- Per-exit optimization is still worthwhile, but the realistic win is probably incremental unless we find avoidable allocations, locks, or copies in the hot path.
- Exit-count reduction is the bigger prize: better notification suppression, completion coalescing, queue depth, packed virtqueues, virtio-pci experiments, and possibly DAX/read caching.
- Debug timing must stay opt-in. Opcode-level instrumentation is useful, but it is hot enough to distort apt-style workloads.

### Short Exit Probe

`tools/virtiofs_exit_sweep.py` runs a warmed, sub-10-second guest workload designed to expose the current problem quickly:

- create a small source-tree-shaped directory
- repeatedly list and stat the files
- perform one small sequential read
- capture `/debug/virtiofs` and `/debug/exits` before and after the workload

On the 128-file, 4-round probe, the normal cache path spent most of the wall time in virtiofs MMIO exits:

| variant | metadata time | read bandwidth | virtiofs VM exits | avg VM-exit cost | FUSE requests | queue notifies |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 0.763s | 509 MiB/s | 59,716 | 22.9us | 18,472 | 23,030 |
| cache=aggressive | 0.017s | 2,664 MiB/s | 7,084 | 21.7us | 2,364 | 2,364 |
| cache=aggressive, workers=2 | 0.017s | 3,385 MiB/s | 7,080 | 20.1us | 2,364 | 2,364 |
| cache=strict | 0.759s | 590 MiB/s | 59,740 | 22.6us | 18,482 | 23,040 |
| workers=1 | 0.759s | 567 MiB/s | 59,742 | 22.6us | 18,472 | 23,030 |
| workers=2 | 0.735s | 526 MiB/s | 59,744 | 22.4us | 18,472 | 23,030 |
| workers=4 | 0.755s | 568 MiB/s | 59,708 | 22.6us | 18,472 | 23,030 |
| workers=8 | 0.747s | 563 MiB/s | 59,744 | 22.7us | 18,472 | 23,030 |
| sync dispatch | 0.791s | 664 MiB/s | 59,746 | 24.9us | 18,472 | 23,030 |
| writeback | 0.759s | 455 MiB/s | 61,759 | 22.7us | 19,163 | 23,793 |
| direct memory off | 0.748s | 602 MiB/s | 59,742 | 22.5us | 18,472 | 23,030 |

The important result is not that `aggressive` should become the default everywhere. It should not, because host-truth semantics still matter. The useful conclusion is that ordinary Linux VFS caching can erase nearly 90% of the FUSE/MMIO traffic for shell-shaped metadata workloads. The next durable optimization should make that kind of caching safe for the right scopes: guest-root overlay, immutable image trees, and read-only `/host` views first; writable `/host` can stay strict until watcher/invalidation support exists.

The kick-poll experiment was not kept. It can reduce queue notifications, but the current implementation can fail to make forward progress on this probe, so it needs a correctness pass before more tuning.

## External Implementation Notes

### Firecracker

Firecracker is useful as a virtio transport and queue reference, but it is not a direct virtiofs baseline.

The main performance lesson is that Firecracker does not make ordinary userspace MMIO handling magically cheap. On KVM it avoids the hot queue-notify MMIO path by registering each queue notification with `KVM_IOEVENTFD`, and it routes interrupt injection through irqfd-style event delivery. The guest still sees virtio-mmio, but queue notify writes signal eventfds instead of forcing the VMM's normal MMIO handler to do the work.

Firecracker also keeps queue handling very lean:

- cache host-side pointers into descriptor, avail, and used rings after queue setup
- walk descriptors directly from mapped guest memory
- use notification suppression / event-index logic to drain batches before sleeping
- keep fixed, small queue structures and avoid hot-path allocations
- use vhost-user for devices where moving backend work out of the VMM is the right shape

Implications for this project:

- Darwin/HVF probably cannot copy `ioeventfd`/`irqfd` directly; there is no obvious public equivalent. Queue notify exits will still happen, so the HVF path must make each notify exit do as little work as possible.
- The queue-pointer idea does transfer. We should validate and cache ring mappings at queue activation, then replace repeated `ReadIPA`/copy-heavy descriptor reads with direct ring access where the memory backend can safely expose it.
- Completion should batch naturally: write used elements, publish the used index once per drain where possible, and interrupt only when notification rules require it.
- A future Linux/KVM backend should implement ioeventfd/irqfd for virtio queues early, because that is the big Firecracker-style exit-count reduction.

Sources:

- Firecracker MMIO virtio registration: https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/device_manager/mmio.rs
- Firecracker virtio-mmio transport: https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/devices/virtio/transport/mmio.rs
- Firecracker virtqueue implementation: https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/devices/virtio/queue.rs
- Firecracker vhost-user support: https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/devices/virtio/vhost_user.rs

### OpenVMM

OpenVMM is a better reference for a portable, structured implementation. It is less aggressive than Firecracker about raw queue pointers, but it has a clean split between transport, queue, and device workers.

Useful patterns:

- common transport core shared by MMIO and PCI
- doorbell registration abstraction for queue notifications
- one event per virtqueue, with queue workers waiting asynchronously on those events
- cached available index for split queues, so a batch drain does not reread `avail.idx` for every descriptor
- explicit arm/suppress kick state around queue draining
- split and packed virtqueue implementations behind one queue API
- guest-memory subranges for descriptor, avail, and used rings
- virtiofs workers that process one queue asynchronously and write FUSE replies directly into guest payload buffers
- virtiofs DAX/shared-memory hooks through a `MappedMemoryRegion` mapper

OpenVMM's design suggests a middle path for this codebase:

- Keep our Go implementation simple, but introduce a real queue object that owns validated ring mappings, cached indices, notification state, and completion batching.
- Split "queue transport work" from "virtiofs backend work" more sharply. The notify exit should signal/drain queue work and get out; FUSE dispatch should run elsewhere.
- Add packed-ring support as an optimization track after split-ring direct mapping is stable.
- Preserve a backend abstraction for doorbells and interrupt events. On HVF this may stay as userspace MMIO handling; on KVM it should become ioeventfd/irqfd; on Windows it should map to the closest WHP/OpenVMM-style trigger/event support if available.
- Treat DAX/shared-memory mapping as a separate, explicit `/host` optimization mode because host mutation semantics matter.

Sources:

- OpenVMM guide networking architecture, especially frontend/backend queue separation: https://openvmm.dev/guide/reference/backends/networking.html
- OpenVMM virtio transport core: https://github.com/microsoft/openvmm/blob/main/vm/devices/virtio/virtio/src/transport/core.rs
- OpenVMM virtio-mmio transport: https://github.com/microsoft/openvmm/blob/main/vm/devices/virtio/virtio/src/transport/mmio.rs
- OpenVMM virtqueue implementation: https://github.com/microsoft/openvmm/blob/main/vm/devices/virtio/virtio/src/queue.rs
- OpenVMM split queue implementation: https://github.com/microsoft/openvmm/blob/main/vm/devices/virtio/virtio/src/queue/split.rs
- OpenVMM virtiofs device: https://github.com/microsoft/openvmm/blob/main/vm/devices/virtio/virtiofs/src/virtio.rs
- OpenVMM guest memory and doorbell abstractions: https://github.com/microsoft/openvmm/blob/main/vm/vmcore/guestmem/src/lib.rs

## Optimization Order

1. Keep correctness locked down.
   - dpkg/apt installs must remain stable.
   - `/dev/pts`, interactive package prompts, Ctrl+C, HOME, and non-root defaults must keep working.
   - Cache defaults must not hide host changes in surprising ways.

2. Build a tight benchmark harness.
   - Reuse `tools/fs_benchmark.py` where possible.
   - Add profiles and virtiofs stat deltas to every run.
   - Make worker count, async mode, cache mode, and writeback mode easy to sweep.

3. Reduce hot-path copies and allocations.
   - Keep `CCX3_EXIT_TIMING` and timing recorder overhead out of the normal data-abort path.
   - Avoid duplicate device lookup and broad helper layering in the HVF data-abort path.
   - Avoid per-notification slice allocation when harvesting virtiofs work.
   - Avoid concatenating FUSE response header plus payload before writing to guest memory.
   - Pool or stack-encode small replies where useful.
   - Avoid request-buffer allocations for common small requests.
   - Investigate direct guest-memory reads/writes for large READ/WRITE payloads.

4. Exploit concurrency.
   - Enable async dispatch by default once ordering is proven.
   - Tune worker count by benchmark rather than intuition.
   - Verify Linux guest uses all request queues.
   - Keep ordered completion per queue while allowing backend work to run in parallel.

5. Improve cache policy without breaking host truth.
   - Strict remains available for debugging.
   - Normal mode should be correct first.
   - Add invalidation/watch-based host truth later if TTLs become necessary for performance.

6. Backend-specific optimization.
   - Reduce global locking in `imageFS`.
   - Keep passthrough operations out from under broad locks.
   - Optimize rootfs overlay metadata operations because package managers hammer them.
   - Keep virtiofs useful even after virtual ext4 becomes available for rootfs.

7. Compare against external baselines.
   - QEMU virtiofs with comparable cache/queue/thread settings.
   - QEMU virtio-blk/ext4 for rootfs-style workloads.
   - Firecracker only where the device model is comparable, usually block/vsock/net rather than virtiofs.

## First Implementation Push

- Turn async virtiofs dispatch on by default, with `CCX3_VIRTIOFS_ASYNC=0` as a debug fallback.
- Add `CCX3_VIRTIOFS_WORKERS` so the benchmark runner can sweep concurrency.
- Remove the FUSE response concatenation copy by writing the response header and payload as separate segments into guest descriptors.
- Extend benchmark output enough to compare worker/cache/writeback configurations without reading raw debug JSON.
