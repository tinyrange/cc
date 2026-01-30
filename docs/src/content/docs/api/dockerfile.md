---
title: Dockerfile
description: Build images from Dockerfiles
---

CrumbleCracker can build images directly from Dockerfile content, without requiring Docker to be installed. This is useful for creating custom images programmatically.

## Overview

```go
dockerfile := []byte(`
FROM alpine:3.19
RUN apk add --no-cache curl
COPY app /usr/local/bin/
CMD ["app"]
`)

source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildContextDir("./build"),
)
if err != nil {
    return err
}

instance, err := cc.New(source)
```

## Building from Dockerfile

### Basic Build

```go
client, err := cc.NewOCIClient()
if err != nil {
    return err
}

dockerfile := []byte(`
FROM python:3.12-slim
RUN pip install flask
WORKDIR /app
CMD ["python", "-m", "flask", "run"]
`)

source, err := cc.BuildDockerfileSource(ctx, dockerfile, client)
if err != nil {
    return err
}
defer source.Close()

instance, err := cc.New(source)
```

### With Build Context

Provide files for COPY/ADD instructions:

```go
source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildContextDir("/path/to/context"),
)
```

The build context directory should contain the files referenced in COPY/ADD instructions.

### With Build Arguments

Pass build-time variables:

```go
dockerfile := []byte(`
FROM alpine:3.19
ARG VERSION=1.0.0
RUN echo "Building version $VERSION"
`)

source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildArg("VERSION", "2.0.0"),
)
```

### With Cache Directory

Cache intermediate layers for faster rebuilds:

```go
source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithDockerfileCacheDir("/path/to/cache"),
)
```

## Supported Instructions

The Dockerfile parser supports these instructions:

| Instruction | Description |
|-------------|-------------|
| `FROM` | Set base image |
| `RUN` | Execute command during build |
| `COPY` | Copy files from build context |
| `ADD` | Add files (with URL and tar extraction support) |
| `ENV` | Set environment variables |
| `ARG` | Define build-time variables |
| `WORKDIR` | Set working directory |
| `USER` | Set user for subsequent commands |
| `CMD` | Set default command |
| `ENTRYPOINT` | Set container entrypoint |
| `EXPOSE` | Document exposed ports |
| `LABEL` | Add metadata labels |

## Getting Runtime Configuration

Extract CMD, ENTRYPOINT, and other runtime settings without building:

```go
dockerfile := []byte(`
FROM alpine:3.19
WORKDIR /app
ENV APP_ENV=production
ENTRYPOINT ["/app/server"]
CMD ["--port", "8080"]
`)

config, err := cc.BuildDockerfileRuntimeConfig(dockerfile)
if err != nil {
    return err
}

fmt.Println("WorkDir:", config.WorkDir)
fmt.Println("Entrypoint:", config.Entrypoint)
fmt.Println("Cmd:", config.Cmd)
fmt.Println("Env:", config.Env)
```

This is useful for understanding what a Dockerfile will produce without executing the build.

## Custom Build Context

Create a build context programmatically:

```go
buildContext, err := cc.NewDirBuildContext("/path/to/files")
if err != nil {
    return err
}

source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildContext(buildContext),
)
```

## Example: Build and Run Web App

```go
func buildAndRunApp(appDir string) error {
    client, _ := cc.NewOCIClient()

    dockerfile := []byte(`
FROM node:20-slim
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
`)

    source, err := cc.BuildDockerfileSource(
        context.Background(),
        dockerfile,
        client,
        cc.WithBuildContextDir(appDir),
        cc.WithDockerfileCacheDir(filepath.Join(os.TempDir(), "cc-dockerfile-cache")),
    )
    if err != nil {
        return fmt.Errorf("build failed: %w", err)
    }
    defer source.Close()

    instance, err := cc.New(source, cc.WithMemoryMB(512))
    if err != nil {
        return err
    }
    defer instance.Close()

    // Start the server
    cmd := instance.EntrypointCommand()
    return cmd.Run()
}
```

## Example: Dynamic Dockerfile Generation

Generate Dockerfiles based on configuration:

```go
func createDockerfile(baseImage string, packages []string) []byte {
    var b strings.Builder

    fmt.Fprintf(&b, "FROM %s\n", baseImage)

    if len(packages) > 0 {
        pkgList := strings.Join(packages, " ")
        fmt.Fprintf(&b, "RUN apt-get update && apt-get install -y %s\n", pkgList)
    }

    b.WriteString("WORKDIR /app\n")
    b.WriteString("CMD [\"bash\"]\n")

    return []byte(b.String())
}

func main() {
    dockerfile := createDockerfile("debian:bookworm-slim", []string{"curl", "vim", "git"})

    client, _ := cc.NewOCIClient()
    source, _ := cc.BuildDockerfileSource(context.Background(), dockerfile, client)
    defer source.Close()

    instance, _ := cc.New(source)
    defer instance.Close()

    output, _ := instance.Command("git", "--version").Output()
    fmt.Println(string(output))
}
```

## Multi-stage Builds

Multi-stage builds are supported:

```go
dockerfile := []byte(`
FROM golang:1.22 AS builder
WORKDIR /src
COPY . .
RUN go build -o /app

FROM alpine:3.19
COPY --from=builder /app /usr/local/bin/app
CMD ["app"]
`)

source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildContextDir("./"),
)
```

Only the final stage becomes the VM filesystem.

## Caching Behavior

Dockerfile builds cache at the layer level:

1. Each instruction creates a layer
2. Layers are cached based on instruction content
3. Changing an instruction invalidates it and all subsequent layers
4. COPY/ADD invalidates based on file content hash

Use `WithDockerfileCacheDir` to persist the cache across runs.

## Limitations

- `HEALTHCHECK` is parsed but not enforced
- `STOPSIGNAL` is parsed but not used
- `SHELL` instruction is not supported (always uses `/bin/sh -c`)
- Build secrets are not supported

## Next Steps

- [Snapshots](/api/snapshots/) - Cache built images as snapshots
- [Creating Instances](/api/creating-instances/) - Start VMs from Dockerfile builds
