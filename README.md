# cc

`cc` is an experimental microVM runtime for OCI-backed Linux workloads. It
imports OCI, SIMG/SIF, and CVMFS-backed images, boots a managed Linux guest,
mounts image environments through virtio-fs, and executes commands through a
local HTTP/WebSocket daemon.

The runtime is workload-centric rather than hypervisor-centric: callers manage
images, instances, execs, and shares without providing kernels, wiring boot
devices, or running a privileged helper daemon.

This repository is published at
[github.com/tinyrange/cc](https://github.com/tinyrange/cc).

## Status

Supported host backends:

- `linux/amd64` via KVM
- `linux/arm64` via KVM
- `darwin/arm64` via HVF

The `linux/amd64` backend supports native amd64 images, one-shot command
execution, persistent VMs, writable host shares, local SIMG containers, remote
Neurodesk CVMFS containers, and attaching additional image environments inside a
running VM. Known foreign-architecture images are rejected on `linux/amd64`;
arm64 guest emulation is not implemented there yet.

Runtime networking and snapshots are roadmap features. Snapshots are currently
treated as a future performance optimization rather than an MVP 1 requirement.

## Requirements

- Go 1.25 or newer, matching `go.mod`
- A supported host architecture
- Linux KVM hosts: `/dev/kvm`, regular-user permission to open it, and hardware
  virtualization enabled. Some distributions grant this automatically; others
  require one-time host configuration outside `cc`.
- Network access for kernel downloads, OCI pulls, or CVMFS-backed containers

Quick KVM check:

```sh
test -r /dev/kvm -a -w /dev/kvm && echo "KVM is accessible"
```

## Build

```sh
git clone https://github.com/tinyrange/cc.git
cd cc
go build ./cmd/cc
go build ./cmd/ccvm
```

For a throwaway local smoke test:

```sh
tmp="$(mktemp -d)"
go build -o "$tmp/cc" ./cmd/cc
go build -o "$tmp/ccvm" ./cmd/ccvm
```

## CLI Usage

`cc` starts `ccvm` automatically when needed. If the binaries are not installed
next to each other, pass the daemon path explicitly:

```sh
cc -ccvm ./ccvm kernel-download
cc -ccvm ./ccvm vm-supported
```

The daemon also exposes a capability summary at `GET /capabilities`, including
the active host backend, VM support state, instance concurrency, supported share
semantics, and roadmap feature slots for networking and snapshots.

VM boot waits default to 5 seconds. Set `CCX3_VM_BOOT_TIMEOUT` to a positive
number of seconds when running on slower hosts or diagnosing long boots.

Run the small tracked Alpine bringup SIMG:

```sh
cc -ccvm ./ccvm pull alpine ./fixtures/alpine.simg
cc -ccvm ./ccvm run alpine sh -lc 'cat /etc/alpine-release'
```

Run directly from Neurodesk CVMFS:

```sh
cc -ccvm ./ccvm pull niimath-cvmfs \
  https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016

cc -ccvm ./ccvm run niimath-cvmfs niimath -help
```

Persistent VM flow:

```sh
cc -ccvm ./ccvm vm-start niimath-cvmfs
cc -ccvm ./ccvm run niimath-cvmfs niimath -help
cc -ccvm ./ccvm vm-stop
```

The HTTP API supports named instances through an optional `id` field on VM and
exec requests. The CLI continues to use the default instance for simple local
workflows.

## Python Client

The Python package lives in `pyneurodesk/` and is published as `neurodesk`. It
can start or connect to the daemon, import Neurodesk containers from CVMFS, and
expose container commands through Python or shell wrappers.

```sh
pip install neurodesk
```

```sh
cd pyneurodesk
uv run pytest
```

Example:

```python
import neurodesk as nd

nm = nd.container("niimath")
print(nm.run("niimath", "-help"))
```

See [pyneurodesk/README.md](pyneurodesk/README.md) for Python-specific usage.

## Tests

Fast checks:

```sh
go test ./...
GOOS=linux GOARCH=amd64 go test ./...
GOOS=linux GOARCH=arm64 go test -run '^$' ./...
```

Live Linux KVM checks are opt-in:

```sh
CCX3_KVM_BOOT=1 go test ./internal/hv/kvm ./internal/vm -run 'Test(KernelBootSerial|InitramfsBootReadyMarker|RuntimeBackendRunCommand)' -count=1 -v
```

Live Neurodesk CVMFS execution is also opt-in:

```sh
CCX3_KVM_BOOT=1 CCX3_CVMFS_LIVE=1 \
  go test ./internal/vm -run 'TestRuntimeBackendRunNiimathFromCVMFSPath' -count=1 -v
```

Python tests:

```sh
cd pyneurodesk
uv run pytest
```

## Repository Layout

- `cmd/cc`: user-facing CLI
- `cmd/ccvm`: local HTTP/WebSocket daemon
- `internal/hv`: host virtualization support
- `internal/vm`: runtime backend orchestration
- `internal/oci`: OCI, SIMG/SIF, and CVMFS image import
- `internal/cvmfs`: minimal remote CVMFS catalog and file client
- `pyneurodesk`: Python client and shell integration
- `PLAN.md`: linux/amd64 support plan and milestone notes
