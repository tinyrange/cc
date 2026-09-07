# vmsh

The experimental interactive shell for host, VM, and SSH contexts, now maintained
inside cc. See the [repository README](../../README.md#interactive-vmsh) for build
instructions. No CrumbleCracker checkout is required.

```text
@alpine
uname -a
pwd
@host
```

Run short checks from this directory with `go test -short ./...` after building
the guest init payloads from the repository root. Real VM integration tests remain
available for targeted runs.
