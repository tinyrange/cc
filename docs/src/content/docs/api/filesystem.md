---
title: Filesystem
description: Working with the guest filesystem
---

The `FS` interface provides filesystem operations on the guest VM. It mirrors functions from the `os` package, making it familiar to Go developers.

## Overview

Every `Instance` implements `FS`, so you can call filesystem methods directly on it:

```go
// Write a file
err := instance.WriteFile("/tmp/hello.txt", []byte("Hello!"), 0644)

// Read it back
content, err := instance.ReadFile("/tmp/hello.txt")
```

## Reading Files

### ReadFile

Read an entire file into memory:

```go
content, err := instance.ReadFile("/etc/os-release")
if err != nil {
    return err
}
fmt.Println(string(content))
```

### Open

Open a file for reading (returns a `File` handle):

```go
file, err := instance.Open("/etc/passwd")
if err != nil {
    return err
}
defer file.Close()

// Read line by line
scanner := bufio.NewScanner(file)
for scanner.Scan() {
    fmt.Println(scanner.Text())
}
```

### Stat

Get file information:

```go
info, err := instance.Stat("/tmp/file.txt")
if err != nil {
    if os.IsNotExist(err) {
        fmt.Println("File doesn't exist")
    }
    return err
}

fmt.Printf("Size: %d bytes\n", info.Size())
fmt.Printf("Mode: %s\n", info.Mode())
fmt.Printf("Modified: %s\n", info.ModTime())
```

### Lstat

Like `Stat`, but doesn't follow symlinks:

```go
info, err := instance.Lstat("/usr/bin/python")
if info.Mode()&os.ModeSymlink != 0 {
    target, _ := instance.Readlink("/usr/bin/python")
    fmt.Printf("Symlink to: %s\n", target)
}
```

## Writing Files

### WriteFile

Write data to a file, creating it if necessary:

```go
data := []byte("Hello, World!\n")
err := instance.WriteFile("/app/output.txt", data, 0644)
```

### Create

Create or truncate a file for writing:

```go
file, err := instance.Create("/app/log.txt")
if err != nil {
    return err
}
defer file.Close()

file.Write([]byte("Log entry 1\n"))
file.Write([]byte("Log entry 2\n"))
```

### OpenFile

Open with specific flags and permissions (mirrors `os.OpenFile`):

```go
file, err := instance.OpenFile("/app/data.bin",
    os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
if err != nil {
    return err
}
defer file.Close()

file.Write([]byte("appended data"))
```

## Directories

### Mkdir

Create a single directory:

```go
err := instance.Mkdir("/app/data", 0755)
```

### MkdirAll

Create a directory and all parent directories:

```go
err := instance.MkdirAll("/app/data/cache/temp", 0755)
```

### ReadDir

List directory contents:

```go
entries, err := instance.ReadDir("/etc")
if err != nil {
    return err
}

for _, entry := range entries {
    if entry.IsDir() {
        fmt.Printf("[DIR]  %s\n", entry.Name())
    } else {
        fmt.Printf("[FILE] %s\n", entry.Name())
    }
}
```

## Removing Files

### Remove

Remove a single file or empty directory:

```go
err := instance.Remove("/tmp/file.txt")
```

### RemoveAll

Remove a file or directory tree recursively:

```go
err := instance.RemoveAll("/app/cache")
```

## Symlinks

### Symlink

Create a symbolic link:

```go
err := instance.Symlink("/usr/bin/python3", "/usr/local/bin/python")
```

### Readlink

Read the target of a symbolic link:

```go
target, err := instance.Readlink("/usr/bin/python")
fmt.Printf("python -> %s\n", target)
```

## File Permissions

### Chmod

Change file permissions:

```go
err := instance.Chmod("/app/script.sh", 0755)
```

### Chown

Change file ownership:

```go
err := instance.Chown("/app/data", 1000, 1000) // uid, gid
```

### Chtimes

Change file timestamps:

```go
now := time.Now()
err := instance.Chtimes("/app/file.txt", now, now) // atime, mtime
```

## Rename

Move or rename a file:

```go
err := instance.Rename("/tmp/old.txt", "/tmp/new.txt")
```

## Context Support

Add a context for cancellation or timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

fs := instance.WithContext(ctx)
content, err := fs.ReadFile("/large/file.bin")
```

## File Interface

The `File` type returned by `Open`, `Create`, and `OpenFile` implements:

```go
type File interface {
    io.Reader
    io.Writer
    io.Closer
    io.Seeker
    io.ReaderAt
    io.WriterAt

    Stat() (fs.FileInfo, error)
    Sync() error
    Truncate(size int64) error
    Name() string
}
```

This mirrors `*os.File`, so code that works with files on the host can often work with guest files unchanged.

## Example: Deploy a Web Application

```go
func deploy(instance cc.Instance, appDir string) error {
    // Create application directory
    if err := instance.MkdirAll("/app", 0755); err != nil {
        return err
    }

    // Copy files from host to guest
    entries, err := os.ReadDir(appDir)
    if err != nil {
        return err
    }

    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }

        hostPath := filepath.Join(appDir, entry.Name())
        guestPath := "/app/" + entry.Name()

        data, err := os.ReadFile(hostPath)
        if err != nil {
            return err
        }

        if err := instance.WriteFile(guestPath, data, 0644); err != nil {
            return err
        }
    }

    return nil
}
```

## Next Steps

- [Command Execution](/cc/api/commands/) - Run programs in the VM
- [Filesystem Snapshots](/cc/api/snapshots/) - Save and restore filesystem state
