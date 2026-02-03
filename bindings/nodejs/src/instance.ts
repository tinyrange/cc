/**
 * Instance class for running VM instances and filesystem operations.
 */

import { AlreadyClosedError, CCError, ErrorCode } from './errors.js';
import type { IPCClient } from './ipc/client.js';
import {
  MsgCmdEntrypoint,
  MsgCmdNew,
  MsgFileClose,
  MsgFileName,
  MsgFileRead,
  MsgFileSeek,
  MsgFileStat,
  MsgFileSync,
  MsgFileTruncate,
  MsgFileWrite,
  MsgFsChmod,
  MsgFsChown,
  MsgFsChtimes,
  MsgFsCreate,
  MsgFsLstat,
  MsgFsMkdir,
  MsgFsMkdirAll,
  MsgFsOpen,
  MsgFsOpenFile,
  MsgFsReadDir,
  MsgFsReadFile,
  MsgFsReadlink,
  MsgFsRemove,
  MsgFsRemoveAll,
  MsgFsRename,
  MsgFsSnapshot,
  MsgFsStat,
  MsgFsSymlink,
  MsgFsWriteFile,
  MsgInstanceClose,
  MsgInstanceExec,
  MsgInstanceID,
  MsgInstanceIsRunning,
  MsgInstanceNew,
  MsgInstanceSetConsole,
  MsgInstanceSetNetwork,
  MsgInstanceWait,
  MsgNetListen,
  MsgSnapshotAsSource,
  MsgSnapshotCacheKey,
  MsgSnapshotClose,
  MsgSnapshotParent,
} from './ipc/messages.js';
import { Decoder, Encoder } from './ipc/protocol.js';
import { Cmd } from './cmd.js';
import { Listener } from './network.js';
import type {
  DirEntry,
  FileInfo,
  InstanceOptions,
  SeekWhenceType,
  SnapshotOptions,
} from './types.js';
import { SeekWhence } from './types.js';

/**
 * A file handle for reading/writing files in a VM instance.
 */
export class File implements AsyncDisposable {
  private client: IPCClient;
  private _handle: bigint;
  private _path: string;
  private _closed = false;

  constructor(client: IPCClient, handle: bigint, path: string) {
    this.client = client;
    this._handle = handle;
    this._path = path;
  }

  /**
   * Check if the file is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the file name.
   */
  async name(): Promise<string> {
    if (this._closed) return this._path;

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgFileName, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) return this._path;

