# Networking Implementation Plan

This plan covers adding real guest networking to `cc` by porting the custom
TCP/IP stack and virtio-net device from `../cc-archive`, then wiring them into
the current VM runtime behind an explicit client request.

Networking should be off by default for now. A VM should only receive a network
device, DNS setup, internet access, or port forwards when the client request
explicitly asks for networking.

## Goals

- Add a virtio-net device compatible with the Linux virtio-net driver.
- Add a user-mode TCP/IP stack for guest networking without requiring TAP,
  bridge setup, root privileges, or host network namespace changes.
- Support guest access to the host computer through DNS.
- Support optional guest access to the internet.
- Support host-to-guest TCP port forwarding.
- Keep behavior explicit and testable through client/runtime configuration.

## Non-Goals For The First Version

- No bridged networking.
- No TAP device requirement.
- No IPv6 support.
- No full NAT implementation.
- No arbitrary inbound host-to-guest networking except configured port forwards.
- No hidden enablement through environment variables.

## Source Material

The archive has three main pieces to port:

- `../cc-archive/internal/devices/virtio/net.go`
  - virtio-net MMIO device
  - RX/TX queues
  - MAC configuration
  - mergeable receive buffers
  - checksum handling
- `../cc-archive/internal/devices/virtio/netstack_backend.go`
  - adapter between virtio-net and the network stack
- `../cc-archive/internal/netstack`
  - ARP, IPv4, ICMP, UDP, DNS, TCP helpers, TCP proxy/listener support

The archive virtio implementation is not drop-in. The current repo's virtio
devices under `internal/virtio` each own more of their MMIO and queue handling,
so the virtio-net implementation should be adapted to the current style rather
than copied mechanically.

## Proposed Network Model

Use a small synthetic network:

- host/gateway IP: `10.42.0.1`
- guest IP: `10.42.0.2`
- subnet: `10.42.0.0/24`
- DNS server: `10.42.0.1`
- default host DNS name: `host.containers.internal`

When networking is enabled, the guest gets a virtio-net NIC and static network
configuration. The host-side stack handles ARP, DNS, ICMP, outbound TCP proxying,
and configured host-to-guest port forwards.

## Runtime Configuration

Add an explicit network config that flows from the client/API request down to VM
construction.

Example shape:

```go
type NetworkConfig struct {
    Enabled bool

    // Guest -> internet through host-side proxying.
    AllowInternet bool

    // Guest -> host access.
    HostDNSName string // default: "host.containers.internal"
    HostIP      string // default: "10.42.0.1"

    // Host -> guest access.
    PortForwards []PortForward
}

type PortForward struct {
    Protocol string // "tcp" first; UDP can come later.

    HostAddr string // default: "127.0.0.1"
    HostPort int

    GuestAddr string // default: "10.42.0.2"
    GuestPort int
}
```

Expected behavior:

- `Enabled: false`
  - no virtio-net device
  - no guest network configuration
  - no DNS server
  - no port forward listeners
- `Enabled: true, AllowInternet: false`
  - guest can reach the synthetic host IP
  - guest can resolve the configured host DNS name
  - guest can use configured host/guest port-forward functionality
  - guest cannot reach arbitrary internet destinations
- `Enabled: true, AllowInternet: true`
  - all enabled networking behavior above
  - DNS can resolve external names
  - outbound guest TCP connections can proxy through the host

## Implementation Phases

### 1. Port The Network Stack

Create `internal/netstack` from the archive implementation.

Initial protocol support:

- Ethernet frame parsing
- ARP
- IPv4
- ICMP echo to the gateway IP
- UDP enough for DNS
- DNS server support
- TCP listener/proxy support needed for outbound internet and port forwarding

Implementation notes:

- Update module imports from the archive path to the current module path.
- Keep the stack independent from KVM and virtio for unit testing.
- Decide whether to keep the archive DNS dependency or replace it with a small
  built-in DNS responder for the required records.
- Port or trim packet capture support. It is useful for debugging, but should
  not block the first working version.
- Ensure `Close` stops listeners, goroutines, and open connections.

Deliverable:

```sh
go test ./internal/netstack
```

### 2. Add The virtio-net Device

Create a current-codebase-style virtio-net device under `internal/virtio`.

Expected files:

- `internal/virtio/net.go`
- `internal/virtio/net_test.go`

Device behavior:

- virtio device ID `1`
- two queues:
  - receive queue `0`
  - transmit queue `1`
- stable generated or configured guest MAC address
- virtio-net config space exposing the guest MAC
- 12-byte virtio-net header support
- mergeable receive buffer support
- transmit checksum completion for `VIRTIO_NET_HDR_F_NEEDS_CSUM`
- backend interface for delivering guest TX frames to the network stack
- RX enqueue path for frames produced by the network stack

Testing should cover:

- MMIO identification and config reads
- TX descriptor chain parsing
- RX descriptor writing and interrupt signaling
- mergeable buffer behavior
- checksum helper behavior
- queue shutdown and backpressure behavior

Deliverable:

```sh
go test ./internal/virtio
```

