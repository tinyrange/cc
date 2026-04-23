# ccx3

`ccx3` is an experimental container runtime for Neurodesk-style workloads. It
imports OCI, SIMG/SIF, and CVMFS-backed containers, boots a small Linux guest,
mounts the container root filesystem through virtio-fs, and executes commands
through a local HTTP/WebSocket daemon.

## Status

Supported host backends:

- `linux/amd64` via KVM
- `linux/arm64` via KVM
- `darwin/arm64` via HVF

The `linux/amd64` backend supports native amd64 images, one-shot command
execution, persistent VMs, writable host shares, local SIMG containers, and
remote Neurodesk CVMFS containers. Known foreign-architecture images are
rejected on `linux/amd64`; arm64 guest emulation is not implemented there yet.

## Requirements

- Go 1.25 or newer, matching `go.mod`
- A supported host architecture
- Linux KVM hosts: `/dev/kvm`, user permission to open it, and hardware
  virtualization enabled
- Network access for kernel downloads, OCI pulls, or CVMFS-backed containers

Quick KVM check:

```sh
test -r /dev/kvm -a -w /dev/kvm && echo "KVM is accessible"
```

## Build

```sh
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

Run a local SIMG:

```sh
cc -ccvm ./ccvm pull niimath ./local/niimath_1.0.20250804_20251016.simg
cc -ccvm ./ccvm run niimath niimath -help
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

## Python Client

The Python package lives in `pyneurodesk/`. It can start or connect to the
daemon, import Neurodesk containers from CVMFS, and expose container commands
through Python or shell wrappers.

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
