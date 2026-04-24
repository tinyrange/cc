# pyneurodesk

`pyneurodesk` is a Python client and shell helper for the `ccx3` daemon. It is
focused on Neurodesk container discovery, CVMFS-backed image import, VM
lifecycle management, and running container commands from Python, notebooks, or
normal shell scripts.

## What It Provides

- A typed HTTP client for `ccvm`
- High-level `container("name")` helpers for Neurodesk containers
- CVMFS listing and file-read helpers
- Automatic daemon startup for local workflows
- Shell activation and command wrappers for `nd load` / `nd exec`
- Fulltest helpers for running Neurodesk test suites inside containers

## Development Setup

The package uses `uv` and requires Python 3.13 or newer.

```sh
cd pyneurodesk
uv sync
uv run pytest
```

The default package install keeps the client and shell dependencies small. The
`pyneurodesk-fulltest` helper needs NIfTI and YAML support, so install the
optional extra when you want to run fulltest recipes:

```sh
uv sync --extra fulltest
```

If you want to exercise real VM execution, build the Go daemon first from the
repository root:

```sh
go build -o /tmp/ccx3-dev/ccvm ./cmd/ccvm
```

Then point `pyneurodesk` at it if it is not bundled in the default location:

```sh
export PYNEURODESK_CCVM=/tmp/ccx3-dev/ccvm
```

## Python Usage

Run a Neurodesk command from Python:

```python
import neurodesk as nd

niimath = nd.container("niimath")
print(niimath.run("niimath", "-help"))
```

Share a host directory with a container:

```python
from pathlib import Path
import neurodesk as nd

work = nd.share_dir(Path.cwd(), writable=True)
niimath = nd.container("niimath")
print(niimath.run("sh", "-lc", f"ls {work.guest_path}"))
```

Connect to an existing daemon:

```python
import neurodesk as nd

client = nd.connect(base_url="http://127.0.0.1:3456")
print(client.instance_status())
```

## Shell Usage

Generate an activation script and load a container:

```sh
source <(uv run neurodesk activate --shell bash)
nd load niimath
niimath -help
```

Run a one-off command through the active VM:

```sh
nd exec niimath -- niimath -help
```

The shell integration creates wrapper scripts in a session directory under the
user cache directory and reuses the shared daemon/VM where possible.

## CVMFS Helpers

Search for available versions:

```python
import neurodesk as nd

print(nd.search("niimath"))
```

Import a specific CVMFS path with the lower-level client:

```python
import neurodesk as nd
from pyneurodesk.models import CVMFSSource, ImportImageRequest

client = nd.connect()
source = CVMFSSource(
    mirror="https://cvmfs.neurodesk.org",
    repo="neurodesk.ardc.edu.au",
    path="/containers/niimath_1.0.20250804_20251016",
)
client.import_image("niimath-cvmfs", ImportImageRequest.from_cvmfs_container(
    mirror=source.mirror,
    repo=source.repo,
    path=source.path,
))
```

## Tests

Unit tests do not require KVM or a live daemon:

```sh
uv run pytest
```

The shell smoke example does require the Go daemon and host virtualization:

```sh
./examples/test_niimath_shell.sh
```

## Environment Variables

- `PYNEURODESK_BASE_URL`: connect to an existing `ccvm` daemon
- `PYNEURODESK_CCVM`: path to the `ccvm` binary used for automatic startup
- `PYNEURODESK_CACHE_DIR`: cache root for daemon state and shell sessions
- `PYNEURODESK_HTTP_TIMEOUT`: default HTTP timeout in seconds
- `PYNEURODESK_BOOT_TIMEOUT`: VM boot timeout in seconds
- `PYNEURODESK_RELEASES_DIR`: local Neurodesk release metadata directory
- `PYNEURODESK_RELEASES_API`: GitHub contents API endpoint for release metadata

## Notes

`pyneurodesk` delegates actual container execution to `ccvm`. On `linux/amd64`,
the daemon currently runs native amd64 containers and rejects known foreign
architectures. Live execution requires the same KVM setup described in the
top-level repository README.
