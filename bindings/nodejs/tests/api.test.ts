/**
 * API version tests for the cc Node.js bindings.
 */

import { describe, it, expect } from 'vitest';
import {
  apiVersion,
  apiVersionCompatible,
  VERSION,
} from '../src/index.js';

describe('TestAPIVersion', () => {
  it('should return expected version string', () => {
    const version = apiVersion();
    expect(version).toBe('0.1.0');
    expect(version).toBe(VERSION);
  });

  it('should check version compatibility correctly', () => {
    // Same version should be compatible
    expect(apiVersionCompatible(0, 1)).toBe(true);

    // Lower minor version should be compatible
    expect(apiVersionCompatible(0, 0)).toBe(true);

    // Higher major version should not be compatible
    expect(apiVersionCompatible(1, 0)).toBe(false);

    // Higher minor version should not be compatible
    expect(apiVersionCompatible(0, 99)).toBe(false);
  });
});
