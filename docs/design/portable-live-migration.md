# Portable live migration

## Status and scope

This document defines the implementation boundary and staged delivery plan for
portable live migration. It deliberately does not reuse the current KVM, WHP,
or HVF startup-snapshot manifests as a wire format. Those manifests contain
backend representations and are useful only for local restore experiments.

The product targets are:

- amd64 Windows/WHP to amd64 Linux/KVM; and
- arm64 macOS/HVF to arm64 Linux/KVM on a Raspberry Pi.

Migration stays within one guest architecture. The first implementation target
is Windows/WHP to Linux/KVM because both backends already capture amd64 startup
state and use the same cc-managed machine layout. The arm64 path remains a
required target, but follows the portable amd64 checkpoint so GIC and CPU-ID
compatibility can be solved without changing the container format.

The first implementation is intentionally constrained to a managed Linux guest
with one vCPU, a fixed memory layout, and the cc-owned device profile. Arbitrary
firmware guests, host CPU pass-through, nested virtualization, PCI hotplug,
writable host shares, and live host-bound exec streams are excluded until the
base migration transaction is proven.

## Design principles

1. A checkpoint describes architectural and cc device state, never a KVM, WHP,
   or HVF structure.
2. A destination must reject incompatibility before the source pauses.
3. Exactly one host owns a runnable guest. Until commit, that host is the
   source.
4. Failure before commit resumes the source and destroys the destination
   candidate. Failure after commit never resumes the source automatically.
5. Resources that cannot be transferred or reconstructed are rejected during
   preflight; they are not silently dropped.
6. The migration layer accepts an authenticated, confidential byte stream. The
   remote control layer owns mTLS, peer identity, authorization, discovery, and
   certificate renewal.
7. Correct stop-and-copy comes before pre-copy optimization. A backend without
   usable dirty-page tracking remains supported through explicit stop-and-copy
   policy rather than an unsafe approximation.

## Architecture

Migration is split into three packages with no hypervisor types crossing their
boundaries:

- `migration/model` owns versioned portable state and validation.
- `migration/transaction` owns negotiation, transfer, ownership, and rollback.
- each hypervisor backend implements capture, dirty-memory, and restore adapters
  between its native API and `migration/model`.

The core backend contract should be equivalent to:

```go
type Source interface {
	Describe(context.Context) (model.Offer, error)
	BeginDirtyTracking(context.Context) (DirtyTracker, error)
	Pause(context.Context) error
	Capture(context.Context) (model.State, MemoryReader, error)
	Resume(context.Context) error
	Stop(context.Context) error
}

type Destination interface {
	Probe(context.Context, model.Offer) (model.Acceptance, error)
	Prepare(context.Context, model.State) (Candidate, error)
}

type Candidate interface {
	ApplyMemory(context.Context, model.MemoryChunk) error
	Restore(context.Context, model.State) error
	ResumeIsolated(context.Context) error
	Commit(context.Context) error
	Destroy(context.Context) error
}
```

These names are illustrative. The important contract is that preparation and
restore cannot expose a running guest, and that commit is a separate ownership
operation.

## Portable checkpoint

The checkpoint is a small manifest plus content-addressed binary chunks. The
manifest is deterministic, size-bounded, and versioned independently from the
transport protocol. Every chunk carries its length and SHA-256 digest; the
receiver verifies both before making it visible to a backend.

The manifest contains:

- format major/minor version and required feature identifiers;
- migration ID, generation, guest architecture, and machine profile;
- page size, memory ranges, and ordered memory-chunk descriptors;
- one architectural vCPU record per vCPU;
- interrupt-controller and timer records;
- cc device records keyed by stable device identity;
- reconstructible resource descriptors;
- source clock sample and monotonic-time policy; and
- a digest over the canonical manifest.

Unknown required features or a different major version are fatal. Unknown
optional fields can be ignored. Minor versions may only add optional fields;
semantic changes require a new required feature or major version.

