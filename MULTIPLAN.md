# Multi-VM Host Plan

## Goal

Support many named VMs on every host while keeping one portable daemon API.

The immediate reason is macOS/HVF: Apple's HVF VM creation limit is effectively
per process, so one daemon process can only host one HVF VM. Virtualization
framework avoids this by using helper processes. We should use the same shape,
but make it a general placement model rather than a macOS-only workaround.

The target architecture is:

- one `ccvmd` coordinator process
- zero or more VM host backends
- VM hosts may be in-process, local sidecar processes, remote hosts later, or
  nested hosts inside VMs
- coordinator-owned image, filesystem, and network services
- worker-owned hypervisor state and virtio frontends

## Implementation Status

The first functional sidecar path is implemented for `darwin/arm64`:

- the coordinator uses the in-process HVF backend for the first VM
- additional VMs are placed in local `ccvmd -worker` sidecar processes
- sidecars are launched from the same `ccvm` executable, including embedded
  `vsh --vsh-internal-ccvm` mode
- named VM start, status, shutdown, port forward, and exec routing work through
  the existing daemon API

Verified locally on macOS arm64 by starting two named Alpine VMs at the same
time and running `uname -m` in both.

The current sidecar path still shares the on-disk image store directly. The
next deeper architecture step is moving virtio-fs and virtio-net backends behind
coordinator-owned IPC services so save/snapshot and L2 networking are fully
coordinator-owned across sidecars.

Linux/KVM and Windows/WHP should continue to run multiple VMs in-process when
that is the best local placement. macOS/HVF should use one in-process VM if
available, then launch sidecar workers for additional VMs.

## Current Shape

The useful seam already exists in `internal/vm`:

- `Manager` owns named VM state, lifecycle, capacity checks, and status.
- `Backend` owns host-specific VM creation and exec routing.
- `Instance` owns per-VM operations such as exec, shares, port forwards, wait,
  and close.

Today `Manager` assumes one backend in the daemon process. Capabilities are
reported by `HostCapabilities`; on `darwin/arm64` it reports `MaxInstances = 1`.

The next design should keep `Manager` as the user-facing lifecycle coordinator,
but split "backend" into placement and VM host operations.

## Terminology

- **Coordinator**: the main `ccvmd` process. It owns the HTTP/WebSocket API,
  image store, global VM registry, placement, L2 switch, and shared filesystem
  backends.
- **VM host**: an execution location capable of hosting one or more VMs. It may
  be in-process, a local sidecar process, or a remote endpoint.
- **Worker**: a local sidecar VM host process started by the coordinator.
- **VM handle**: coordinator-side object for one running VM, regardless of where
  it is placed.
- **Virtio service**: coordinator-owned service that handles backend work for a
  virtio device whose frontend lives in a worker.

## Ownership

### Coordinator owns

- HTTP/WebSocket API
- image store metadata and image pull/save publication
- VM id to VM host/worker mapping
- placement and capacity accounting
- worker process lifecycle and cleanup
- Unix socket paths and auth tokens for local workers
- filesystem service for rootfs, image mounts, and host shares
- network switch, DHCP/IPAM, NAT, and port forwards
- status aggregation

### VM host or worker owns

- host hypervisor API objects
- vCPU threads
- guest RAM mappings
- MMIO/PIO exit loop
- virtio transport/frontends
- virtqueue parsing enough to translate queue work to coordinator services
- console/serial frontend and per-VM lifecycle
- guest init control connection for exec start/input/signal/resize

The worker should not mutate shared image store state directly. It should not
own the global L2 network. It should not publish saved images.

## Device Boundary

### Filesystem

Virtio-fs frontend lives beside the VM. The backend filesystem state lives in
the coordinator.

```text
guest virtio-fs
  <-> worker virtqueue/frontend
  <-> Unix socket FS RPC
  <-> coordinator FS backend
  <-> image store / overlay / host mounts
```

This keeps on-disk state authoritative in one process. It also makes save
straightforward: the coordinator already owns the rootfs/image mount backend and
can flush, snapshot, and register the saved image.

The worker still has to handle virtqueue mechanics and guest memory copies. The
coordinator handles FUSE operation semantics, overlay/copy-up, host-backed
shares, metadata, and snapshot export.

### Network

Virtio-net frontend lives beside the VM. The L2 switch lives in the coordinator.

```text
guest virtio-net
  <-> worker virtqueue/frontend
  <-> Unix socket packet stream
  <-> coordinator L2 switch
  <-> worker virtio-net
  <-> guest
```

Coordinator responsibilities:

- MAC learning and broadcast/multicast fanout
- VM attachment registry
- DHCP/IPAM
- DNS configuration
- NAT/uplink
- port forwards

Worker responsibilities:

- transmit packets from guest queues to the coordinator
- inject packets received from the coordinator into guest queues
- report link state and device stats

This gives real L2 between VMs even when they live in different worker
processes.

### Console And Exec

Console/serial frontend state should live in the worker. The coordinator should
only proxy and retain optional logs.

Exec control is already guest-init based. Keep the same semantics:

- coordinator receives HTTP/WebSocket exec request
- coordinator routes to the VM handle
- VM handle sends exec control to the owning worker
- worker forwards to guest init and streams events back

For in-process hosts, this can remain direct Go calls. For workers, use the same
logical protocol over the worker socket.

## Placement Model

Replace one daemon-local backend with a small placement layer.

