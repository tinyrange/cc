---
title: Networking
description: Network operations in guest VMs
---

The `Net` interface provides network operations that mirror the Go `net` package. Connect from the guest to external services, or expose guest services to the host.

## Overview

Every `Instance` implements `Net`, providing familiar networking operations:

```go
// Dial from guest to external service
conn, err := instance.Dial("tcp", "example.com:80")

// Listen inside the guest
listener, err := instance.Listen("tcp", ":8080")
```

## Network Architecture

Each VM gets a virtual network stack with:

- **Default route**: Internet access via the host
- **DNS resolution**: Automatic DNS forwarding
- **Guest IP**: Typically `10.0.2.15`
- **Host-accessible gateway**: The host can connect to guest services

## Dialing Out

### Dial

Connect from the guest to a remote service:

```go
conn, err := instance.Dial("tcp", "api.example.com:443")
if err != nil {
    return err
}
defer conn.Close()

// Use the connection
conn.Write([]byte("GET / HTTP/1.1\r\nHost: api.example.com\r\n\r\n"))
response, _ := io.ReadAll(conn)
```

### DialContext

Dial with a context for cancellation or timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

conn, err := instance.DialContext(ctx, "tcp", "slow-server.com:80")
```

### Supported Networks

- `tcp`, `tcp4`, `tcp6` - TCP connections
- `udp`, `udp4`, `udp6` - UDP connections

## HTTP Requests

Combine `Dial` with Go's HTTP client:

```go
// Create a transport that uses the guest's network
transport := &http.Transport{
    DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
        return instance.DialContext(ctx, network, addr)
    },
}

client := &http.Client{Transport: transport}
resp, err := client.Get("https://api.example.com/data")
```

## Listening

### Listen

Create a TCP listener inside the guest:

```go
listener, err := instance.Listen("tcp", ":8080")
if err != nil {
    return err
}
defer listener.Close()

for {
    conn, err := listener.Accept()
    if err != nil {
        return err
    }
    go handleConnection(conn)
}
```

### ListenPacket

Create a UDP listener:

```go
packetConn, err := instance.ListenPacket("udp", ":5353")
if err != nil {
    return err
}
defer packetConn.Close()

buf := make([]byte, 1024)
n, addr, err := packetConn.ReadFrom(buf)
```

## Port Forwarding

### Expose (Guest to Host)

Expose a guest listener on the host:

```go
// Guest listens on :8080
guestListener, _ := instance.Listen("tcp", ":8080")

// Create a host listener
hostListener, _ := net.Listen("tcp", "127.0.0.1:8080")

// Forward host connections to guest
closer, err := instance.Expose("tcp", ":8080", hostListener)
if err != nil {
    return err
}
defer closer.Close()

// Now connections to localhost:8080 reach the guest
```

### Forward (Host to Guest)

Forward a guest listener to a host address:

```go
guestListener, _ := instance.Listen("tcp", ":8080")

closer, err := instance.Forward(guestListener, "tcp", "127.0.0.1:9000")
if err != nil {
    return err
}
defer closer.Close()

// Guest :8080 is now accessible at host localhost:9000
```

## Controlling Network Access

### Enable/Disable Internet

Control external network access:

```go
// Disable internet access (guest can still talk to host netstack)
instance.SetNetworkEnabled(false)

// Re-enable
instance.SetNetworkEnabled(true)
```

This is useful for sandboxed execution where you want to prevent network exfiltration.

## DNS Resolution

DNS works automatically inside the guest. The VM uses the host's DNS configuration by default.

Run DNS lookups from guest commands:

```go
output, _ := instance.Command("nslookup", "example.com").Output()
```

## Example: HTTP Server

Run a web server in the guest and access it from the host:

```go
func runWebServer(instance cc.Instance) error {
    // Write a simple Python server
    serverCode := `
from http.server import HTTPServer, SimpleHTTPRequestHandler
import os
os.chdir('/www')
httpd = HTTPServer(('', 8080), SimpleHTTPRequestHandler)
print('Serving on port 8080')
httpd.serve_forever()
`
    instance.MkdirAll("/www", 0755)
    instance.WriteFile("/www/index.html", []byte("<h1>Hello from VM!</h1>"), 0644)
    instance.WriteFile("/server.py", []byte(serverCode), 0644)

    // Start the server
    cmd := instance.Command("python3", "/server.py")
    if err := cmd.Start(); err != nil {
        return err
    }

    // Wait for server to start
    time.Sleep(time.Second)

    // Access it via Dial
    conn, err := instance.Dial("tcp", "127.0.0.1:8080")
    if err != nil {
        return err
    }
    defer conn.Close()

    conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
    response, _ := io.ReadAll(conn)
    fmt.Println(string(response))

    return nil
}
```

## Example: Web Scraping Sandbox

Create an isolated environment for web scraping:

```go
func scrapeInSandbox(url string) ([]byte, error) {
    client, _ := cc.NewOCIClient()
    source, _ := client.Pull(ctx, "python:3.12-slim")

    instance, _ := cc.New(source,
        cc.WithMemoryMB(256),
        cc.WithTimeout(30*time.Second),
    )
    defer instance.Close()

    // Install requests
    instance.Command("pip", "install", "requests").Run()

    // Write scraper script
    script := fmt.Sprintf(`
import requests
r = requests.get('%s')
print(r.text)
`, url)
    instance.WriteFile("/scrape.py", []byte(script), 0644)

    // Run it
    return instance.Command("python3", "/scrape.py").Output()
}
```

## Packet Capture

Capture network traffic for debugging:

```go
f, _ := os.Create("traffic.pcap")
defer f.Close()

instance, err := cc.New(source, cc.WithPacketCapture(f))
defer instance.Close()

// All network traffic is captured to traffic.pcap
// View with: wireshark traffic.pcap
```

## Next Steps

- [OCI Images](/api/oci-images/) - Pull and manage container images
- [Options Reference](/reference/options/) - Network-related options
