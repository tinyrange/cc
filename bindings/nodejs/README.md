# @crumblecracker/cc

TypeScript bindings for the cc virtualization library. Run containers as lightweight VMs with full isolation.

## Installation

```bash
npm install @crumblecracker/cc
# or
bun add @crumblecracker/cc
```

The `cc-helper` binary is automatically installed for your platform.

## Requirements

- Node.js 18+ or Bun
- Hypervisor support:
  - **macOS**: Apple Silicon (M1/M2/M3) with Hypervisor.framework
  - **Linux**: KVM support (`/dev/kvm` accessible)

## Supported Platforms

| Platform | Architecture | Package |
|----------|-------------|---------|
| macOS | arm64 (Apple Silicon) | `@crumblecracker/cc-darwin-arm64` |
| macOS | x64 (Intel) | `@crumblecracker/cc-darwin-x64` |
| Linux | x64 | `@crumblecracker/cc-linux-x64` |
| Linux | arm64 | `@crumblecracker/cc-linux-arm64` |

## Quick Start

```typescript
import { OCIClient } from '@crumblecracker/cc';

// Pull an image and create a VM instance
const client = new OCIClient();
const source = await client.pull('python:3-alpine');

await using inst = await source.createInstance({});

// Write a file to the VM
await inst.writeFile('/hello.txt', 'Hello, World!');

// Run Python to read it
const cmd = await inst.command('python3', '-c', 'print(open("/hello.txt").read())');
const output = await cmd.output();
console.log(output.toString().trim()); // "Hello, World!"

// Cleanup
await source.helper.close();
client.close();
```

## API Reference

### Library Functions

```typescript
// Version info
apiVersion(): string
apiVersionCompatible(major: number, minor: number): boolean
guestProtocolVersion(): number

// Lifecycle
init(): void
shutdown(): void

// Capabilities
supportsHypervisor(): boolean
queryCapabilities(): Promise<Capabilities>
```

### OCIClient

```typescript
const client = new OCIClient(cacheDir?: string);

// Pull from registry
const source = await client.pull('alpine:latest', options?: PullOptions);

// Load from local files
const source = await client.loadTar('/path/to/image.tar');
const source = await client.loadDir('/path/to/rootfs');

// Cleanup
client.close();
```

### Instance

```typescript
// Create instance
await using inst = await source.createInstance(options?: InstanceOptions);

// Properties
await inst.id(): Promise<string>
await inst.isRunning(): Promise<boolean>

// Filesystem operations
await inst.readFile(path: string): Promise<Buffer>
await inst.writeFile(path: string, data: Buffer, mode?: number): Promise<void>
await inst.stat(path: string): Promise<FileInfo>
await inst.mkdir(path: string, mode?: number): Promise<void>
await inst.remove(path: string): Promise<void>
await inst.removeAll(path: string): Promise<void>
await inst.readDir(path: string): Promise<DirEntry[]>

// File handles
await using file = await inst.open(path: string): Promise<File>
await using file = await inst.create(path: string): Promise<File>

// Commands
const cmd = await inst.command('echo', 'hello');
const output = await cmd.output();
const exitCode = await cmd.run();

// Networking
const listener = await inst.listen('tcp', ':8080');
const conn = await listener.accept();

// Cleanup
await inst.close();
```

### File

```typescript
await using file = await inst.open('/path/to/file');

await file.read(size?: number): Promise<Buffer>
await file.write(data: Buffer): Promise<number>
await file.seek(offset: number, whence?: SeekWhence): Promise<number>
await file.stat(): Promise<FileInfo>
await file.sync(): Promise<void>
await file.truncate(size: number): Promise<void>
await file.close(): Promise<void>
```

### Command

```typescript
const cmd = await inst.command('sh', '-c', 'echo hello');

// Configure
await cmd.setDir('/tmp');
await cmd.setEnv('KEY', 'value');

// Execute
const output = await cmd.output();           // stdout
const combined = await cmd.combinedOutput(); // stdout + stderr
const exitCode = await cmd.run();            // just run

// Async execution
await cmd.start();
const exitCode = await cmd.wait();
```

## Types

### InstanceOptions

```typescript
interface InstanceOptions {
  memoryMb?: number;      // Default: 256
  cpus?: number;          // Default: 1
  timeoutSeconds?: number;
  user?: string;
  enableDmesg?: boolean;
  mounts?: MountConfig[];
}
```

### FileInfo

```typescript
interface FileInfo {
  name: string;
  size: number;
  mode: number;
  modTimeUnix: number;
  isDir: boolean;
  isSymlink: boolean;
}
```

### DirEntry

```typescript
interface DirEntry {
  name: string;
  isDir: boolean;
  mode: number;
}
```

## Error Handling

All errors inherit from `CCError`:

```typescript
import {
  CCError,
  InvalidHandleError,
  InvalidArgumentError,
  NotRunningError,
  AlreadyClosedError,
  TimeoutError,
  HypervisorUnavailableError,
  IOError,
  NetworkError,
  CancelledError,
} from '@crumblecracker/cc';

try {
  await inst.readFile('/nonexistent');
} catch (err) {
  if (err instanceof IOError) {
    console.error('File not found:', err.message);
  }
}
```

## Resource Management

Use `await using` (ES2022 Explicit Resource Management) for automatic cleanup:

```typescript
// Automatic cleanup when scope exits
await using inst = await source.createInstance();
await using file = await inst.create('/tmp/test.txt');

// Or manual cleanup
const inst = await source.createInstance();
try {
  // ... use instance
} finally {
  await inst.close();
}
```

## Environment Variables

- `CC_HELPER_PATH` - Override path to cc-helper binary (optional, bundled by default)
- `CC_RUN_VM_TESTS` - Set to `1` to run VM integration tests

## Development

```bash
# Install dependencies
npm install

# Build
npm run build

# Run tests
npm test

# Run VM tests (requires hypervisor)
CC_RUN_VM_TESTS=1 npm test
```

## License

MIT
