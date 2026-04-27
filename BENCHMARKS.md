# Benchmarks

## Minimal VM Boot

This benchmark imports the local `fixtures/alpine.simg` once, then each timed
iteration boots a fresh VM, runs `whoami`, verifies the output is `root`, and
exits.

```sh
go test ./internal/vm -run '^$' -bench '^BenchmarkAlpineSIMGWhoamiBoot$' -count 5
```

Use `-benchtime=10x` to run a fixed number of boots:

```sh
go test ./internal/vm -run '^$' -bench '^BenchmarkAlpineSIMGWhoamiBoot$' -benchtime=10x
```