### 3. Wire Networking Into VM Construction

Plumb `NetworkConfig` from client/API request handling into the VM runtime.

Linux/amd64 should be the first integration target because it is the main path
for the full neurocontainer runs. Other backends can follow once the behavior is
proven.

Work items:

- Add MMIO base/size/IRQ layout for virtio-net.
- Attach the virtio-net device only when `NetworkConfig.Enabled` is true.
- Create a `netstack.NetStack` per VM/session when networking is enabled.
- Bind virtio-net to the stack with a backend adapter.
- Include the net device in MMIO dispatch and interrupt updates.
- Close the network stack on VM/session shutdown and timeout cleanup.
- Add required kernel config/module checks for `CONFIG_VIRTIO_NET`.

Deliverable:

- boot a live Linux guest with a visible `eth0` device when networking is
  requested
- boot the same guest with no network device when networking is not requested

### 4. Configure Guest Networking

When networking is enabled, guest init should configure:

- `eth0` up
- `10.42.0.2/24` on `eth0`
- default route via `10.42.0.1`
- `/etc/resolv.conf` with `nameserver 10.42.0.1`
- host DNS entry available through DNS, not only `/etc/hosts`

The first implementation can use guest tools if they are reliably available,
but the robust version should configure the interface without depending on the
container image having `ip` or a particular network manager. A small Go netlink
helper would be preferable if external tooling is missing or inconsistent.

Deliverables:

```sh
ip addr show eth0
ping -c1 10.42.0.1
getent hosts host.containers.internal
```

### 5. Implement DNS Behavior

The DNS server should always provide the configured host name when networking is
enabled:

```text
host.containers.internal -> 10.42.0.1
```

When `AllowInternet` is false:

- return the synthetic host record
- reject, ignore, or return no answer for external names

When `AllowInternet` is true:

- return the synthetic host record
- forward or resolve external names through the host resolver

Tests should verify that disabling internet access prevents external DNS from
turning into usable outbound connectivity.

### 6. Implement Guest Internet Access

When `AllowInternet` is true, allow outbound guest TCP connections to proxy
through the host.

Expected behavior:

- guest opens TCP connection to remote IPv4 destination
- netstack accepts the guest-side TCP flow
- host opens a corresponding outbound TCP connection
- bytes are proxied bidirectionally
- shutdown and half-close behavior is handled well enough for HTTP(S), package
  downloads, and common CLI tools

When `AllowInternet` is false:

- do not proxy arbitrary external TCP connections
- keep host DNS and configured port-forward behavior available

### 7. Implement Host-To-Guest Port Forwarding

Support configured TCP forwards:

```text
127.0.0.1:8080 on host -> 10.42.0.2:80 in guest
```

Implementation shape:

- create host listeners for configured forwards
- for each accepted host connection, open a TCP flow into the guest through the
  netstack
- proxy bytes bidirectionally
- close listeners when the VM/session exits
- fail VM/session startup if a requested host port cannot bind

Initial support should be TCP-only. UDP forwarding can be added later if a real
use case appears.

### 8. Add Client/API Support

Expose the network config through the existing client/API request types.

The important rule is that networking must be explicit. If a client sends no
network config, behavior should remain identical to today's default.

Potential client-facing fields:

- enable networking
- allow internet
- host DNS name override
- port forwards

The Python client should mirror the same behavior where it constructs VM/run
requests.

### 9. Add Integration Tests

Add tests in layers:

Unit tests:

- `internal/netstack`
- `internal/virtio`

Live VM tests:

- networking disabled: no `eth0`
- networking enabled: `eth0` exists
- gateway ping works
- `host.containers.internal` resolves to `10.42.0.1`
- internet disabled blocks external DNS/TCP
- internet enabled can reach a host-side deterministic test server through
  outbound TCP
- TCP port forward from host to guest works

The internet-enabled test should avoid relying on the public internet. Prefer a
local host-side HTTP or TCP server and a custom outbound dialer in the netstack
where possible.

## Suggested First PR Scope

Keep the first implementation tight:

1. Port `internal/netstack` with unit tests.
2. Add `internal/virtio/net.go` with descriptor-level tests.
3. Add explicit `NetworkConfig` plumbing.
4. Wire linux/amd64 KVM.
5. Configure guest `eth0`, route, and DNS.
6. Prove:
   - gateway ping
   - host DNS name
   - outbound TCP with `AllowInternet`
   - one TCP host-to-guest port forward

After that is working, extend to other backends and broaden fulltest coverage.

## Risks And Watch Points

- The archive virtio-net device must be adapted to the current virtio code
  style; direct copying will likely fight the current MMIO/queue architecture.
- Mergeable RX buffers and checksum offload need careful tests because Linux's
  virtio-net driver can fail in non-obvious ways when these are wrong.
- Guest network setup should not rely permanently on container image tooling.
- Network goroutines, listeners, and proxy connections must close reliably on VM
  timeout and cancellation.
- `AllowInternet: false` must be tested as a real isolation mode, not only as a
  DNS setting.
- Port forwarding should fail loudly when requested host ports cannot bind.
