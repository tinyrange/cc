# Benchmarks

## Minimal VM Boot

This benchmark imports the local `fixtures/alpine.simg` once, then each timed
iteration boots a fresh VM, runs `whoami`, verifies the output is `root`, and
exits.

```sh
go test -v ./internal/vm -run '^$' -bench '^BenchmarkAlpineSIMGWhoamiBoot$' -benchtime=1x
```

On Darwin/arm64 this benchmark is intentionally single-shot because the current
HVF backend cannot reliably create a second VM in the same test process after a
cold boot. Use separate `go test` invocations for repeated samples.
Benchmark setup artifacts are cached under the user cache directory by default;
set `CCX3_BENCH_CACHE_DIR` to use a different cache root.

```sh
go test -v ./internal/vm -run '^$' -bench '^BenchmarkAlpineSIMGWhoamiBootDetailedDarwin$' -benchtime=1x
```

The benchmark reports additional phase metrics:

- `start_ms/op`: fresh VM start through guest ready
- `exec_ms/op`: managed exec of `sh -c whoami`
- `close_ms/op`: VM close request after exec
- `wait_ms/op`: wait for VM teardown to finish after close
- `total_ms/op`: start plus exec plus close plus teardown wait

On Darwin/arm64, the detailed benchmark splits the start phase into backend
request construction and the HVF boot wait. It also reports `trace_*_ms/op`
metrics for individual calls recorded by the backend, HVF setup, and exec path,
for example:

- `trace_backend_guestinit_build_ms/op`
- `trace_backend_prepare_amd64_emulator_ms/op`
- `trace_hvf_prepare_boot_ms/op`
- `trace_hvf_wait_guest_ready_ms/op`
- `trace_exec_stream_events_ms/op`

Use `CCX3_DEBUG_TIMING=1` with either command to print the existing lower-level
timing logs for kernel/module planning, initramfs construction, guest ready, and
managed exec phases. The extra logging is useful for attribution, but it can
perturb the measured result.
