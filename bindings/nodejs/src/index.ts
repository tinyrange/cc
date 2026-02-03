/**
 * Node.js/Bun TypeScript bindings for the cc virtualization library.
 *
 * This package provides a TypeScript interface to cc's virtualization primitives,
 * allowing you to pull OCI images, create VM instances, and interact with them
 * through filesystem operations, command execution, and networking.
 *
 * @example
 * import { init, shutdown, apiVersion, supportsHypervisor, OCIClient, Instance } from '@crumblecracker/cc';
 *
 * // Initialize library
 * const client = await init();
 *
 * // Check version
 * console.log(apiVersion()); // "0.1.0"
 *
 * // Check hypervisor
 * const available = await supportsHypervisor(client);
 *
 * // OCI Client usage
 * const source = await client.pull('alpine:latest');
 *
 * // Create instance
 * await using inst = await Instance.create(source, { memoryMb: 256, cpus: 1 });
 * console.log(await inst.id());
 * console.log(await inst.isRunning());
 *
 * // Run command
 * const output = await (await inst.command('echo', 'Hello')).output();
 *
 * // File operations
 * await inst.writeFile('/tmp/test.txt', Buffer.from('Hello'));
 * const data = await inst.readFile('/tmp/test.txt');
 *
 * // Cleanup
 * await shutdown(client);
 *
 * @packageDocumentation
 */

// Re-export errors
export {
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
  ErrorCode,
  errorFromCode,
} from './errors.js';

// Re-export types
export {
  PullPolicy,
  SeekWhence,
  O_RDONLY,
  O_WRONLY,
  O_RDWR,
  O_APPEND,
  O_CREATE,
  O_TRUNC,
  O_EXCL,
  type PullPolicyType,
  type SeekWhenceType,
  type DownloadProgress,
  type ProgressCallback,
  type PullOptions,
  type MountConfig,
  type InstanceOptions,
  type FileInfo,
  type DirEntry,
  type ImageConfig,
  type Capabilities,
  type SnapshotOptions,
} from './types.js';

// Re-export classes
export { File, Snapshot, Instance, SourceType } from './instance.js';
export { Cmd } from './cmd.js';
export { Listener, Conn } from './network.js';
export { HelperProcess, HelperNotFoundError, findHelper, spawnHelper } from './helper.js';
export { IPCClient, connectTo } from './ipc/client.js';
export { Encoder, Decoder, HEADER_SIZE, readHeader, writeHeader } from './ipc/protocol.js';

// Version information
export const VERSION = '0.1.0';

/**
 * Get the API version string.
 * This is the version of the TypeScript bindings.
 */
export function apiVersion(): string {
  return VERSION;
}

/**
 * Check if the runtime library is compatible with the given version.
 */
export function apiVersionCompatible(major: number, minor: number): boolean {
  const [currentMajor, currentMinor] = VERSION.split('.').map(Number);
  if (major !== currentMajor) return false;
  if (minor > currentMinor) return false;
  return true;
}

/**
 * Get the guest protocol version.
 * This returns 1 for compatibility with the Go bindings.
 */
export function guestProtocolVersion(): number {
  return 1;
}

/**
 * Library state for global initialization.
 */
let _initialized = false;

/**
 * Initialize the library.
 * For the IPC-based bindings, this is a no-op but provided for API compatibility.
 */
export function init(): void {
  _initialized = true;
}

/**
 * Shutdown the library.
 * For the IPC-based bindings, this is a no-op but provided for API compatibility.
 */
export function shutdown(): void {
  _initialized = false;
}

/**
 * Check if the library is initialized.
 */
export function isInitialized(): boolean {
  return _initialized;
}

/**
 * Cancel token for long-running operations.
 * Note: In the IPC-based architecture, cancellation is handled differently.
 * This is provided for API compatibility.
 */
export class CancelToken {
  private _cancelled = false;

  /**
   * Cancel the token.
   */
  cancel(): void {
    this._cancelled = true;
  }

  /**
   * Check if the token is cancelled.
   */
  get isCancelled(): boolean {
    return this._cancelled;
  }

  /**
   * Close the token (no-op for compatibility).
   */
  close(): void {
    // No-op
  }
}

// Import for OCIClient
import { HelperProcess, spawnHelper } from './helper.js';
import { Instance, SourceType } from './instance.js';
import { IPCClient } from './ipc/client.js';
import type { ImageConfig, InstanceOptions, PullOptions } from './types.js';
import { PullPolicy } from './types.js';

/**
 * Instance source representing a pulled or loaded image.
 */
