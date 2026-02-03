/**
 * Unix socket client for communicating with cc-helper.
 */

import * as net from 'node:net';
import { CCError, ErrorCode, errorFromCode } from '../errors.js';
import { MsgError } from './messages.js';
import { Decoder, Encoder, HEADER_SIZE, readHeader, writeHeader } from './protocol.js';

/**
 * Client manages a connection to a cc-helper process.
 */
export class IPCClient {
  private socket: net.Socket;
  private closed = false;
  private responseBuffer: Buffer = Buffer.alloc(0);
  private pendingResolve: ((value: Buffer) => void) | null = null;
  private pendingReject: ((reason: Error) => void) | null = null;
  private expectedLength = 0;
  private expectedType = 0;

  constructor(socket: net.Socket) {
    this.socket = socket;
    this.setupSocket();
  }

  private setupSocket(): void {
    this.socket.on('data', (data: Buffer) => {
      this.responseBuffer = Buffer.concat([this.responseBuffer, data]);
      this.processBuffer();
    });

    this.socket.on('error', (err: Error) => {
      if (this.pendingReject) {
        this.pendingReject(err);
        this.pendingResolve = null;
        this.pendingReject = null;
      }
    });

    this.socket.on('close', () => {
      this.closed = true;
      if (this.pendingReject) {
        this.pendingReject(new CCError('Connection closed', ErrorCode.IO));
        this.pendingResolve = null;
        this.pendingReject = null;
      }
    });
  }

  private processBuffer(): void {
    if (!this.pendingResolve) return;

    // Need at least a header
    if (this.responseBuffer.length < HEADER_SIZE) return;

    // Read header if we haven't yet
    if (this.expectedLength === 0) {
      const header = readHeader(this.responseBuffer);
      this.expectedType = header.type;
      this.expectedLength = header.length;
    }

    // Check if we have the full payload
    const totalNeeded = HEADER_SIZE + this.expectedLength;
    if (this.responseBuffer.length < totalNeeded) return;

    // Extract payload
    const payload = this.responseBuffer.subarray(HEADER_SIZE, totalNeeded);
    this.responseBuffer = this.responseBuffer.subarray(totalNeeded);

    // Check for error response
    if (this.expectedType === MsgError) {
      const dec = new Decoder(payload);
      const err = dec.error();
      if (err) {
        this.pendingReject?.(err);
      } else {
        this.pendingReject?.(new CCError('Unknown error', ErrorCode.Unknown));
      }
    } else {
      this.pendingResolve?.(payload);
    }

    // Reset state
    this.pendingResolve = null;
    this.pendingReject = null;
    this.expectedLength = 0;
    this.expectedType = 0;
  }

  /**
   * Check if the client is closed.
   */
  get isClosed(): boolean {
    return this.closed;
  }

  /**
   * Close the client connection.
   */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.socket.destroy();
  }

  /**
   * Send a request and wait for a response.
   * This is a synchronous RPC call.
   */
  async call(msgType: number, payload: Buffer): Promise<Buffer> {
    if (this.closed) {
      throw new CCError('Client closed', ErrorCode.AlreadyClosed);
    }

    // Create request packet
    const header = Buffer.alloc(HEADER_SIZE);
    writeHeader(header, { type: msgType, length: payload.length });

    return new Promise((resolve, reject) => {
      this.pendingResolve = resolve;
      this.pendingReject = reject;
      this.expectedLength = 0;
      this.expectedType = 0;

      // Write header then payload
      this.socket.write(header, (err) => {
        if (err) {
          this.pendingResolve = null;
          this.pendingReject = null;
          reject(err);
          return;
        }
        if (payload.length > 0) {
          this.socket.write(payload, (err) => {
            if (err) {
              this.pendingResolve = null;
              this.pendingReject = null;
              reject(err);
            }
          });
        }
      });
    });
  }

  /**
   * Convenience method that uses an encoder for the request.
   */
  async callWithEncoder(
    msgType: number,
    encode: (enc: Encoder) => void
  ): Promise<Buffer> {
    const enc = new Encoder();
    encode(enc);
    return this.call(msgType, enc.getBytes());
  }
}

/**
 * Connect to an existing cc-helper process at the given socket path.
 */
export function connectTo(socketPath: string): Promise<IPCClient> {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(socketPath, () => {
      resolve(new IPCClient(socket));
    });
    socket.on('error', reject);
  });
}
