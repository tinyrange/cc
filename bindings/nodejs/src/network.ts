/**
 * Network operations for VM instances.
 */

import { AlreadyClosedError } from './errors.js';
import type { IPCClient } from './ipc/client.js';
import {
  MsgConnClose,
  MsgConnLocalAddr,
  MsgConnRead,
  MsgConnRemoteAddr,
  MsgConnWrite,
  MsgListenerAccept,
  MsgListenerAddr,
  MsgListenerClose,
} from './ipc/messages.js';
import { Decoder, Encoder } from './ipc/protocol.js';

/**
 * A network connection.
 */
export class Conn implements AsyncDisposable {
  private client: IPCClient;
  private _handle: bigint;
  private _closed = false;

  constructor(client: IPCClient, handle: bigint) {
    this.client = client;
    this._handle = handle;
  }

  /**
   * Get the connection handle.
   */
  get handle(): bigint {
    if (this._closed) {
      throw new AlreadyClosedError('Connection is closed');
    }
    return this._handle;
  }

  /**
   * Check if the connection is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the local address.
   */
  async localAddr(): Promise<string> {
    if (this._closed) return '';

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgConnLocalAddr, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Get the remote address.
   */
  async remoteAddr(): Promise<string> {
    if (this._closed) return '';

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgConnRemoteAddr, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Read up to size bytes from the connection.
   */
  async read(size: number): Promise<Buffer> {
    const enc = new Encoder();
    enc.uint64(this.handle).uint32(size);

    const resp = await this.client.call(MsgConnRead, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.bytes();
  }

  /**
   * Write data to the connection.
   * @returns Number of bytes written
   */
  async write(data: Buffer | Uint8Array): Promise<number> {
    const enc = new Encoder();
    enc.uint64(this.handle).bytes(data);

    const resp = await this.client.call(MsgConnWrite, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.uint32();
  }

  /**
   * Close the connection.
   */
  async close(): Promise<void> {
    if (this._closed) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    try {
      const resp = await this.client.call(MsgConnClose, enc.getBytes());
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
 * A network listener for accepting connections.
 */
export class Listener implements AsyncDisposable {
  private client: IPCClient;
  private _handle: bigint;
  private _closed = false;

  constructor(client: IPCClient, handle: bigint) {
    this.client = client;
    this._handle = handle;
  }

  /**
   * Get the listener handle.
   */
  get handle(): bigint {
    if (this._closed) {
      throw new AlreadyClosedError('Listener is closed');
    }
    return this._handle;
  }

  /**
   * Check if the listener is closed.
   */
  get closed(): boolean {
    return this._closed;
  }

  /**
   * Get the listener address.
   */
  async addr(): Promise<string> {
    if (this._closed) return '';

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgListenerAddr, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.string();
  }

  /**
   * Accept a connection from a client.
   */
  async accept(): Promise<Conn> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgListenerAccept, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const connHandle = dec.uint64();
    return new Conn(this.client, connHandle);
  }

  /**
   * Close the listener.
   */
  async close(): Promise<void> {
    if (this._closed) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    try {
      const resp = await this.client.call(MsgListenerClose, enc.getBytes());
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
