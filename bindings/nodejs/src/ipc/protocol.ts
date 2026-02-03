/**
 * Binary encoder/decoder for the IPC protocol.
 *
 * Wire format: [2 bytes: msg_type (big endian)][4 bytes: payload_len (big endian)][payload]
 */

import { ErrorCode, errorFromCode, type CCError } from '../errors.js';
import type { DirEntry, FileInfo, InstanceOptions, MountConfig, SnapshotOptions } from '../types.js';

/** Header size in bytes */
export const HEADER_SIZE = 6;

/**
 * Message header structure.
 */
export interface Header {
  type: number;
  length: number;
}

/**
 * Read a header from a buffer.
 */
export function readHeader(buf: Buffer, offset = 0): Header {
  return {
    type: buf.readUInt16BE(offset),
    length: buf.readUInt32BE(offset + 2),
  };
}

/**
 * Write a header to a buffer.
 */
export function writeHeader(buf: Buffer, header: Header, offset = 0): void {
  buf.writeUInt16BE(header.type, offset);
  buf.writeUInt32BE(header.length, offset + 2);
}

/**
 * Encoder for writing IPC messages.
 */
export class Encoder {
  private buf: Buffer;
  private pos: number;

  constructor(initialCapacity = 4096) {
    this.buf = Buffer.alloc(initialCapacity);
    this.pos = 0;
  }

  /**
   * Reset the encoder for reuse.
   */
  reset(): void {
    this.pos = 0;
  }

  /**
   * Get the encoded bytes.
   */
  getBytes(): Buffer {
    return this.buf.subarray(0, this.pos);
  }

  /**
   * Ensure there's enough capacity for n more bytes.
   */
  private ensureCapacity(n: number): void {
    const required = this.pos + n;
    if (required > this.buf.length) {
      const newBuf = Buffer.alloc(Math.max(this.buf.length * 2, required));
      this.buf.copy(newBuf, 0, 0, this.pos);
      this.buf = newBuf;
    }
  }

  /**
   * Write a uint8.
   */
  uint8(v: number): this {
    this.ensureCapacity(1);
    this.buf.writeUInt8(v, this.pos);
    this.pos += 1;
    return this;
  }

  /**
   * Write a uint16 (big endian).
   */
  uint16(v: number): this {
    this.ensureCapacity(2);
    this.buf.writeUInt16BE(v, this.pos);
    this.pos += 2;
    return this;
  }

  /**
   * Write a uint32 (big endian).
   */
  uint32(v: number): this {
    this.ensureCapacity(4);
    this.buf.writeUInt32BE(v, this.pos);
    this.pos += 4;
    return this;
  }

  /**
   * Write a uint64 (big endian).
   */
  uint64(v: bigint): this {
    this.ensureCapacity(8);
    this.buf.writeBigUInt64BE(v, this.pos);
    this.pos += 8;
    return this;
  }

  /**
   * Write an int32 (big endian).
   */
  int32(v: number): this {
    this.ensureCapacity(4);
    this.buf.writeInt32BE(v, this.pos);
    this.pos += 4;
    return this;
  }

  /**
   * Write an int64 (big endian).
   */
  int64(v: bigint): this {
    this.ensureCapacity(8);
    this.buf.writeBigInt64BE(v, this.pos);
    this.pos += 8;
    return this;
  }

  /**
   * Write a bool (1 byte).
   */
  bool(v: boolean): this {
    return this.uint8(v ? 1 : 0);
  }

  /**
   * Write a length-prefixed string (4 bytes length + data).
   */
  string(s: string): this {
    const data = Buffer.from(s, 'utf-8');
    this.uint32(data.length);
    this.ensureCapacity(data.length);
    data.copy(this.buf, this.pos);
    this.pos += data.length;
    return this;
  }

  /**
   * Write a length-prefixed byte slice (4 bytes length + data).
   */
  bytes(data: Buffer | Uint8Array): this {
    this.uint32(data.length);
    this.ensureCapacity(data.length);
    Buffer.from(data).copy(this.buf, this.pos);
    this.pos += data.length;
    return this;
  }

  /**
   * Write a string slice (4 bytes count + strings).
   */
  stringSlice(ss: string[]): this {
    this.uint32(ss.length);
    for (const s of ss) {
      this.string(s);
    }
    return this;
  }