The decoder must reject duplicate identities, overlapping memory ranges,
integer overflow, impossible lengths, missing required device records, and
unreferenced or multiply referenced chunks before allocating guest memory.
Limits are derived from the negotiated machine profile and destination
capacity, not hard-coded guesses about a normal VM size.

### CPU profiles

The source does not export its host CPUID or ID registers. Each guest boots with
a named synthetic CPU profile, and the same profile is recreated at the
destination.

`cc-amd64-v1` is an explicit CPUID/MSR allowlist, including the XSAVE layout,
APIC mode, physical-address width, TSC behavior, and topology visible to the
guest. KVM and WHP adapters must each prove they can implement every required
bit. Migration is rejected when they cannot. Vendor-specific optional features
are disabled for this profile even if both current hosts happen to expose them.

The amd64 vCPU record stores architectural general, control, segment, debug,
floating-point, XSAVE, interrupt, and approved MSR state. Backend-only padding,
ioctl structures, WHP register blobs, and synthetic hypervisor leaves are not
portable state.

`cc-arm64-v1` similarly names an explicit ID-register profile, exception level,
page-granule set, timer frequency contract, and SVE policy. It stores general,
system, floating-point/SIMD, exception, and virtual-timer state by architectural
register name. SVE is disabled in the first profile.

The arm64 adapter milestone must resolve the current platform mismatch: the HVF
machine uses GICv3 while the current KVM arm64 managed machine uses GICv2. The
portable profile will use one GIC version and topology on both hosts; translating
an active GICv3 into GICv2 state is not an accepted shortcut.

### Platform and device state

The portable platform schema models behavior rather than backend registers:

- amd64 local APICs, IOAPIC, legacy PIC/PIT where present, HPET, CMOS/RTC, and
  the guest-visible clock;
- arm64 GIC distributor, redistributors, CPU interfaces, and architected virtual
  timer; and
- the fixed memory and interrupt routing table for the selected machine profile.

Each cc-owned device gets its own versioned record. The first profile includes
virtio queue configuration and indices, negotiated features, interrupt status,
and device-specific state for console, vsock, RNG, balloon, network, and each
filesystem device actually attached to the guest. Device state must be captured
only after request-processing goroutines are quiesced so no completion is lost
between memory and queue state.

Resources are classified during preflight:

- **portable**: memory, architectural CPU state, and cc-owned device state;
- **reconstructible**: immutable image layers identified by digest, a new entropy
  source, control vsock listener, and destination-side user-mode networking; or
- **host-bound**: writable host directories, open host files without a portable
  backing identity, active forwarded host sockets, and in-flight exec/control
  streams.

The first profile requires every immutable image digest at the destination and
rejects host-bound resources. Network interfaces retain their guest MAC and IP
configuration, but existing host TCP/UDP connection tracking is not migrated in
the first milestone. The destination remains externally isolated until commit.
Vsock control reconnects after resume; an in-flight request is failed at the
source with a structured migration interruption.

RNG state is not copied as a reusable entropy stream. Pending queue metadata is
made consistent, then the destination supplies fresh host entropy. Guest wall
clock is advanced to at least the destination wall clock, while monotonic guest
time must never move backwards.

## Transaction protocol

Every migration has a random 128-bit ID and a monotonically increasing guest
generation. Messages include the migration ID, generation, protocol version,
and sequence number. Replayed messages from an older generation are rejected.
Transport framing places bounded metadata ahead of bounded or streamed binary
chunks; it does not require buffering all guest memory.

The transaction is:

1. **Offer**: source sends the machine/resource description and required
   features while the guest continues running.
2. **Accept**: destination probes CPU, memory, backend, disk/image, and device
   compatibility, reserves capacity, and returns its supported transfer modes.
3. **Copy**: source sends base memory and any immutable metadata. With dirty
   tracking, it may send repeated dirty generations while the guest runs.
4. **Freeze**: source stops accepting new guest operations, quiesces cc devices,
   pauses every vCPU, and captures the final dirty pages and portable state.
