/**
 * VM tests for the cc Node.js bindings.
 *
 * These tests require:
 * - CC_RUN_VM_TESTS=1 environment variable
 * - cc-helper binary available
 * - Hypervisor support
 */

import { describe, it, expect, beforeEach, afterEach, beforeAll } from 'vitest';
import {
  init,
  shutdown,
  supportsHypervisor,
  OCIClient,
  Instance,
} from '../src/index.js';

const RUN_VM_TESTS = process.env.CC_RUN_VM_TESTS === '1';

describe.skipIf(!RUN_VM_TESTS)('TestWithVM', () => {
  beforeEach(() => {
    init();
  });

  afterEach(() => {
    shutdown();
  });

  it('should pull and create instance', async () => {
    if (!supportsHypervisor()) {
      console.log('Skipping: Hypervisor not available');
      return;
    }

    const client = new OCIClient();
    try {
      // Pull alpine image
      const source = await client.pull('alpine:latest');
      const config = await source.getConfig();
      console.log(`Image architecture: ${config.architecture}`);

      // Create instance
      await using inst = await source.createInstance({
        memoryMb: 256,
        cpus: 1,
      });

      expect(await inst.isRunning()).toBe(true);
      console.log(`Instance ID: ${await inst.id()}`);

      // Close helper after instance
      await source.helper.close();
    } finally {
      client.close();
    }
  });

  it('should execute commands', async () => {
    if (!supportsHypervisor()) {
      console.log('Skipping: Hypervisor not available');
      return;
    }

    const client = new OCIClient();
    try {
      const source = await client.pull('alpine:latest');

      await using inst = await source.createInstance();

      // Run echo command
      const cmd = await inst.command('echo', 'Hello from Node.js!');
      const output = await cmd.output();
      expect(output.toString()).toContain('Hello from Node.js!');

      await source.helper.close();
    } finally {
      client.close();
    }
  });

  it('should perform filesystem operations', async () => {
    if (!supportsHypervisor()) {
      console.log('Skipping: Hypervisor not available');
      return;
    }

    const client = new OCIClient();
    try {
      const source = await client.pull('alpine:latest');

      await using inst = await source.createInstance();

      const testPath = '/tmp/test_file.txt';
      const testData = Buffer.from('Hello, filesystem!');

      // Write file
      await inst.writeFile(testPath, testData);

      // Read file back
      const readData = await inst.readFile(testPath);
      expect(readData.equals(testData)).toBe(true);

      // Stat file
      const info = await inst.stat(testPath);
      expect(info.size).toBe(testData.length);
      expect(info.isDir).toBe(false);

      // Remove file
      await inst.remove(testPath);

      await source.helper.close();
    } finally {
      client.close();
    }
  });

  it('should perform directory operations', async () => {
    if (!supportsHypervisor()) {
      console.log('Skipping: Hypervisor not available');
      return;
    }

    const client = new OCIClient();
    try {
      const source = await client.pull('alpine:latest');

      await using inst = await source.createInstance();

      // Create directory
      await inst.mkdir('/tmp/testdir');

      // List directory
      const entries = await inst.readDir('/tmp');
      const names = entries.map((e) => e.name);
      expect(names).toContain('testdir');

      // Remove directory
      await inst.removeAll('/tmp/testdir');

      await source.helper.close();
    } finally {
      client.close();
    }
  });

  it('should handle file handles', async () => {
    if (!supportsHypervisor()) {
      console.log('Skipping: Hypervisor not available');
      return;
    }

    const client = new OCIClient();
    try {
      const source = await client.pull('alpine:latest');

      await using inst = await source.createInstance();

      // Create and write to file
      {
        await using f = await inst.create('/tmp/handle_test.txt');
        const n = await f.write(Buffer.from('Hello, World!'));
        expect(n).toBe(13);
      }

      // Open and read file
      {
        await using f = await inst.open('/tmp/handle_test.txt');
        const data = await f.read();
        expect(data.toString()).toBe('Hello, World!');

        // Seek and read
        await f.seek(0);
        const data2 = await f.read(5);
        expect(data2.toString()).toBe('Hello');
      }

      await source.helper.close();
    } finally {
      client.close();
    }
  });
});
