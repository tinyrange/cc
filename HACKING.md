# Hacking on CrumbleCracker Bindings

This guide covers setting up out-of-tree projects that use the Bun/Node.js, Rust, and Python bindings.

## Prerequisites (all languages)

Build the native artifacts from the repo:

```bash
cd $CC_PATH

# Build cc-helper (needed by all bindings)
./tools/build.go cc-helper

# Build the shared library (needed by Python)
./tools/build.go libcc
```

After building, you'll have:
- `build/cc-helper` -- the helper binary (macOS will need it codesigned, which the build does)
- `build/libcc.dylib` -- the shared library

## 1. Bun / Node.js Project

The Node.js binding uses IPC to a `cc-helper` subprocess (no native shared lib needed). It's compatible with Bun.

```bash
mkdir ~/my-cc-bun && cd ~/my-cc-bun
bun init -y
```

Build the bindings, then add the `dist/` directory which contains a clean `package.json` with no devDependencies or optional platform packages:

```bash
# One-time: build the bindings
cd $CC_PATH/bindings/nodejs
bun install && bun run build

# In your project: add the built output
cd ~/my-cc-bun
bun add $CC_PATH/bindings/nodejs/dist
```

Tell it where to find `cc-helper` via env var, then use it:

```ts
// index.ts
import { OCIClient } from '@crumblecracker/cc';

const client = new OCIClient();
const source = await client.pull('alpine:latest');
await using inst = await source.createInstance({});

const output = await inst.command('echo', 'Hello from Bun!').output();
console.log(output.toString());

await client.close();
```

Run it:

```bash
CC_HELPER_PATH=$CC_PATH/build/cc-helper bun run index.ts
```

## 2. Rust Project

The Rust binding's `build.rs` automatically builds `libcc.a` and `cc-helper` from the Go source, so it just needs to be able to find the repo root. It does this via a relative path from `CARGO_MANIFEST_DIR` (`../../`), so you need to use a `path` dependency.

```bash
mkdir ~/my-cc-rust && cd ~/my-cc-rust
cargo init
```

Edit `Cargo.toml`:

```toml
[package]
name = "my-cc-rust"
version = "0.1.0"
edition = "2021"

[dependencies]
cc-vm = { path = "$CC_PATH/bindings/rust" }
```

**Important caveat**: the `build.rs` resolves the project root as `CARGO_MANIFEST_DIR/../../` (i.e., it expects to be inside `bindings/rust/` of the repo). Since you're referencing it as a `path` dependency, `CARGO_MANIFEST_DIR` for the `cc-vm` crate will still point to `bindings/rust/`, so the relative path resolution will work correctly.

Write `src/main.rs`:

```rust
use cc::{Instance, InstanceOptions, OciClient};

fn main() -> cc::Result<()> {
    cc::init()?;

    let caps = cc::query_capabilities()?;
    println!("Hypervisor available: {}", caps.hypervisor_available);

    let client = OciClient::new()?;
    let source = client.pull("alpine:latest", None, None)?;

    let opts = InstanceOptions {
        memory_mb: 256,
        cpus: 1,
        ..Default::default()
    };
    let inst = Instance::new(source, Some(opts))?;

    let output = inst.command("echo", &["Hello from Rust!"])?.output()?;
    println!("{}", String::from_utf8_lossy(&output.stdout));

    cc::shutdown();
    Ok(())
}
```

Build and run:

```bash
cargo run
```

No environment variables needed -- `build.rs` handles everything (building `libcc.a`, `cc-helper`, codesigning, and copying the helper next to the output binary).

## 3. Python Project

The Python binding uses `ctypes` to load `libcc.dylib` directly, so it needs both the shared library and `cc-helper` available.

```bash
mkdir ~/my-cc-python && cd ~/my-cc-python
python3 -m venv .venv
source .venv/bin/activate

# Install the bindings as an editable package
pip install -e $CC_PATH/bindings/python
```

Write `main.py`:

```python
import crumblecracker as cc

cc.init()

caps = cc.query_capabilities()
print(f"Hypervisor available: {caps.hypervisor_available}")

with cc.OCIClient() as client:
    source = client.pull("alpine:latest")

    with cc.Instance(source) as inst:
        output = inst.command("echo", "Hello from Python!").output()
        print(output.decode())

cc.shutdown()
```

Run it, pointing to the built shared library:

```bash
LIBCC_PATH=$CC_PATH/build/libcc.dylib \
CC_HELPER_PATH=$CC_PATH/build/cc-helper \
  python main.py
```

## Environment Variables Summary

| Language | `LIBCC_PATH` | `CC_HELPER_PATH` |
|----------|-------------|------------------|
| Bun/Node | Not needed (uses IPC) | Yes (or put `cc-helper` on `$PATH`) |
| Rust | Not needed (static link via `build.rs`) | Not needed (`build.rs` builds + copies it) |
| Python | Yes (path to `libcc.dylib`) | Yes (path to `cc-helper`) |

Alternatively, instead of using env vars every time, you can copy the built binaries onto your `$PATH`:

```bash
# One-time: make cc-helper available everywhere
cp $CC_PATH/build/cc-helper /usr/local/bin/
```
