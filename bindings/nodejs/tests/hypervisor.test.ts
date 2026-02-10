/**
 * Hypervisor detection tests for the cc Node.js bindings.
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  init,
  shutdown,
  supportsHypervisor,
  queryCapabilities,
  guestProtocolVersion,
} from '../src/index.js';

describe('TestLibraryInit', () => {
  it('should init and shutdown', () => {
    init();
    shutdown();
  });

  it('should return guest protocol version', () => {
    init();
    try {
      const version = guestProtocolVersion();
      expect(version).toBe(1);
    } finally {
      shutdown();
    }
  });
});

describe('TestHypervisor', () => {
  beforeEach(() => {
    init();
  });

  afterEach(() => {
    shutdown();
  });

  it('should detect hypervisor support', () => {
    const result = supportsHypervisor();
    // Result is a boolean - either hypervisor is available or not
    expect(typeof result).toBe('boolean');
  });

  it('should query capabilities', async () => {
    const caps = await queryCapabilities();

    expect(typeof caps.hypervisorAvailable).toBe('boolean');
    expect(typeof caps.architecture).toBe('string');
    expect(['x86_64', 'arm64', 'amd64', 'riscv64', 'x64', 'arm']).toContain(caps.architecture);

    console.log(
      `Capabilities: hypervisor=${caps.hypervisorAvailable}, arch=${caps.architecture}`
    );
  });
});