    return dec.string() || this._path;
  }

  private checkOpen(): void {
    if (this._closed) {
      throw new AlreadyClosedError('File is closed');
    }
  }

  /**
   * Read up to size bytes from the file.
   * If size is -1, read until EOF.
   */
  async read(size = -1): Promise<Buffer> {
    this.checkOpen();

    if (size === -1) {
      // Read all
      const chunks: Buffer[] = [];
      const chunkSize = 65536;
      while (true) {
        const data = await this.readChunk(chunkSize);
        if (data.length === 0) break;
        chunks.push(data);
      }
      return Buffer.concat(chunks);
    }

    return this.readChunk(size);
  }

  private async readChunk(size: number): Promise<Buffer> {
    const enc = new Encoder();
    enc.uint64(this._handle).uint32(size);

    const resp = await this.client.call(MsgFileRead, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.bytes();
  }

  /**
   * Write data to the file.
   * @returns Number of bytes written
   */
  async write(data: Buffer | Uint8Array): Promise<number> {
    this.checkOpen();

    const enc = new Encoder();
    enc.uint64(this._handle).bytes(data);

    const resp = await this.client.call(MsgFileWrite, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.uint32();
  }

  /**
   * Seek to a position in the file.
   * @returns New file position
   */
  async seek(offset: number, whence: SeekWhenceType = SeekWhence.Set): Promise<number> {
    this.checkOpen();

    const enc = new Encoder();
    enc.uint64(this._handle).int64(BigInt(offset)).int32(whence);

    const resp = await this.client.call(MsgFileSeek, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return Number(dec.int64());
  }

  /**
   * Sync the file to disk.
   */
  async sync(): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgFileSync, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Truncate the file to the given size.
   */
  async truncate(size: number): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.uint64(this._handle).int64(BigInt(size));

    const resp = await this.client.call(MsgFileTruncate, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Get file information.
   */
  async stat(): Promise<FileInfo> {
    this.checkOpen();

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgFileStat, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.fileInfo();
  }

  /**
   * Close the file.
   */
  async close(): Promise<void> {
    if (this._closed) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    try {
      const resp = await this.client.call(MsgFileClose, enc.getBytes());
      const dec = new Decoder(resp);
      dec.error(); // Ignore errors during close
    } catch {
      // Ignore errors during close
    }

    this._closed = true;
  }

  /**
   * Async dispose support for "await using" syntax.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }
}

/**
 * A filesystem snapshot from a VM instance.
 */
export class Snapshot implements AsyncDisposable {
  private client: IPCClient;
  private _handle: bigint;
  private _closed = false;

  constructor(client: IPCClient, handle: bigint) {
    this.client = client;
    this._handle = handle;
  }

  /**
   * Get the snapshot handle.
   */
  get handle(): bigint {
    if (this._closed) {
      throw new AlreadyClosedError('Snapshot is closed');
    }
    return this._handle;
  }

  /**
   * Check if the snapshot is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the snapshot cache key.
   */
  async cacheKey(): Promise<string> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgSnapshotCacheKey, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Get the parent snapshot, if any.
   */
  async parent(): Promise<Snapshot | null> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgSnapshotParent, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const parentHandle = dec.uint64();
    if (parentHandle === 0n) {
      return null;
    }

    return new Snapshot(this.client, parentHandle);
  }

  /**
   * Convert the snapshot to a source handle.
   * Note: This returns the snapshot handle which can be used as a source.
   */
  async asSourceHandle(): Promise<bigint> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgSnapshotAsSource, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.uint64();
  }

  /**
   * Close the snapshot.
   */
  async close(): Promise<void> {
    if (this._closed) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    try {
      const resp = await this.client.call(MsgSnapshotClose, enc.getBytes());
      const dec = new Decoder(resp);
      dec.error(); // Ignore errors during close
    } catch {
      // Ignore errors during close
    }

    this._closed = true;
  }

  /**
   * Async dispose support for "await using" syntax.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }
}

/**
 * Source type for creating instances.
 */
export const SourceType = {
  Tar: 0,
  Directory: 1,
  Reference: 2,
} as const;

/**
 * A running VM instance.
 *
 * Provides filesystem operations, command execution, and networking.
 *
 * @example
 * await using inst = await Instance.create(client, sourceType, sourcePath, imageRef, cacheDir, options);
 *
 * // Run a command
 * const output = await inst.command('echo', 'hello').output();
 *
 * // Read a file
 * const data = await inst.readFile('/etc/hosts');
 *
 * // Write a file
 * await inst.writeFile('/tmp/test.txt', Buffer.from('hello world'));
 */
export class Instance implements AsyncDisposable {
  private client: IPCClient;
  private _closed = false;

  private constructor(client: IPCClient) {
    this.client = client;
  }

  /**
   * Create a new instance from a source.
   */
  static async create(
    client: IPCClient,
    sourceType: number,
    sourcePath: string,
    imageRef: string,
    cacheDir: string,
    options?: InstanceOptions
  ): Promise<Instance> {
    const enc = new Encoder();
    enc.uint8(sourceType);
    enc.string(sourcePath);
    enc.string(imageRef);
    enc.string(cacheDir);
    enc.instanceOptions(options ?? {});

    const resp = await client.call(MsgInstanceNew, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return new Instance(client);
  }

  private checkOpen(): void {
    if (this._closed) {
      throw new AlreadyClosedError('Instance is closed');
    }
  }

  /**
   * Check if the instance is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the instance ID.
   */
  async id(): Promise<string> {
    if (this._closed) return '';

    const resp = await this.client.call(MsgInstanceID, Buffer.alloc(0));
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Check if the instance is still running.
   */
  async isRunning(): Promise<boolean> {
    if (this._closed) return false;

    const resp = await this.client.call(MsgInstanceIsRunning, Buffer.alloc(0));
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) return false;

    return dec.bool();
  }

  /**
   * Wait for the instance to terminate.
   */
  async wait(): Promise<void> {
    if (this._closed) return;

    const resp = await this.client.call(MsgInstanceWait, Buffer.alloc(0));
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Set the console size for interactive mode.
   */
  async setConsoleSize(cols: number, rows: number): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.int32(cols).int32(rows);

    const resp = await this.client.call(MsgInstanceSetConsole, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Enable or disable network access.
   */
  async setNetworkEnabled(enabled: boolean): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.bool(enabled);

    const resp = await this.client.call(MsgInstanceSetNetwork, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  // ========== Filesystem Operations ==========

  /**
   * Open a file for reading.
   */
  async open(path: string): Promise<File> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsOpen, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new File(this.client, handle, path);
  }

  /**
   * Create or truncate a file.
   */
  async create(path: string): Promise<File> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsCreate, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new File(this.client, handle, path);
  }

  /**
   * Open a file with specific flags and permissions.
   */
  async openFile(path: string, flags: number, mode = 0o644): Promise<File> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).int32(flags).uint32(mode);

    const resp = await this.client.call(MsgFsOpenFile, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new File(this.client, handle, path);
  }

  /**
   * Read entire file contents.
   */
  async readFile(path: string): Promise<Buffer> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsReadFile, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.bytes();
  }

  /**
   * Write entire file contents.
   */
  async writeFile(path: string, data: Buffer | Uint8Array, mode = 0o644): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).bytes(data).uint32(mode);

    const resp = await this.client.call(MsgFsWriteFile, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Get file information by path.
   */
  async stat(path: string): Promise<FileInfo> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsStat, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.fileInfo();
  }

  /**
   * Get file information (don't follow symlinks).
   */
  async lstat(path: string): Promise<FileInfo> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsLstat, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.fileInfo();
  }

  /**
   * Remove a file.
   */
  async remove(path: string): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsRemove, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Remove a file or directory recursively.
   */
  async removeAll(path: string): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsRemoveAll, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Create a directory.
   */
  async mkdir(path: string, mode = 0o755): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).uint32(mode);

    const resp = await this.client.call(MsgFsMkdir, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Create a directory and all parent directories.
   */
  async mkdirAll(path: string, mode = 0o755): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).uint32(mode);

    const resp = await this.client.call(MsgFsMkdirAll, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Rename a file or directory.
   */
  async rename(oldPath: string, newPath: string): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(oldPath).string(newPath);

    const resp = await this.client.call(MsgFsRename, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Create a symbolic link.
   */
  async symlink(target: string, linkPath: string): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(target).string(linkPath);

    const resp = await this.client.call(MsgFsSymlink, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Read a symbolic link target.
   */
  async readlink(path: string): Promise<string> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsReadlink, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Read directory contents.
   */
  async readDir(path: string): Promise<DirEntry[]> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path);

    const resp = await this.client.call(MsgFsReadDir, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const count = dec.uint32();
    const entries: DirEntry[] = [];
    for (let i = 0; i < count; i++) {
      entries.push(dec.dirEntry());
    }
    return entries;
  }

  /**
   * Change file permissions.
   */
  async chmod(path: string, mode: number): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).uint32(mode);

    const resp = await this.client.call(MsgFsChmod, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Change file owner.
   */
  async chown(path: string, uid: number, gid: number): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).int32(uid).int32(gid);

    const resp = await this.client.call(MsgFsChown, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  /**
   * Change file access and modification times.
   */
  async chtimes(path: string, atimeUnix: number, mtimeUnix: number): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(path).int64(BigInt(atimeUnix)).int64(BigInt(mtimeUnix));

    const resp = await this.client.call(MsgFsChtimes, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  // ========== Command Execution ==========

  /**
   * Create a command to run in the instance.
   */
  async command(name: string, ...args: string[]): Promise<Cmd> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(name).stringSlice(args);

    const resp = await this.client.call(MsgCmdNew, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new Cmd(this.client, handle);
  }

  /**
   * Create a command using the container's entrypoint.
   */
  async entrypointCommand(...args: string[]): Promise<Cmd> {
    this.checkOpen();

    const enc = new Encoder();
    enc.stringSlice(args);

    const resp = await this.client.call(MsgCmdEntrypoint, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new Cmd(this.client, handle);
  }

  /**
   * Replace the init process with the specified command.
   * This is a terminal operation - the instance cannot be used for
   * other operations after this.
   */
  async exec(name: string, ...args: string[]): Promise<void> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(name).stringSlice(args);

    const resp = await this.client.call(MsgInstanceExec, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;
  }

  // ========== Networking ==========

  /**
   * Listen for connections on the guest network.
   */
  async listen(network: string, address: string): Promise<Listener> {
    this.checkOpen();

    const enc = new Encoder();
    enc.string(network).string(address);

    const resp = await this.client.call(MsgNetListen, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new Listener(this.client, handle);
  }

  // ========== Snapshots ==========

  /**
   * Take a filesystem snapshot.
   */
  async snapshotFilesystem(options?: SnapshotOptions): Promise<Snapshot> {
    this.checkOpen();

    const enc = new Encoder();
    enc.snapshotOptions(options ?? {});

    const resp = await this.client.call(MsgFsSnapshot, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const handle = dec.uint64();
    return new Snapshot(this.client, handle);
  }

  /**
   * Close the instance and release resources.
   */
  async close(): Promise<void> {
    if (this._closed) return;

    try {
      const resp = await this.client.call(MsgInstanceClose, Buffer.alloc(0));
      const dec = new Decoder(resp);
      dec.error(); // Ignore errors during close
    } catch {
      // Ignore errors during close
    }

    this._closed = true;
  }

  /**
   * Async dispose support for "await using" syntax.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }
}
