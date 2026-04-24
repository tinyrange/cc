# TODO

This file tracks follow-up work from the `VISION.md` alignment pass.

## Completed

- Fixed linux/amd64 streaming exec so WebSocket stdin and control messages are delivered to the guest.
- Added unit coverage for streaming stdin, TTY request sizing, terminal resize, signal forwarding, stdout/stderr event ordering, stdin close, and exit status reporting.
- Reworked linux/amd64 direct WebSocket exec to use the same command/env/workdir resolution path as non-stream exec.
- Added named-instance support in the VM manager while preserving the existing default-instance API behavior.
- Added a capabilities endpoint that reports host/backend support, instance concurrency, snapshot classes, network modes, share guarantees, resource-limit knobs, and multi-image exec support.
- Modeled macOS HVF's current single-instance limitation as a capability instead of a universal API assumption.
- Kept the multi-image-per-VM model explicit: one primary image environment plus additional attached image environments.
- Kept `README.md` framed as a microVM runtime, not a generic container runtime.
- Documented Linux KVM as acceptable when `/dev/kvm` is available to regular users, without requiring `ccx3` itself to run as root.
- Moved the small Alpine bringup SIMG to `fixtures/` and updated tests to use that path.

## Remaining MVP Decisions

- Decide whether blank VMs should remain a public API concept or become an internal implementation tool behind named multi-image sessions.
- Add user-facing CLI/client affordances for named instances if the HTTP-level `id` field proves useful in practice.

## Deferred Roadmap

- Use tests to identify the minimum runtime networking needed before adding network objects to the MVP API.
- Design snapshot metadata and compatibility rules before implementing boot or warm snapshots.
- Treat snapshots as a performance optimization until session semantics and restore correctness are well specified.
- Avoid adding large local Neurodesk SIMG files to git; live Neurodesk fixtures should remain opt-in or be fetched/generated.