export class InstanceSource {
  private _helper: HelperProcess;
  private _imageRef: string;
  private _cacheDir: string;
  private _sourceType: number;
  private _sourcePath: string;
  private _closed = false;

  constructor(
    helper: HelperProcess,
    sourceType: number,
    sourcePath: string,
    imageRef: string,
    cacheDir: string
  ) {
    this._helper = helper;
    this._sourceType = sourceType;
    this._sourcePath = sourcePath;
    this._imageRef = imageRef;
    this._cacheDir = cacheDir;
  }

  /**
   * Get the helper process.
   */
  get helper(): HelperProcess {
    return this._helper;
  }

  /**
   * Get the source type.
   */
  get sourceType(): number {
    return this._sourceType;
  }

  /**
   * Get the source path.
   */
  get sourcePath(): string {
    return this._sourcePath;
  }

  /**
   * Get the image reference.
   */
  get imageRef(): string {
    return this._imageRef;
  }

  /**
   * Get the cache directory.
   */
  get cacheDir(): string {
    return this._cacheDir;
  }

  /**
   * Check if the source is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the image configuration.
   * Note: This requires additional IPC messages not yet implemented.
   * Returns a placeholder for now.
   */
  async getConfig(): Promise<ImageConfig> {
    // TODO: Implement when config retrieval is added to IPC
    return {
      architecture: process.arch === 'x64' ? 'amd64' : process.arch,
      env: [],
      workingDir: '/',
      entrypoint: [],
      cmd: ['/bin/sh'],
      user: 'root',
    };
  }

  /**
   * Create an instance from this source.
   */
  async createInstance(options?: InstanceOptions): Promise<Instance> {
    return Instance.create(
      this._helper.client,
      this._sourceType,
      this._sourcePath,
      this._imageRef,
      this._cacheDir,
      options
    );
  }

  /**
   * Close the source.
   */
  close(): void {
    this._closed = true;
  }
}

/**
 * OCI client for pulling and managing container images.
 *
 * Note: In the IPC-based architecture, each pull/load operation spawns a new
 * cc-helper process. For repeated operations, consider reusing the InstanceSource.
 */
export class OCIClient implements AsyncDisposable {
  private _cacheDir?: string;
  private _closed = false;

  constructor(cacheDir?: string) {
    this._cacheDir = cacheDir;
  }

  /**
   * Get the cache directory.
   */
  get cacheDir(): string {
    return this._cacheDir ?? '';
  }

  /**
   * Check if the client is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Pull an OCI image from a registry.
   *
   * This spawns a cc-helper process that will pull the image and manage the VM.
   */
  async pull(imageRef: string, _options?: PullOptions): Promise<InstanceSource> {
    if (this._closed) {
      throw new Error('OCIClient is closed');
    }

    const helper = await spawnHelper();
    return new InstanceSource(
      helper,
      SourceType.Reference,
      '',
      imageRef,
      this._cacheDir ?? ''
    );
  }

  /**
   * Load an image from a local tar file.
   */
  async loadTar(tarPath: string): Promise<InstanceSource> {
    if (this._closed) {
      throw new Error('OCIClient is closed');
    }

    const helper = await spawnHelper();
    return new InstanceSource(
      helper,
      SourceType.Tar,
      tarPath,
      '',
      this._cacheDir ?? ''
    );
  }

  /**
   * Load an image from a prebaked directory.
   */
  async loadDir(dirPath: string): Promise<InstanceSource> {
    if (this._closed) {
      throw new Error('OCIClient is closed');
    }

    const helper = await spawnHelper();
    return new InstanceSource(
      helper,
      SourceType.Directory,
      dirPath,
      '',
      this._cacheDir ?? ''
    );
  }

  /**
   * Close the client.
   */
  close(): void {
    this._closed = true;
  }

  /**
   * Async dispose support for "await using" syntax.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    this.close();
  }
}

/**
 * Query system capabilities.
 * Note: This requires a running helper process.
 */
export async function queryCapabilities(): Promise<import('./types.js').Capabilities> {
  // For now, return basic capabilities based on the current system
  return {
    hypervisorAvailable: process.platform === 'darwin' || process.platform === 'linux',
    maxMemoryMb: 0,
    maxCpus: 0,
    architecture: process.arch === 'x64' ? 'x86_64' : process.arch === 'arm64' ? 'arm64' : process.arch,
  };
}

/**
 * Check if hypervisor is available.
 * Note: This is a simplified check based on platform.
 */
export function supportsHypervisor(): boolean {
  // On macOS and Linux, hypervisor support is typically available
  return process.platform === 'darwin' || process.platform === 'linux';
}