5. **Restore**: destination verifies the complete checkpoint, restores a
   non-runnable candidate, then resumes it with external network delivery still
   blocked.
6. **Ready**: destination proves the guest control channel is alive and returns
   the restored generation and state digest.
7. **Commit**: source records the handoff decision, tells the destination to
   publish network ownership, receives a commit acknowledgement, then destroys
   its paused VM.

The source remains authoritative through `Ready`. A disconnect or error before
the commit decision destroys the candidate and resumes the source. Once the
source has durably recorded commit, it must not resume the old guest. An
ambiguous post-commit disconnect is reported for explicit reconciliation; it
does not trigger automatic dual execution.

The journal needs only migration ID, guest ID/generation, peer identity, current
phase, state digest, and commit decision. It is not a general transaction log or
automatic daemon-recovery system. On restart it prevents an old source from
resuming a committed generation and gives an operator enough information to
query the peer.

Cancellation before freeze ends the transfer without guest impact. Cancellation
during freeze follows the same pre-commit rollback. No fixed migration timeout
is embedded in the library: the caller supplies policy based on observed link
throughput, dirty rate, memory size, and requested downtime. Individual network
and backend operations still accept contexts and cannot wait after cancellation.

## Memory transfer and downtime

The correctness milestone is stop-and-copy. It copies all guest memory after
pause and proves portable restore before adding concurrency to the capture path.

Pre-copy is enabled only when a backend adapter can provide generation-safe
dirty bitmaps. A round clears or advances the tracking generation, reads the
marked pages, and transfers content tagged with that generation. The final
paused round always wins over earlier chunks. The model must tolerate a page
being sent in multiple rounds but never accept conflicting chunks within one
generation.

Convergence uses measured values rather than a default number of rounds or a
default timeout. After each round the coordinator estimates dirty bytes per
second, effective link throughput, and predicted final-copy time. Caller policy
chooses a maximum downtime or stop-and-copy-only mode. If the prediction cannot
meet that policy, migration fails before freeze or continues only after an
explicit policy decision.

WHP and KVM adapters must document the exact dirty-log API and the race-free
clear/read sequence they use. If WHP cannot provide the required generation
semantics, the first cross-backend release remains stop-and-copy. HVF dirty
tracking is a separate adapter investigation; write-protect trapping is only
acceptable if benchmarks show it does not make the running workload unusable.
Post-copy is out of scope because a source or network failure would make the
destination VM unrecoverable.

## Failure and consistency rules

- The destination validates all advertised compatibility before source freeze.
- A candidate cannot send or receive external network traffic before commit.
- A source cannot resume after recording a commit decision.
- A destination cannot publish a candidate whose state digest differs from the
  source's final digest.
- Rollback removes all candidate memory and temporary artifacts.
- Temporary checkpoint files are owner-only and never contain credentials.
- Logs and errors identify the phase and peer but do not include guest memory,
  command payloads, certificate material, or host paths not already authorized
  for display.
- A migration involving a writable disk requires a future storage ownership
  protocol. Copying bytes while both hosts can write is forbidden.

Protocol errors are typed as incompatible profile, unsupported resource,
capacity unavailable, corrupt checkpoint, source capture failure, destination
restore failure, authorization failure, cancelled, or ambiguous commit. A
human-readable explanation may add context but is not the programmatic contract.

## Delivery plan

### Phase 0: portable model and offline adapters

- Add the versioned model, canonical encoder/decoder, validation, and chunk
  store interfaces.
- Build explicit WHP and KVM translations for the `cc-amd64-v1` one-vCPU
  profile.
- Capture an offline WHP checkpoint and restore it under KVM without a network
  transport.
- Keep the existing startup snapshot formats private and unchanged.

Exit gate: a Linux guest paused after boot resumes under KVM from WHP-produced
portable state and runs a new command through a reconnected control channel.

### Phase 1: transactional stop-and-copy

