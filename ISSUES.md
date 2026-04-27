# Performance and Usability Issues

Ranked review findings from the April 2026 performance and usability pass.

## 1. Cold Python container imports are silent and timeout-prone

Status: addressed by streaming cold imports through `import_image_stream()` when
available, with a compatibility fallback to blocking `import_image()`.

`pyneurodesk.container()` calls blocking `import_image()` even though streaming
import support exists.

- Evidence: `pyneurodesk/src/pyneurodesk/api.py:435`
- Related streaming API: `pyneurodesk/src/pyneurodesk/client.py:57`
- Impact: large CVMFS images can leave users with stale progress and may hit
  host API read timeouts.
- Recommended fix: use `import_image_stream()` from `container()` and feed its
  progress into the existing reporter.

## 2. VM boot has a hard 30 second ceiling

The daemon wraps VM boot in a fixed timeout, and the Python client mirrors the
same default.

- Evidence: `cmd/ccvm/main.go:535`
- Python default: `pyneurodesk/src/pyneurodesk/client.py:449`
- Impact: larger images and slower hosts fail with `vm boot timed out after 30s`;
  `FULLTEST.md` already records real failures.
- Recommended fix: make boot timeout configurable per request and prefer streamed
  boot events by default.

## 3. CLI long operations do not use available progress streams

`cc kernel-download`, `cc pull`, and `cc vm-start` call blocking endpoints even
though the daemon supports NDJSON progress and boot streams.

- Evidence: `cmd/cc/main.go:70`
- Impact: the slowest operations give users the least feedback.
- Recommended fix: add CLI stream handling for download, pull, and boot commands.

## 4. Go network clients use `http.DefaultClient` with no timeout

OCI pulls and CVMFS reads are built on clients without bounded request timeouts.

- Evidence: `internal/oci/oci.go:190`
- Evidence: `internal/cvmfs/cvmfs.go:189`
- Impact: network stalls can hang pulls, imports, and CVMFS metadata operations
  indefinitely.
- Recommended fix: use configured clients with timeouts, retries/backoff, and
  clearer transient error reporting.

## 5. CVMFS range reads can download whole files

Local range reads load the full file, and uncached remote range reads fall back
to `ReadFile()`.

- Evidence: `internal/cvmfs/cvmfs.go:397`
- Remote fallback: `internal/cvmfs/cvmfs.go:453`
- Impact: small metadata probes can become large downloads and memory allocations.
- Recommended fix: implement true ranged/chunked reads for local and remote CVMFS
  objects.

## 6. CVMFS prefetch can overload disk or network

Prefetch worker count defaults to 4 but is otherwise uncapped, and all workers
write through one packed contents file via offset writers.

- Evidence: `internal/oci/cvmfs_import.go:138`
- Worker writes: `internal/oci/cvmfs_import.go:223`
- Impact: aggressive settings can saturate disk or network and make imports less
  predictable.
- Recommended fix: cap worker counts, validate user input, and consider
  per-host concurrency or rate limiting.

## 7. Streaming exec has worse behavior than non-streaming exec

Non-streaming `Run` can start one-shot workloads when no VM exists, while
streaming requires an already running VM and rejects other images.

- Evidence: `internal/vm/vm.go:278`
- Streaming restriction: `internal/vm/vm.go:303`
- Impact: the better UX path is less capable, which pushes users back to
  blocking APIs.
- Recommended fix: make streaming run support the same one-shot and image
  behavior as non-streaming run.

## 8. Python shell streaming drops output fidelity

The shell wrapper only reads `output`, writes all stream events to stdout, and
ignores binary `data` payloads.

- Evidence: `pyneurodesk/src/pyneurodesk/shell.py:328`
- Impact: stderr semantics are lost and binary or chunked output may be dropped.
- Recommended fix: handle stdout/stderr separately and preserve `data` payloads.

## 9. Python search does an N+1 GitHub metadata crawl

Remote release search lists files, then fetches each JSON metadata file
separately with no cache.

- Evidence: `pyneurodesk/src/pyneurodesk/api.py:1363`
- Per-file fetch: `pyneurodesk/src/pyneurodesk/api.py:1395`
- Impact: search is slow and sensitive to GitHub rate limits.
- Recommended fix: cache remote results or ship a compact release manifest.

## 10. Daemon startup failures are too raw for users

`ccvm` still panics on setup failures, while the CLI only waits for a startup
JSON banner.

- Evidence: `cmd/ccvm/main.go:111`
- CLI startup read: `cmd/cc/main.go:340`
- Impact: startup failures can produce confusing output instead of actionable
  remediation.
- Recommended fix: replace startup panic paths with structured errors and surface
  the daemon log path or next step.

## Verification

Focused sanity check passed:

```sh
go test ./cmd/cc ./cmd/ccvm ./client ./internal/vm ./internal/cvmfs ./internal/oci
```
