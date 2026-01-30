# CrumbleCracker

This repo’s **primary developer entrypoint is `./tools/build.go`**. It builds binaries into `build/` and can also run the main integration test workflows (`quest`, `bringup`, `bringup-gpu`, `tests/<name>`).

Common “just run it” commands:

- **Build `cc` + `ccapp`**: `./tools/build.go`
- **Build + run `cc`**: `./tools/build.go -run -- [cc args...]`
  - Example from `README.md`: `./tools/build.go -run -- alpine`
- **Build + run `ccapp`**: `./tools/build.go -app -- [ccapp args...]`

## Debugging workflow (preferred)

### 1) Run the scenario and capture a binary debug log

The `quest` runner (specifically its `-exec`/InitX path) can emit a high-volume **binary debug log** when you set:

- **`CC_DEBUG_FILE`**: path to write the binary log (e.g. `local/debug.bin`)
- **`CC_DEBUG_MEMORY`**: if set (any value), buffer logs in memory and flush at the end (often reduces IO distortion)

Common flows that hit the `quest -exec` path:

- **Bringup guest tests (filesystem + networking, inside a Linux VM)**:
  - `CC_DEBUG_FILE=local/bringup-debug.bin ./tools/build.go -bringup`
- **GPU bringup guest tests (framebuffer + input, opens a window when supported)**:
  - `CC_DEBUG_FILE=local/bringup-gpu-debug.bin ./tools/build.go -bringup-gpu`

If you want packet captures for bringup networking, set:

- **`CC_NETSTACK_PCAP_DIR`**: directory where host-side pcap files are written
  - Example: `CC_NETSTACK_PCAP_DIR=local/pcap CC_DEBUG_FILE=local/bringup.bin ./tools/build.go -bringup`

To enable the “large” bringup tests (e.g. 1MiB HTTP download), set:

- **`CC_BRINGUP_LARGE=1`**
- **`CC_BRINGUP_LARGE_ITERS=<n>`** (optional)

### 2) Inspect the binary log with the debug tool

Build/run the log inspector via:

- `./tools/build.go -dbg-tool -- [debug flags] <file>`

Examples:

- **List sources**: `./tools/build.go -dbg-tool -- -list local/bringup-debug.bin`
- **Show time range**: `./tools/build.go -dbg-tool -- -range local/bringup-debug.bin`
- **Tail last 200 entries**: `./tools/build.go -dbg-tool -- -tail -limit 200 local/bringup-debug.bin`
- **Filter**: `./tools/build.go -dbg-tool -- -source 'net|virtio' -match '(?i)error|timeout' -limit 0 local/bringup-debug.bin`

**Important:** use `--` so flags like `-list` are passed to the debug tool, not parsed by `tools/build.go`.

## Testing workflow

### Fast unit tests (host Go tests)

Run Go unit tests via `go test`:

- **All packages**: `go test ./...`
- **Single package**: `go test ./internal/netstack`

### Quest (bringup quest + minimal Linux boot check)

`quest` is the **host-side** test runner that validates:

- Hypervisor bringup (x86_64/arm64/riscv64 where supported)
- A small Linux boot smoke test that should print `Hello, World`

Run:

- `./tools/build.go -quest`

To build/run `quest` with the Go race detector (when supported on your platform):

- `./tools/build.go -race -quest`

To pass `quest`’s own flags (e.g. `-linux`, `-arch`, `-exec`, `-gpu`), you must use `--`:

- `./tools/build.go -quest -- -linux`
- `./tools/build.go -quest -- -arch arm64`
- `./tools/build.go -quest -- -exec build/bringup_linux_arm64` (example path; see `build/`)

### Bringup (guest `go test` binary executed inside a VM)

`./tools/build.go -bringup` does two things:

- Builds a **guest** `go test -c` binary from `internal/cmd/bringup` (tagged `guest`)
- Runs it inside a Linux VM using `quest -exec ...`

Run:

- `./tools/build.go -bringup`

### Bringup GPU (guest `go test` binary + virtio-gpu + virtio-input)

Run:

- `./tools/build.go -bringup-gpu`

Notes:

- On platforms where windowing is supported, this opens a window and runs an interactive test loop.

### Dockerfile-based integration tests (`tests/<name>`)

`./tools/build.go -runtest <name>` builds `tests/<name>/Dockerfile`, saves it as `build/test-<name>.tar`, then runs it with `cc`.

Available tests currently include: `hello`, `linux`, `gcc`, `sway`, `userperm`.

Examples:

- `./tools/build.go -runtest hello`
- `./tools/build.go -runtest sway -- -exec -gpu`

Notes:

- Docker is required to build the image tar (but `tools/build.go` will reuse `build/test-<name>.tar` if the context hasn’t changed).
- If you pass `cc` flags (like `-gpu`) you need `--` so `tools/build.go` doesn’t try to parse them.
- `tools/build.go` uses `CC_CACHE_DIR` (or the default `~/Library/Application Support/cc/oci`-style config dir) when managing image extraction cache.
