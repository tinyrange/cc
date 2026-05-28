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