```go
type VMHost interface {
    Capabilities(context.Context) (VMHostCapabilities, error)
    Start(context.Context, client.CreateInstanceRequest, BootEventSink) (VMHandle, error)
    StartBlank(context.Context, client.StartInstanceRequest, BootEventSink) (VMHandle, error)
    Close() error
}

type VMHostCapabilities struct {
    Backend        string
    MaxVMs         int
    Locality       string // in-process, sidecar, remote
    SupportsFSRPC  bool
    SupportsL2     bool
    SupportsNested bool
}
```

Initial placement policy:

1. Prefer in-process host while it has capacity.
2. If capacity is full and the platform supports sidecars, launch a local
   worker host.
3. Later, consider remote hosts or nested hosts.

Expected capacities:

- Linux/KVM: in-process capacity remains high; sidecars are optional.
- Windows/WHP: in-process capacity remains high if stable; sidecars are
  optional.
- macOS/HVF: in-process capacity is one; sidecars are required for additional
  VMs.

`client.CapabilitiesResponse.MaxInstances` should become the coordinator's
placement capacity, not the capacity of a single in-process backend. It should
also report host slots if useful for debugging.

## Worker Protocol

Use Unix sockets for local workers.

Do not use stdio as the main transport. We need bidirectional control streams,
exec streams, filesystem RPC, network packet streams, and boot events. Stdio
would turn into a fragile custom multiplexer.

First worker socket services:

- `Start` or one-shot worker startup handshake
- `Stop`
- `Wait`
- `Status`
- `ExecStream`
- `ConsoleStream`
- `VirtioFSChannel`
- `VirtioNetChannel`

The protocol should be transport-neutral at the interface level. Unix sockets
are the first transport. TCP/QUIC with authentication can be added later for
remote VM hosts.

Local worker startup should include:

- worker id
- VM id
- cache root
- coordinator socket path
- auth token
- host backend request

Workers should exit when their VM exits or when the coordinator closes the
control channel. Coordinator startup should clean stale worker sockets and
state files.

## Filesystem RPC Notes

Do not send high-level paths for every operation if that makes the worker
understand filesystem semantics. The worker should translate virtqueue requests
into a compact FUSE-like RPC and let the coordinator filesystem backend decide.

The first implementation can be simple and correctness-first:

- one request stream per virtqueue
- request id
- operation
- node/handle ids
- payload references copied from guest memory
- response payload copied back by worker

Optimization can follow the existing `VIOFS_PLAN.md` direction:

- fewer allocations
- batched completions
- direct ring mappings
- worker-side queue batching
- coordinator-side async filesystem dispatch

## Network RPC Notes

Start with Ethernet frames over a length-prefixed stream:

- worker sends frames with VM id and device id
- coordinator switch forwards frames to target worker streams
- broadcast goes to all VMs on the network except source

Keep IPAM and NAT coordinator-local. Do not let workers independently allocate
addresses.

## Implementation Phases

### Phase 1: Interfaces And No-Op Placement

- Introduce `VMHost` and `VMHandle` interfaces beside the existing `Backend`
  and `Instance` contracts.
- Add an in-process host adapter that wraps the current backend.
- Keep behavior unchanged.
- Move capacity accounting from raw `HostCapabilities` into placement.

### Phase 2: Worker Process Skeleton

- Add local worker mode to `ccvm`.
- Launch worker process from coordinator over Unix socket.
- Implement handshake, status, stop, wait, and boot event forwarding.
- Worker may initially own all devices locally to prove lifecycle and HVF
  multi-process startup.

### Phase 3: Remote Virtio-FS

- Move filesystem backend ownership to coordinator for sidecar workers.
- Worker keeps virtio frontend and queue handling.
- Coordinator handles rootfs/image/share backend operations.
- Ensure `@save` snapshots through coordinator-owned state.

### Phase 4: Remote Virtio-Net And L2 Switch

- Move packet switching to coordinator.
- Add L2 connectivity between VMs on the same logical network.
- Add coordinator-owned DHCP/IPAM.
- Rehome port forwarding through coordinator network state.

### Phase 5: General Sidecar Placement

- Allow sidecars on Linux and Windows behind a capability flag.
- Keep in-process placement as the default while capacity is available.
- Add tests that start more VMs than one host slot can hold.

### Phase 6: Remote Hosts

- Replace Unix-socket assumptions in `VMHost` transport with an authenticated
  network transport.
- Support remote or nested VM hosts as placement targets.
- Keep image and filesystem authority in the coordinator unless explicit remote
  caching is designed.

## Testing

Required early tests:

- unit tests for placement capacity and fallback to sidecar hosts
- worker handshake and stale socket cleanup
- macOS live test: start two HVF workers in separate processes
- filesystem RPC golden tests using existing `virtio` request fixtures where
  possible
- L2 switch tests for unicast, broadcast, and VM removal
- integration test: two VMs on macOS ping or TCP connect over coordinator L2
- save test from a sidecar-backed VM

CI should keep the current platform split:

- Linux/amd64 KVM live smoke
- Windows/amd64 WHP live smoke when enabled
- macOS/arm64 unit and compile checks
- macOS live multi-HVF test where a runner is available

## Open Questions

- Should the coordinator ever allow workers to cache immutable image reads
  locally, or should all filesystem data remain coordinator-served at first?
- How much console history should the coordinator retain?
- Should worker processes be long-lived host slots or one process per VM?
  macOS can start with one worker per VM.
- Should sidecar workers be exposed in capabilities for debugging?
- What authentication is enough for local Unix sockets before remote hosts exist?
