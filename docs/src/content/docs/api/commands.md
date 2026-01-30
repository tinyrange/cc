---
title: Commands
description: Execute programs in the guest VM
---

The `Exec` interface provides command execution that mirrors `os/exec`. Run programs, capture output, and manage processes in the guest VM.

## Overview

Every `Instance` implements `Exec`, so you can run commands directly:

```go
output, err := instance.Command("echo", "hello").Output()
```

## Creating Commands

### Command

Create a command to run:

```go
cmd := instance.Command("ls", "-la", "/etc")
```

### CommandContext

Create a command with a context for cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

cmd := instance.CommandContext(ctx, "long-running-process")
```

### EntrypointCommand

Run the container's configured entrypoint:

```go
// Use the image's CMD as-is
cmd := instance.EntrypointCommand()

// Override the CMD but keep ENTRYPOINT
cmd := instance.EntrypointCommand("--verbose", "--config", "/app/config.yaml")
```

This is useful when you want to respect the container's intended startup behavior.

## Running Commands

### Output

Run and capture stdout:

```go
output, err := instance.Command("cat", "/etc/os-release").Output()
if err != nil {
    return err
}
fmt.Println(string(output))
```

### CombinedOutput

Capture both stdout and stderr:

```go
output, err := instance.Command("sh", "-c", "echo stdout; echo stderr >&2").CombinedOutput()
if err != nil {
    // output still contains whatever was written before the error
    fmt.Println("Output:", string(output))
    return err
}
```

### Run

Run without capturing output:

```go
err := instance.Command("touch", "/tmp/marker").Run()
```

### Start and Wait

Start a command asynchronously:

```go
cmd := instance.Command("sleep", "10")
if err := cmd.Start(); err != nil {
    return err
}

// Do other work...

if err := cmd.Wait(); err != nil {
    return err
}
```

## I/O Handling

### Setting Stdin

Provide input to a command:

```go
input := strings.NewReader("hello world")
cmd := instance.Command("cat")
cmd.SetStdin(input)
output, err := cmd.Output()
// output is "hello world"
```

### Setting Stdout/Stderr

Redirect output to writers:

```go
var stdout, stderr bytes.Buffer
cmd := instance.Command("sh", "-c", "echo out; echo err >&2")
cmd.SetStdout(&stdout)
cmd.SetStderr(&stderr)
err := cmd.Run()

fmt.Println("stdout:", stdout.String())
fmt.Println("stderr:", stderr.String())
```

### Pipes

Get pipes for streaming I/O:

```go
cmd := instance.Command("cat")

stdin, err := cmd.StdinPipe()
if err != nil {
    return err
}

stdout, err := cmd.StdoutPipe()
if err != nil {
    return err
}

if err := cmd.Start(); err != nil {
    return err
}

// Write to stdin
go func() {
    stdin.Write([]byte("hello"))
    stdin.Close()
}()

// Read from stdout
output, _ := io.ReadAll(stdout)
fmt.Println(string(output))

cmd.Wait()
```

## Environment Variables

### SetEnv

Set a single environment variable:

```go
cmd := instance.Command("sh", "-c", "echo $MY_VAR")
cmd.SetEnv("MY_VAR", "hello")
output, _ := cmd.Output()
// output is "hello\n"
```

### GetEnv

Get an environment variable:

```go
cmd := instance.Command("printenv")
path := cmd.GetEnv("PATH")
fmt.Println("PATH:", path)
```

### Environ

Get all environment variables:

```go
cmd := instance.Command("env")
for _, env := range cmd.Environ() {
    fmt.Println(env)
}
```

## Working Directory

Set the working directory for the command:

```go
cmd := instance.Command("ls")
cmd.SetDir("/app")
output, _ := cmd.Output()
```

## Exit Codes

Get the exit code after a command completes:

```go
cmd := instance.Command("sh", "-c", "exit 42")
err := cmd.Run()
if err != nil {
    fmt.Printf("Exit code: %d\n", cmd.ExitCode())
}
```

## Shell Commands

Run shell commands with `sh -c`:

```go
// Pipes and redirects
output, _ := instance.Command("sh", "-c", "cat /etc/passwd | grep root").Output()

// Multiple commands
output, _ := instance.Command("sh", "-c", "cd /app && make build").Output()

// Environment variable expansion
instance.Command("sh", "-c", "echo $HOME").Output()
```

## Exec (Replace Init)

Replace the VM's init process with a new command. This is a terminal operation - when the command exits, the VM terminates:

```go
// This replaces init and doesn't return
err := instance.Exec("/usr/bin/python3", "/app/server.py")
```

Use `ExecContext` for cancellation support:

```go
err := instance.ExecContext(ctx, "/app/server")
```

## Example: Build and Test

```go
func buildAndTest(instance cc.Instance) error {
    // Write source code
    code := `
package main

import "fmt"

func main() {
    fmt.Println("Hello!")
}
`
    if err := instance.MkdirAll("/app", 0755); err != nil {
        return err
    }
    if err := instance.WriteFile("/app/main.go", []byte(code), 0644); err != nil {
        return err
    }

    // Initialize Go module
    cmd := instance.Command("go", "mod", "init", "app")
    cmd.SetDir("/app")
    if err := cmd.Run(); err != nil {
        return err
    }

    // Build
    cmd = instance.Command("go", "build", "-o", "app")
    cmd.SetDir("/app")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("build failed: %s", output)
    }

    // Run
    output, err = instance.Command("/app/app").Output()
    if err != nil {
        return err
    }
    fmt.Println("Output:", string(output))

    return nil
}
```

## Example: Interactive Python REPL

```go
func runPythonREPL(instance cc.Instance) error {
    cmd := instance.Command("python3")

    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    stderr, _ := cmd.StderrPipe()

    if err := cmd.Start(); err != nil {
        return err
    }

    // Read and write interactively
    go io.Copy(os.Stderr, stderr)
    go io.Copy(os.Stdout, stdout)
    go io.Copy(stdin, os.Stdin)

    return cmd.Wait()
}
```

## Next Steps

- [Networking](/cc/api/networking/) - Connect to network services
- [Environment Variables Reference](/cc/reference/options/) - All command options
