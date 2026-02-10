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
      const inst = await source.createInstance({
        memoryMb: 256,
        cpus: 1,
      });

      try {
        expect(await inst.isRunning()).toBe(true);
        console.log(`Instance ID: ${await inst.id()}`);
      } finally {
        await inst.close();
      }

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

      const inst = await source.createInstance();
      try {
        // Run echo command
        const cmd = await inst.command('echo', 'Hello from Node.js!');
        const output = await cmd.output();
        expect(output.toString()).toContain('Hello from Node.js!');
      } finally {
        await inst.close();
      }

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

      const inst = await source.createInstance();
      try {
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
      } finally {
        await inst.close();
      }

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

      const inst = await source.createInstance();
      try {
        // Create directory
        await inst.mkdir('/tmp/testdir');

        // List directory
        const entries = await inst.readDir('/tmp');
        const names = entries.map((e) => e.name);
        expect(names).toContain('testdir');

        // Remove directory
        await inst.removeAll('/tmp/testdir');
      } finally {
        await inst.close();
      }

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

      const inst = await source.createInstance();
      try {
        // Create and write to file
        const fw = await inst.create('/tmp/handle_test.txt');
        try {
          const n = await fw.write(Buffer.from('Hello, World!'));
          expect(n).toBe(13);
        } finally {
          await fw.close();
        }

        // Open and read file
        const fr = await inst.open('/tmp/handle_test.txt');
        try {
          const data = await fr.read();
          expect(data.toString()).toBe('Hello, World!');

          // Seek and read
          await fr.seek(0);
          const data2 = await fr.read(5);
          expect(data2.toString()).toBe('Hello');
        } finally {
          await fr.close();
        }
      } finally {
        await inst.close();
      }

      await source.helper.close();
    } finally {
      client.close();
    }
  });
});
