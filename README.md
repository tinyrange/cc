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
- `windows/amd64` via Windows Hypervisor Platform

The stable amd64 backends (`linux/amd64` and `windows/amd64`) support native
amd64 images, one-shot command execution, persistent VMs, writable host shares,
local SIMG containers, remote Neurodesk CVMFS containers, and attaching
additional image environments inside a running VM. Known foreign-architecture
images are rejected on amd64 hosts; arm64 guest emulation is not implemented
there yet.

Runtime networking and snapshots are roadmap features. Snapshots are currently
treated as a future performance optimization rather than an MVP 1 requirement.

## Requirements

- Go 1.25 or newer, matching `go.mod`
- A supported host architecture
- Linux KVM hosts: `/dev/kvm`, regular-user permission to open it, and hardware
  virtualization enabled. Some distributions grant this automatically; others
  require one-time host configuration outside `cc`.
- Windows amd64 hosts: Windows Hypervisor Platform enabled and hardware
  virtualization available.
- Network access for kernel downloads, OCI pulls, or CVMFS-backed containers

Quick KVM check:

```sh
test -r /dev/kvm -a -w /dev/kvm && echo "KVM is accessible"
```

## Remote daemon TLS

The default `ccvm` listener is local-only. Any wildcard, non-loopback IP, or
hostname other than `localhost` requires mutual TLS and fails before startup
without it. This applies to health, control, debug, streaming, and WebSocket
routes; a bearer-token wrapper or firewall rule is not treated as remote
authentication.

Put the listener configuration in an owner-only JSON file:

```json
{
  "certificate_file": "server.crt",
  "private_key_file": "server.key",
  "client_ca_file": "clients.pem"
}
```

Relative paths are resolved beside the configuration file. On Unix, both the
configuration and private key must be inaccessible to group and other users.
Start a remote listener with:

```sh
ccvm --addr 100.64.0.10:8080 --tls-config /secure/ccvm-tls.json
```

`ccvm` requires TLS 1.3 and a verified client certificate. Any certificate
chaining to `client_ca_file` has full daemon API authority, so use a dedicated
client CA, issue short-lived certificates, and keep its signing key outside the
daemon host. Tailscale can provide reachability but does not replace this
application-layer authentication.

The server certificate, private key, and client CA bundle are reloaded for each
new TLS connection, and TLS session resumption is disabled so new connections
always reauthenticate. Rotate files with atomic replacement. For CA rotation,
publish an overlap bundle containing old and new client CAs, rotate clients,
then remove the old CA. Existing TLS connections keep their authenticated
state; new connections fail closed if the rotated files are missing or invalid.
Changing the configured file paths requires a daemon restart. TLS termination
at a reverse proxy is not accepted by this listener mode.

## Build

```sh
git clone https://github.com/tinyrange/cc.git
cd cc
go build ./cmd/cc
go build ./cmd/ccvm
```

For a throwaway local build:

```sh
tmp="$(mktemp -d)"
go build -o "$tmp/cc" ./cmd/cc
go build -o "$tmp/ccvm" ./cmd/ccvm
```

## CLI Usage

`cc` starts `ccvm` automatically when needed. If the binaries are not installed
next to each other, pass the daemon path explicitly:

```sh
cc -ccvm ./ccvm doctor
cc -ccvm ./ccvm status
```

The daemon also exposes a capability summary at `GET /capabilities`, including
the active host backend, VM support state, instance concurrency, supported share
semantics, and roadmap feature slots for networking and snapshots.

VM boot waits default to 5 seconds. Set `CCX3_VM_BOOT_TIMEOUT` to a positive
number of seconds when running on slower hosts or diagnosing long boots.

Long-running CLI operations use the daemon's streaming endpoints. Human progress
for `doctor`, `pull`, and `start` is written to stderr when stderr is a terminal;
stdout remains reserved for command output and JSON responses.

Run the small tracked Alpine bringup SIMG:

```sh
cc -ccvm ./ccvm pull alpine ./fixtures/alpine.simg
cc -ccvm ./ccvm run alpine -- sh -lc 'cat /etc/alpine-release'
```

Run directly from Neurodesk CVMFS:

```sh
cc -ccvm ./ccvm pull niimath-cvmfs \
  http://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016

cc -ccvm ./ccvm run niimath-cvmfs -- niimath -help
```

Persistent VM flow:

```sh
cc -ccvm ./ccvm start niimath-cvmfs
cc -ccvm ./ccvm run niimath-cvmfs -- niimath -help
cc -ccvm ./ccvm stop
```

Named VM flow:

```sh
cc -ccvm ./ccvm vm start work-a alpine
cc -ccvm ./ccvm vm start work-b niimath-cvmfs
cc -ccvm ./ccvm vm list
cc -ccvm ./ccvm vm run work-a -- sh -lc 'cat /etc/alpine-release'
cc -ccvm ./ccvm vm status work-b
cc -ccvm ./ccvm vm stop work-a
cc -ccvm ./ccvm vm stop work-b
```

Port forwarding is available for named VMs with a `HOST_PORT:GUEST_PORT`
mapping:

```sh
cc -ccvm ./ccvm vm forward work-a 8080:80
```

The simple `start`, `stop`, `status`, and `run` commands continue to operate on
the default VM. The daemon also supports named instances through `id` fields on
VM and exec requests and through `GET /vm` for listing. Reported
`max_instances` is a daemon concurrency limit, not a guarantee that the host has
enough free memory or CPU for that many guests.

## Python Client

The Python package lives in `pyneurodesk/` and is published as `neurodesk`. It
can start or connect to the daemon, import Neurodesk containers from CVMFS, and
expose container commands through Python or shell wrappers.

```sh
pip install neurodesk
```

Example:

```python
import neurodesk as nd

nm = nd.container("niimath")
print(nm.run("niimath", "-help"))
```

See [pyneurodesk/README.md](pyneurodesk/README.md) for Python-specific usage.

## Repository Layout

- `cmd/cc`: user-facing CLI
- `cmd/ccvm`: local HTTP/WebSocket daemon
- `internal/hv`: host virtualization support
- `internal/vm`: runtime backend orchestration
- `internal/oci`: OCI, SIMG/SIF, and CVMFS image import
- `internal/cvmfs`: minimal remote CVMFS catalog and file client
- `pyneurodesk`: Python client and shell integration
- `PLAN.md`: linux/amd64 support plan and milestone notes
