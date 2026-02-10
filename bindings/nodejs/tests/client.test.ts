/**
 * OCI client tests for the cc Node.js bindings.
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { init, shutdown, OCIClient, CancelToken } from '../src/index.js';

describe('TestCancelToken', () => {
  beforeEach(() => {
    init();
  });

  afterEach(() => {
    shutdown();
  });

  it('should handle cancel token lifecycle', () => {
    const token = new CancelToken();
    expect(token.isCancelled).toBe(false);

    token.cancel();
    expect(token.isCancelled).toBe(true);

    token.close();
  });
});

describe('TestOCIClient', () => {
  beforeEach(() => {
    init();
  });

  afterEach(() => {
    shutdown();
  });

  it('should create client', async () => {
    const client = new OCIClient();
    expect(client.closed).toBe(false);

    // Note: cache_dir may be empty for the base client
    const cacheDir = client.cacheDir;
    console.log(`Cache dir: ${cacheDir}`);

    client.close();
    expect(client.closed).toBe(true);
  });

  it('should create client with custom cache', async () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'cc-test-'));
    const cacheDir = path.join(tmpDir, 'cache');
    fs.mkdirSync(cacheDir, { recursive: true });

    try {
      const client = new OCIClient(cacheDir);
      // The cache dir should match what we passed
      expect(client.cacheDir).toBe(cacheDir);

      client.close();
    } finally {
      // Cleanup
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
  });

  it('should throw when using closed client', async () => {
    const client = new OCIClient();
    client.close();

    await expect(client.pull('alpine:latest')).rejects.toThrow('OCIClient is closed');
  });
});
