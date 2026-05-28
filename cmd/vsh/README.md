# vsh

`vsh` is a proof-of-concept shell frontend for `ccvm`. Ordinary lines run in the current context. Lines that begin with `@` are handled by `vsh` itself.

`vsh` must be run from an interactive terminal. Interactive sessions use readline-style editing with persistent history stored in the `ccvm` cache directory.

Guest commands receive a TTY, terminal dimensions, and terminal color environment. `vsh` keeps command execution non-interactive and adds a small color prelude for common commands such as `ls`.

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

The host root is mounted writable into guest commands at `/host`, and the guest workdir defaults to the mirrored host cwd, such as `/host/Users/alice/project`. `cd` changes the host directory; the next guest command runs from the new mirrored `/host/...` path.

## Builtins

These attention words are reserved:

```sh
@help
@host [command...]
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
--memory <n|nM|nG>
--memory-mb <n>
--cpus <n>
--network
--no-network
```

Use `--` when a command begins with something that looks like a `vsh` option:

```sh
@alpine -- --help
```
