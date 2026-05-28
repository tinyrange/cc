# vsh

`vsh` is a proof-of-concept shell frontend for `ccvm`. Ordinary lines run in the current context. Lines that begin with `@` are handled by `vsh` itself.

`vsh` must be run from an interactive terminal. Interactive sessions use the native `vsh` line editor, persistent history stored in the `ccvm` cache directory, and autocomplete support for `@` builtins, cached image names, `vsh` options, command names, and host paths.

Guest commands receive a TTY, terminal dimensions, and terminal color environment. `vsh` keeps command execution non-interactive and adds a small color prelude for common commands such as `ls`.

Interactive host and guest commands run through persistent shell sessions when possible, so shell state such as aliases, functions, `cd`, and exported variables can survive across commands. Commands that need full foreground terminal control fall back to a one-shot shell path.

The core syntax is:

```sh
@<oci-tag> [vsh-options] [--] [command...]
```

Examples:

```sh
@ubuntu:24.04
python --version

@host git status

@node:22 npm test

@alpine --vm scratch --memory 2g --cpus 4 sh -lc 'cat /etc/os-release'

@ --vm work --memory-mb 4096
make -j4
```

## Context Rules

Bare targets update the current context:

```sh
@alpine
```

Switching to an image checks the local image state and downloads it if needed.
The VM itself still starts lazily on the first guest command.

Commands after a target are one-shot:

```sh
@alpine uname -a
```

Bare options update the current context:

```sh
@ --vm work --cpus 8 --memory 12g
```

Options followed by a command apply to that command:

```sh
@ --cpus 2 pytest -q
```

The host root is mounted writable into guest commands at `/host`, and the guest workdir defaults to the mirrored host cwd, such as `/host/Users/alice/project`. In host mode, `cd` changes the host directory. In VM mode, `cd /tmp` changes the guest workdir, while `cd /host/...` moves the host directory and returns guest commands to the mirrored host path.

`export NAME=value` is tracked by `vsh` and applied to later host and guest commands.

Background commands can be started with a trailing `&` and inspected with `@jobs`:

```sh
sleep 10 &
@jobs
```

## Builtins

These attention words are reserved:

```sh
@help
@host [command...]
@jobs
@ps
@status
@start [--vm id]
@stop [--vm id]
@forward <host-port:guest-port>
```

`@host` with no command switches the current context to the host. `@host <command>` runs a one-shot host command.

## Options

`vsh` options are parsed before the command:

```sh
--vm <id>
--cwd <guest-path>
--user <user>
--sudo
--memory <n|nM|nG>
--memory-mb <n>
--cpus <n>
--network
--no-network
--nested
--no-nested
```

Use `--` when a command begins with something that looks like a `vsh` option:

```sh
@alpine -- --help
```

Guest commands run as UID `1000` by default. Use `@ --sudo <cmd>` or
`@sudo <cmd>` to run a command as root in the current VM.

If the daemon reports nested virtualization support, `vsh` enables it by
default for VM contexts. Use `@ --no-nested` to disable it for the current
context or a one-shot command.

## Building

For local development, `tools/run_vsh.sh` builds `ccvm` and `vsh` separately
and runs `vsh -ccvm build/vsh/ccvm`.

For a self-contained test build, use:

```sh
go run ./cmd/build-vsh-single
```

That produces `build/vsh/vsh-<goos>-<goarch>` with the `ccvm` daemon and Linux
guest init payloads compiled in. At runtime, `vsh` re-execs itself with a
private daemon flag when it needs to launch the backend.

Cross builds can set:

```sh
CCX3_TARGET_GOOS=windows CCX3_TARGET_GOARCH=amd64 go run ./cmd/build-vsh-single
```

`tools/build_vsh_single.sh` is a Unix convenience wrapper around the Go builder.
`tools/run_vsh.sh` can also exercise this path with `CCX3_VSH_SINGLE=1`.
