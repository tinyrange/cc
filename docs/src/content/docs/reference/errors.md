---
title: Errors
description: Error types and sentinel errors
---

CrumbleCracker uses structured errors and sentinel values for error handling.

## Sentinel Errors

Use `errors.Is()` to check for these conditions:

### ErrNotRunning

The instance is not in a running state.

```go
output, err := instance.Command("ls").Output()
if errors.Is(err, cc.ErrNotRunning) {
    log.Println("VM is not running")
}
```

**Common causes:**
- Instance was closed
- VM crashed during execution
- Command run after instance shutdown

### ErrAlreadyClosed

The instance has already been closed.

```go
err := instance.Close()
if errors.Is(err, cc.ErrAlreadyClosed) {
    // Already closed, nothing to do
}
```

**Common causes:**
- Calling `Close()` multiple times
- Operations after explicit close

### ErrTimeout

An operation timed out.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

output, err := instance.CommandContext(ctx, "sleep", "100").Output()
if errors.Is(err, cc.ErrTimeout) {
    log.Println("Command timed out")
}
```

**Common causes:**
- Context deadline exceeded
- Instance timeout (from `WithTimeout`) reached
- Network operation timeout

### ErrHypervisorUnavailable

The hypervisor is not available on this system.

```go
if err := cc.SupportsHypervisor(); errors.Is(err, cc.ErrHypervisorUnavailable) {
    log.Println("Cannot run VMs on this system")
}
```

**Common causes:**
- Missing platform support (no KVM, HVF, or WHP)
- Permission issues (`/dev/kvm` not accessible)
- Running in a VM without nested virtualization
- Missing code signing entitlement (macOS)

## Error Type

### cc.Error

Structured error type for operation failures:

```go
type Error struct {
    Op   string  // Operation that failed
    Path string  // Path involved (if applicable)
    Err  error   // Underlying error
}
```

**Example:**
```go
_, err := instance.ReadFile("/nonexistent")
if ccErr, ok := err.(*cc.Error); ok {
    fmt.Printf("Operation: %s\n", ccErr.Op)
    fmt.Printf("Path: %s\n", ccErr.Path)
    fmt.Printf("Cause: %v\n", ccErr.Err)
}
```

**Output:**
```
Operation: read
Path: /nonexistent
Cause: no such file or directory
```

### Error Methods

```go
func (e *Error) Error() string   // Formatted error message
func (e *Error) Unwrap() error   // Returns underlying Err
```

The `Error()` method returns formatted strings like:
- `"read /path/to/file: no such file or directory"`
- `"create instance: hypervisor unavailable"`

## Checking Errors

### Using errors.Is

Check for sentinel errors:

```go
if errors.Is(err, cc.ErrHypervisorUnavailable) {
    // Handle missing hypervisor
}
```

### Using errors.As

Extract structured error information:

```go
var ccErr *cc.Error
if errors.As(err, &ccErr) {
    log.Printf("Operation %s on %s failed: %v", ccErr.Op, ccErr.Path, ccErr.Err)
}
```

### Checking OS Errors

Many filesystem errors wrap standard `os` errors:

```go
if errors.Is(err, os.ErrNotExist) {
    log.Println("File does not exist")
}

if errors.Is(err, os.ErrPermission) {
    log.Println("Permission denied")
}
```

## Common Error Patterns

### Hypervisor Check at Startup

```go
func main() {
    if err := cc.SupportsHypervisor(); err != nil {
        if errors.Is(err, cc.ErrHypervisorUnavailable) {
            fmt.Println("VMs are not supported on this system.")
            fmt.Println("Possible solutions:")
            fmt.Println("  - Enable virtualization in BIOS")
            fmt.Println("  - Add user to 'kvm' group (Linux)")
            fmt.Println("  - Enable Hypervisor.framework (macOS)")
        }
        os.Exit(1)
    }
    // Continue with VM operations
}
```

### Graceful Timeout Handling

```go
func runWithTimeout(instance cc.Instance, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    err := instance.CommandContext(ctx, "long-process").Run()
    if errors.Is(err, cc.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
        // Clean shutdown on timeout
        return fmt.Errorf("process timed out after %v", timeout)
    }
    return err
}
```

### File Operation Errors

```go
func copyToGuest(instance cc.Instance, src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return fmt.Errorf("read source: %w", err)
    }

    err = instance.WriteFile(dst, data, 0644)
    if err != nil {
        var ccErr *cc.Error
        if errors.As(err, &ccErr) {
            return fmt.Errorf("write to guest %s: %w", ccErr.Path, ccErr.Err)
        }
        return fmt.Errorf("write to guest: %w", err)
    }

    return nil
}
```

## Debugging Errors

### Enable Verbose Mode

```bash
CC_VERBOSE=1 ./your-program
```

### Check Instance State

```go
select {
case err := <-instance.Done():
    log.Printf("VM exited: %v", err)
default:
    log.Println("VM still running")
}
```

### Inspect Underlying Errors

```go
func debugError(err error) {
    fmt.Printf("Error: %v\n", err)

    // Check all wrapped errors
    for e := err; e != nil; e = errors.Unwrap(e) {
        fmt.Printf("  Wrapped: %T: %v\n", e, e)
    }
}
```