- Add offer/probe, capacity reservation, streamed chunks, freeze/restore,
  readiness, commit, and rollback.
- Reject writable shares, open forwards, active exec streams, and unsupported
  devices before freeze.
- Integrate the authenticated remote byte stream without putting identity or
  certificate policy into cc's state model.

Exit gate: repeated Windows/WHP to Linux/KVM migrations preserve guest memory,
filesystem visibility, clock monotonicity, and single-host ownership across
success, cancellation, corruption, destination failure, and connection loss.

### Phase 2: pre-copy

- Add generation-safe WHP and KVM dirty tracking.
- Report dirty rate, transfer throughput, total bytes, pause duration, and
  rollback duration.
- Select freeze based on caller downtime policy and measured convergence.

Exit gate: the reference dirty-memory workload completes with no lost writes,
and measured downtime is lower than stop-and-copy on the required hardware.
The implementation PR records at least ten runs of both modes and sets any
release threshold from those results rather than inventing a universal default.

### Phase 3: arm64 HVF to KVM

- Define `cc-arm64-v1` and converge both backends on one GIC machine profile.
- Add architectural HVF and KVM arm64 adapters without translating opaque
  backend blobs.
- Implement stop-and-copy first, then investigate a measured dirty-page path.

Exit gate: the same correctness workload migrates from an Apple Silicon MacBook
to a Raspberry Pi 5 KVM host and continues through the reconnected control
channel with exactly one runnable generation.

### Phase 4: resource continuity

Add writable storage transfer, connection-preserving networking, host shares,
and active operations one resource class at a time. Each class needs its own
ownership, quiesce, rollback, and user-visible failure contract. None is implied
by completion of the base memory/CPU migration.

## Validation strategy

Pure model tests should assert structured fields and behavior:

- canonical round trips and stable digests;
- malformed lengths, overlaps, duplicate identities, unknown requirements, and
  resource-limit rejection;
- fuzzing decoders and state validators;
- transaction state-machine properties, especially no dual ownership;
- deterministic injected failures at every phase; and
- dirty-generation ordering with writes racing a copy round.

Backend tests should compare architectural state before and after a same-backend
round trip, then use the same fixtures across WHP and KVM. Golden fixtures are
appropriate only for the documented portable format, not raw backend state.

The hardware acceptance workload continuously:

- increments and verifies a monotonic in-memory sequence;
- writes checksummed data to guest memory and an allowed portable filesystem;
- reads wall and monotonic clocks;
- exchanges network traffic while recording, but not initially requiring,
  connection continuity; and
- executes a command after the destination control channel reconnects.

Fault injection disconnects the transport, kills the destination candidate,
corrupts a chunk, exhausts destination capacity, and cancels at each transaction
phase. The externally observed invariant is one runnable generation with either
a successful destination or a resumed source.

## Required hardware

Development and release validation require four physical hosts:

- an x86_64 Windows 11 machine with Windows Hypervisor Platform enabled;
- an x86_64 Linux machine with KVM access;
- an Apple Silicon MacBook capable of running the existing HVF backend; and
- a Raspberry Pi 5 with 8 GB RAM, a 64-bit Linux kernel, and working KVM access.

Each source/destination pair needs direct Tailscale connectivity and enough free
RAM for the selected guest on both ends. The test report records CPU model,
firmware, host OS/kernel build, cc commit, negotiated CPU profile, memory size,
link throughput, and whether the run used stop-and-copy or pre-copy. The design
does not assume that successful migration between one pair of CPUs makes an
unadvertised CPU feature portable.

## Decisions deferred to implementation evidence

The following are deliberately not guessed in this design:

- a universal downtime SLA;
- a fixed number of pre-copy rounds;
- a fixed migration timeout;
- whether WHP and HVF dirty tracking is efficient enough for default pre-copy;
  and
- which additional host-bound resource should be made portable first.

Implementation measurements decide those values. They do not change the
portable-state boundary, fail-closed compatibility checks, or single-owner
transaction rules defined here.
