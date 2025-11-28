# Instructions for Codex CLI

- Your sandbox prevents you from accessing go caches without approval. As a result you should ask for approval for most Go commands. `gofmt` and `go env` are the main exceptions.
- `tools/build.go` is a powerful wrapper to handle most Go related tasks for you.
    - For testing you should use `./tools/build.go -test <pkg>`. This is focused on targeted unit tests.
    - For running the bringup quest (in `cmd/quest/main.go`) you should use `./tools/build.go -quest`. This is the main test suite you should run after every change.