  /**
   * Encode mount configuration.
   */
  mountConfig(m: MountConfig): this {
    this.string(m.tag);
    this.string(m.hostPath ?? '');
    this.bool(m.writable ?? false);
    return this;
  }

  /**
   * Encode instance options.
   */
  instanceOptions(opts: InstanceOptions): this {
    this.uint64(BigInt(opts.memoryMb ?? 0));
    this.int32(opts.cpus ?? 0);
    // Convert timeout to nanoseconds
    const timeoutNanos = BigInt(Math.floor((opts.timeoutSeconds ?? 0) * 1e9));
    this.uint64(timeoutNanos);
    this.string(opts.user ?? '');
    this.bool(opts.enableDmesg ?? false);
    const mounts = opts.mounts ?? [];
    this.uint32(mounts.length);
    for (const m of mounts) {
      this.mountConfig(m);
    }
    return this;
  }

  /**
   * Encode snapshot options.
   */
  snapshotOptions(opts: SnapshotOptions): this {
    this.stringSlice(opts.excludes ?? []);
    this.string(opts.cacheDir ?? '');
    return this;
  }
}

/**
 * Decoder for reading IPC messages.
 */
export class Decoder {
  private buf: Buffer;
  private pos: number;

  constructor(buf: Buffer) {
    this.buf = buf;
    this.pos = 0;
  }

  /**
   * Get remaining unread bytes.
   */
  remaining(): number {
    return this.buf.length - this.pos;
  }

  /**
   * Read a uint8.
   */
  uint8(): number {
    if (this.pos >= this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readUInt8(this.pos);
    this.pos += 1;
    return v;
  }

  /**
   * Read a uint16 (big endian).
   */
  uint16(): number {
    if (this.pos + 2 > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readUInt16BE(this.pos);
    this.pos += 2;
    return v;
  }

  /**
   * Read a uint32 (big endian).
   */
  uint32(): number {
    if (this.pos + 4 > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readUInt32BE(this.pos);
    this.pos += 4;
    return v;
  }

  /**
   * Read a uint64 (big endian).
   */
  uint64(): bigint {
    if (this.pos + 8 > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readBigUInt64BE(this.pos);
    this.pos += 8;
    return v;
  }

  /**
   * Read an int32 (big endian).
   */
  int32(): number {
    if (this.pos + 4 > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readInt32BE(this.pos);
    this.pos += 4;
    return v;
  }

  /**
   * Read an int64 (big endian).
   */
  int64(): bigint {
    if (this.pos + 8 > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const v = this.buf.readBigInt64BE(this.pos);
    this.pos += 8;
    return v;
  }

  /**
   * Read a bool (1 byte).
   */
  bool(): boolean {
    return this.uint8() !== 0;
  }

  /**
   * Read a length-prefixed string.
   */
  string(): string {
    const length = this.uint32();
    if (this.pos + length > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const s = this.buf.toString('utf-8', this.pos, this.pos + length);
    this.pos += length;
    return s;
  }

  /**
   * Read a length-prefixed byte slice.
   */
  bytes(): Buffer {
    const length = this.uint32();
    if (this.pos + length > this.buf.length) {
      throw new Error('Unexpected end of buffer');
    }
    const b = Buffer.alloc(length);
    this.buf.copy(b, 0, this.pos, this.pos + length);
    this.pos += length;
    return b;
  }

  /**
   * Read a string slice.
   */
  stringSlice(): string[] {
    const count = this.uint32();
    const ss: string[] = [];
    for (let i = 0; i < count; i++) {
      ss.push(this.string());
    }
    return ss;
  }

  /**
   * Decode file info.
   */
  fileInfo(): FileInfo {
    return {
      name: this.string(),
      size: Number(this.int64()),
      mode: this.uint32(),
      modTimeUnix: Number(this.int64()),
      isDir: this.bool(),
      isSymlink: this.bool(),
    };
  }

  /**
   * Decode directory entry.
   */
  dirEntry(): DirEntry {
    return {
      name: this.string(),
      isDir: this.bool(),
      mode: this.uint32(),
    };
  }

  /**
   * Decode an error from the response.
   * Returns null if no error (code = 0).
   */
  error(): CCError | null {
    const code = this.uint8();
    if (code === ErrorCode.OK) {
      return null;
    }
    const message = this.string();
    const op = this.string();
    const path = this.string();
    return errorFromCode(code, message, op || undefined, path || undefined);
  }
}
