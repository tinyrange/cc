# Multi-VM Plan

## Goal

Support multiple concurrently running VMs on one host, starting with Linux/KVM.
The implementation should expose named VMs through a clearer `cc vm ...` user
interface while reusing the daemon's existing named-instance plumbing.

## Current State

The core daemon is already close:

- `internal/vm.Manager` stores running machines in `map[string]*Machine`.
- `client.CreateInstanceRequest`, `client.StartInstanceRequest`,
  `client.RunRequest`, and `client.ExecRequest` already carry optional `ID`.
- `ccvm` already routes some requests by id:
  - `GET /vm/status?id=...`
  - `GET /vm`
  - `POST /vm/shutdown?id=...`
  - `POST /vm/forward?id=...`
- `Manager.Statuses()` can list running VMs.
- Linux capabilities currently report `max_instances: 64`.

The public tools still mostly behave like there is one default VM:

- `cc start`, `cc stop`, `cc status`, and `cc run` target the default instance.
- Go client convenience methods mostly operate on the default instance.
- PyNeurodesk shell/session code mostly assumes one shared active VM.

## Proposed CLI

Keep simple commands for the default VM:

```text
cc start <image>
cc stop
cc status
cc run <image> -- <cmd...>
```

Add an explicit VM namespace for named/multi-VM work:

```text
cc vm list
cc vm start <name> <image>
cc vm stop <name>
cc vm status <name>
cc vm run <name> -- <cmd...>
cc vm forward <name> <HOST_PORT:GUEST_PORT>
```

Potential later additions:

```text
cc vm logs <name>
cc vm inspect <name>
cc vm stop --all
```

`cc vm run <name>` should execute in the named VM. The image can be inferred
from the running VM, so the user does not need to repeat it.

The existing default commands can be treated as sugar for a VM named `default`:

```text
cc start alpine        == cc vm start default alpine
cc stop                == cc vm stop default
cc status              == combined host/default status
cc run alpine -- sh    == start temporary/default alpine as today
```

## API/Client Work

Add client helpers that make named instances first-class:

```go
InstanceStatuses() ([]InstanceState, error)       // GET /vm
InstanceStatusOf(id string) (InstanceState, error) // GET /vm/status?id=...
CreateInstanceWithID(id string, req CreateInstanceRequest)
ShutdownInstanceWithID(id string)
AddPortForwardTo(id string, forward PortForward)
RunIn(id string, req RunRequest)
ExecStreamIn(id string, req ExecRequest, ...)
```

Alternatively, keep the existing request `ID` fields and add only the missing
HTTP helpers where the URL query is required.

## Daemon Work

Most daemon support exists. Audit and tighten:

- Ensure every VM action consistently accepts and reports `id`.
- Return useful errors when a named VM already exists or does not exist.
- Ensure `POST /vm/run` and WebSocket `/vm/run` route by request `id`.
- Decide whether `GET /vm/status` with no id should keep returning `default` or
  return an aggregate. Prefer keeping it as `default` and using `GET /vm` for
  lists.
- Confirm shutdown watchdog closes all running VMs cleanly.

## Linux/KVM Audit

The Linux backend appears suitable for multiple simultaneous VMs because most
state is per `ManagedSession`:

- vsock backend/listener is in-process per VM.
- `ControlPort` and `GuestCID` are guest-visible constants inside isolated
  emulated devices, so they should not conflict across VMs.
- synthetic network stack is per VM.
- port forwards bind host ports, so conflicts should be normal host listen
  errors.

Still test these directly:

- Start two Linux/KVM VMs with different ids and different images.
- Run commands in each and verify outputs stay isolated.
- Add a writable share to one VM and ensure the other cannot see it.
- Start two network-enabled VMs and verify each can serve through distinct host
  forwards.
- Attempt duplicate host port forwards and check the error is clear.
- Stop one VM and verify the other continues running.

## Resource Limits

The current Linux `max_instances: 64` is optimistic. Add practical guardrails:

- Keep `max_instances` as a capability, but document it as a daemon limit rather
  than a guarantee of available RAM/CPU.
- Default named VM memory should be conservative or explicit.
- Surface memory/cpu in `cc vm list`.
- Consider refusing new VMs when requested memory exceeds a simple available RAM
  heuristic, or at least warn in CLI output.

## PyNeurodesk Impact

PyNeurodesk can continue using the default VM initially.

Later improvements:

- Store a VM id in shell session state.
- Let `nd activate --vm <name>` bind a shell session to one named VM.
- Let `nd shell load` ensure/start that named VM.
- Let `nd shell exec` accept `--vm <name>` or use the active session VM.
- Keep Neurodesktop on a dedicated default id such as `neurodesktop`.

This avoids breaking the simple Neurodesk workflow while enabling advanced
parallel sessions.

## Tests

Unit tests:

- `internal/vm`: already has named-instance tests; expand around run/stream,
  port forwards, shutdown ordering, and capacity.
- `client`: add named helper tests where URL/query construction matters.
- `cmd/cc`: add parser/dispatch tests for `cc vm ...`.

Integration tests on Linux:

- two persistent VMs booted at once;
- exec into both;
- stop one, exec into the other;
- two port forwards on different host ports;
- duplicate host port conflict;
- capacity limit with `MaxInstances`.

PyNeurodesk tests can wait until VM ids are exposed there.

## Implementation Slices

1. Add `cc vm list/status/start/stop/run/forward`.
2. Add missing Go client helpers for named VM operations.
3. Add unit tests for CLI dispatch and client URL/request behavior.
4. Add Linux integration test for two concurrent named VMs.
5. Update docs.
6. Add PyNeurodesk session VM id support.

## Open Questions

- Should `cc run <image> -- ...` keep its current temporary/default behavior, or
  should it become a pure one-shot command that never touches named VMs?
- Should default VM commands remain forever, or should all persistent operations
  move under `cc vm`?
- Should VM names be free-form strings or restricted to a simple slug pattern?
- Should `cc vm start <name> <image>` implicitly pull/import images by source in
  the future, or stay separate from `cc pull`?
