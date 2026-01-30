---
title: Environment Variables
description: Environment variables that affect CrumbleCracker behavior
---

CrumbleCracker respects several environment variables for debugging, logging, and configuration.

## Runtime Variables

### CC_VERBOSE

Enables verbose logging output.

```bash
CC_VERBOSE=1 ./your-program
```

When set to any value, additional debug information is printed during execution.

### CC_DEBUG_FILE

Enables binary debug logging to a file.

```bash
CC_DEBUG_FILE=debug.bin ./your-program
```

The binary log file can be inspected with the debug tool:

```bash
./tools/build.go -dbg-tool -- -list debug.bin
./tools/build.go -dbg-tool -- -tail -limit 100 debug.bin
```

### CC_DEBUG_MEMORY

When used with `CC_DEBUG_FILE`, buffers logs in memory and flushes at the end instead of writing continuously.

```bash
CC_DEBUG_FILE=debug.bin CC_DEBUG_MEMORY=1 ./your-program
```

This reduces I/O overhead during execution but requires enough memory to hold all log entries.

### CC_TIMESLICE_FILE

Enables timeslice recording to a file.

```bash
CC_TIMESLICE_FILE=timeslice.bin ./your-program
```

Used for performance analysis of VM execution.

## Network Variables

### CC_NETSTACK_PCAP_DIR

Directory where host-side packet capture files are written.

```bash
CC_NETSTACK_PCAP_DIR=./pcaps ./your-program
```

Creates `.pcap` files that can be analyzed with Wireshark or tcpdump.

## Testing Variables

### CC_BRINGUP_LARGE

Enables large bringup tests (e.g., 1MiB HTTP downloads).

```bash
CC_BRINGUP_LARGE=1 ./tools/build.go -bringup
```

### CC_BRINGUP_LARGE_ITERS

Sets the number of iterations for large bringup tests.

```bash
CC_BRINGUP_LARGE=1 CC_BRINGUP_LARGE_ITERS=10 ./tools/build.go -bringup
```

## Cache Directories

CrumbleCracker uses platform-specific cache directories by default:

| Platform | Default Cache Location |
|----------|----------------------|
| macOS | `~/Library/Application Support/cc/oci` |
| Linux | `~/.cache/cc/oci` |
| Windows | `%APPDATA%\cc\oci` |

Override by using `NewCacheDir()` programmatically or by setting the cache path in your code.

## Guest Environment Variables

These environment variables are set inside the guest VM:

### TERM

Set automatically to `xterm-256color` if not specified in the container image.

### PATH

Inherited from the container image. If not set, defaults to `/bin:/usr/bin`.

### Container Image Variables

All environment variables defined in the container image (via `ENV` instructions) are passed to the guest.

## Example: Debug a Bringup Test

```bash
# Full debug setup
CC_DEBUG_FILE=local/debug.bin \
CC_NETSTACK_PCAP_DIR=local/pcaps \
CC_VERBOSE=1 \
./tools/build.go -bringup

# Analyze the results
./tools/build.go -dbg-tool -- -list local/debug.bin
./tools/build.go -dbg-tool -- -source 'net' -tail local/debug.bin
wireshark local/pcaps/*.pcap
```

## Example: Verbose Program Execution

```bash
CC_VERBOSE=1 go run ./examples/getting-started/run_python \
    -code "print('hello')"
```
