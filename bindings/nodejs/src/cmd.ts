/**
 * Command execution for VM instances.
 */

import { AlreadyClosedError, CCError, ErrorCode } from './errors.js';
import type { IPCClient } from './ipc/client.js';
import {
  MsgCmdCombinedOutput,
  MsgCmdEnviron,
  MsgCmdExitCode,
  MsgCmdFree,
  MsgCmdGetEnv,
  MsgCmdKill,
  MsgCmdOutput,
  MsgCmdRun,
  MsgCmdSetDir,
  MsgCmdSetEnv,
  MsgCmdStart,
  MsgCmdWait,
} from './ipc/messages.js';
import { Decoder, Encoder } from './ipc/protocol.js';

/**
 * A command to run in a VM instance.
 *
 * Commands can be configured with environment variables and working directory
 * before being executed.
 *
 * @example
 * // Simple command
 * const output = await instance.command('echo', 'hello', 'world').output();
 *
 * // With environment
 * const cmd = instance.command('env');
 * cmd.setEnv('MY_VAR', 'my_value');
 * cmd.setDir('/tmp');
 * const exitCode = await cmd.run();
 *
 * // Async execution
 * const cmd = instance.command('sleep', '10');
 * await cmd.start();
 * // ... do other work ...
 * await cmd.wait();
 */
export class Cmd implements AsyncDisposable {
  private client: IPCClient;
  private _handle: bigint;
  private _started = false;
  private _freed = false;

  constructor(client: IPCClient, handle: bigint) {
    this.client = client;
    this._handle = handle;
  }

  /**
   * Get the command handle.
   */
  get handle(): bigint {
    if (this._freed) {
      throw new AlreadyClosedError('Cmd has been freed');
    }
    return this._handle;
  }

  /**
   * Check if the command has been started.
   */
  get started(): boolean {
    return this._started;
  }

  /**
   * Check if the command has been freed.
   */
  get freed(): boolean {
    return this._freed;
  }

  /**
   * Set the working directory for the command.
   * @returns this for method chaining
   */
  async setDir(dir: string): Promise<this> {
    const enc = new Encoder();
    enc.uint64(this.handle).string(dir);

    const resp = await this.client.call(MsgCmdSetDir, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return this;
  }

  /**
   * Set an environment variable.
   * @returns this for method chaining
   */
  async setEnv(key: string, value: string): Promise<this> {
    const enc = new Encoder();
    enc.uint64(this.handle).string(key).string(value);

    const resp = await this.client.call(MsgCmdSetEnv, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return this;
  }

  /**
   * Get an environment variable.
   * @returns Variable value, or undefined if not set
   */
  async getEnv(key: string): Promise<string | undefined> {
    const enc = new Encoder();
    enc.uint64(this.handle).string(key);

    const resp = await this.client.call(MsgCmdGetEnv, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const value = dec.string();
    return value || undefined;
  }

  /**
   * Get all environment variables.
   * @returns Array of "KEY=VALUE" strings
   */
  async environ(): Promise<string[]> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdEnviron, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.stringSlice();
  }

  /**
   * Start the command (non-blocking).
   * The command runs asynchronously. Use wait() to wait for completion.
   */
  async start(): Promise<void> {
    if (this._started) {
      throw new CCError('Cmd has already been started', ErrorCode.InvalidArgument);
    }

    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdStart, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    this._started = true;
  }

  /**
   * Wait for the command to complete.
   * @returns Exit code
   */
  async wait(): Promise<number> {
    if (!this._started) {
      throw new CCError('Cmd has not been started', ErrorCode.InvalidArgument);
    }

    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdWait, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.int32();
  }

  /**
   * Run the command and wait for completion.
   * @returns Exit code
   */
  async run(): Promise<number> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdRun, enc.getBytes());
    this._started = true;
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.int32();
  }

  /**
   * Run the command and capture stdout.
   * @returns Standard output as Buffer
   */
  async output(): Promise<Buffer> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdOutput, enc.getBytes());
    this._started = true;
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const data = dec.bytes();
    // Exit code follows, but we ignore it here
    return data;
  }

  /**
   * Run the command and capture stdout + stderr.
   * @returns Combined output as Buffer
   */
  async combinedOutput(): Promise<Buffer> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdCombinedOutput, enc.getBytes());
    this._started = true;
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    const data = dec.bytes();
    // Exit code follows, but we ignore it here
    return data;
  }

  /**
   * Get the exit code (after wait/run).
   */
  async exitCode(): Promise<number> {
    const enc = new Encoder();
    enc.uint64(this.handle);

    const resp = await this.client.call(MsgCmdExitCode, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    return dec.int32();
  }

  /**
   * Kill a started command and release resources.
   * Safe to call on commands that have already completed.
   */
  async kill(): Promise<void> {
    if (this._freed) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    const resp = await this.client.call(MsgCmdKill, enc.getBytes());
    const dec = new Decoder(resp);
    const err = dec.error();
    if (err) throw err;

    this._freed = true;
  }

  /**
   * Free the command if not yet started.
   */
  private async free(): Promise<void> {
    if (this._freed || this._started) return;

    const enc = new Encoder();
    enc.uint64(this._handle);

    try {
      const resp = await this.client.call(MsgCmdFree, enc.getBytes());
      const dec = new Decoder(resp);
      dec.error(); // Ignore errors during cleanup
    } catch {
      // Ignore errors during cleanup
    }

    this._freed = true;
  }

  /**
   * Async dispose support for "await using" syntax.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.free();
  }
}
