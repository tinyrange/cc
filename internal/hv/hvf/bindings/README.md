This package provides **CGO-free**, **purego**-backed, low-level bindings for
macOS `Hypervisor.framework` on **arm64**.

The API surface and enum values were derived from the SDK headers at:

`/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/System/Library/Frameworks/Hypervisor.framework/Versions/Current/Headers/`

Notes:
- The bulk of constants/enums are generated into `consts_generated_darwin_arm64.go` via
  `gen_consts_from_sdk.py` and then committed (builds do not require the SDK headers).
- The exported Go functions live in `api_darwin_arm64.go`; the underlying bound symbols
  are registered in `bindings_darwin_arm64.go` via `purego.RegisterLibFunc`.