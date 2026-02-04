/**
 * cc-helper discovery and process management.
 */

import { spawn, ChildProcess, execSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { CCError, ErrorCode } from './errors.js';
import { IPCClient, connectTo } from './ipc/client.js';

/**
 * Error thrown when cc-helper cannot be found.
 */
export class HelperNotFoundError extends CCError {
  public readonly searchedPaths: string[];

  constructor(searchedPaths: string[]) {
    super(
      `cc-helper not found (searched: ${searchedPaths.join(', ')})`,
      ErrorCode.IO
    );
    this.name = 'HelperNotFoundError';
    this.searchedPaths = searchedPaths;
  }
}

/**
 * Get the platform package name for the current OS/arch.
 */
function getPlatformPackageName(): string | null {
  const platform = os.platform();
  const arch = os.arch();

  // Map Node.js os.arch() to our package names
  const archMap: Record<string, string> = {
    'arm64': 'arm64',
    'x64': 'x64',
  };

  const platformMap: Record<string, string> = {
    'darwin': 'darwin',
    'linux': 'linux',
  };

  const mappedPlatform = platformMap[platform];
  const mappedArch = archMap[arch];

  if (!mappedPlatform || !mappedArch) {
    return null;
  }

  return `@crumblecracker/cc-${mappedPlatform}-${mappedArch}`;
}

/**
 * Try to find cc-helper in the npm platform package.
 */
function findHelperInPackage(): string | null {
  const packageName = getPlatformPackageName();
  if (!packageName) {
    return null;
  }

  try {
    // Use require.resolve to find the package, then navigate to the bin directory
    // We resolve the package.json to get the package root
    const packageJsonPath = require.resolve(`${packageName}/package.json`);
    const packageDir = path.dirname(packageJsonPath);
    const helperName = os.platform() === 'win32' ? 'cc-helper.exe' : 'cc-helper';
    const helperPath = path.join(packageDir, 'bin', helperName);

    if (fs.existsSync(helperPath)) {
      return helperPath;
    }
  } catch {
    // Package not installed
  }

  return null;
}

/**
 * Find the cc-helper binary.
 *
 * Search order:
 * 1. CC_HELPER_PATH env
 * 2. Bundled npm platform package (@crumblecracker/cc-{platform}-{arch})
 * 3. Adjacent to lib (same dir as bindings)
 * 4. Platform dirs: ~/Library/Application Support/cc/bin/cc-helper (macOS)
 * 5. PATH
 */
export function findHelper(libPath?: string): { path: string; searched: string[] } | { path: null; searched: string[] } {
  const searched: string[] = [];

  // 1. CC_HELPER_PATH environment variable
  const envPath = process.env.CC_HELPER_PATH;
  if (envPath) {
    searched.push(envPath);
    if (fs.existsSync(envPath)) {
      return { path: envPath, searched: [] };
    }
  }

  // 2. Bundled npm platform package
  const packagePath = findHelperInPackage();
  if (packagePath) {
    return { path: packagePath, searched: [] };
  }
  const packageName = getPlatformPackageName();
  if (packageName) {
    searched.push(`${packageName}/bin/cc-helper`);
  }

  // 3. Adjacent to lib path
  if (libPath) {
    const dir = path.dirname(libPath);
    const helperPath = path.join(dir, 'cc-helper');
    searched.push(helperPath);
    if (fs.existsSync(helperPath)) {
      return { path: helperPath, searched: [] };
    }
  }

  // 3. Platform-specific user directory
  const platform = os.platform();
  const homeDir = os.homedir();

  if (platform === 'darwin') {
    const helperPath = path.join(
      homeDir,
      'Library',
      'Application Support',
      'cc',
      'bin',
      'cc-helper'
    );
    searched.push(helperPath);
    if (fs.existsSync(helperPath)) {
      return { path: helperPath, searched: [] };
    }
  } else if (platform === 'linux') {
    const helperPath = path.join(
      homeDir,
      '.local',
      'share',
      'cc',
      'bin',
      'cc-helper'
    );
    searched.push(helperPath);
    if (fs.existsSync(helperPath)) {
      return { path: helperPath, searched: [] };
    }
  } else if (platform === 'win32') {
    const appData = process.env.LOCALAPPDATA;
    if (appData) {
      const helperPath = path.join(appData, 'cc', 'bin', 'cc-helper.exe');
      searched.push(helperPath);
      if (fs.existsSync(helperPath)) {
        return { path: helperPath, searched: [] };
      }
    }
  }

  // 4. System PATH
  try {
    const whichCmd = platform === 'win32' ? 'where' : 'which';
    const result = execSync(`${whichCmd} cc-helper`, { encoding: 'utf-8' }).trim();
    if (result) {
      return { path: result.split('\n')[0], searched: [] };
    }
  } catch {
    // Not found in PATH
  }
  searched.push('$PATH');

  return { path: null, searched };
}

/**
 * Managed cc-helper process with client connection.
 */
export class HelperProcess {
  private process: ChildProcess;
  private _client: IPCClient;
  private socketPath: string;

  constructor(process: ChildProcess, client: IPCClient, socketPath: string) {
    this.process = process;
    this._client = client;
    this.socketPath = socketPath;
  }

  /**
   * Get the IPC client.
   */
  get client(): IPCClient {
    return this._client;
  }

  /**
   * Close the helper process and client connection.
   */
  async close(): Promise<void> {
    this._client.close();

    // Give the helper a chance to exit gracefully
    const exitPromise = new Promise<void>((resolve) => {
      const timeout = setTimeout(() => {
        this.process.kill('SIGKILL');
        resolve();
      }, 2000);

      this.process.on('exit', () => {
        clearTimeout(timeout);
        resolve();
      });
    });

    this.process.kill('SIGTERM');
    await exitPromise;

    // Clean up socket file
    try {
      fs.unlinkSync(this.socketPath);
    } catch {
      // Ignore errors
    }
  }
}

/**
 * Wait for a file to exist on the filesystem.
 */
async function waitForFile(filePath: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(filePath)) {
      return true;
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  return false;
}

/**
 * Spawn a new cc-helper process and connect to it.
 */
export async function spawnHelper(libPath?: string): Promise<HelperProcess> {
  const result = findHelper(libPath);
  if (!result.path) {
    throw new HelperNotFoundError(result.searched);
  }

  // Create a temporary socket path
  const tmpDir = os.tmpdir();
  const socketPath = path.join(
    tmpDir,
    `cc-helper-${process.pid}-${Date.now()}.sock`
  );

  // Start the helper process
  const helperProcess = spawn(result.path, ['-socket', socketPath], {
    stdio: ['ignore', 'pipe', 'inherit'],
  });

  // First, wait for the socket file to exist on the filesystem
  // This is important for Bun compatibility - Bun may try to connect
  // before the socket file is created, causing ENOENT errors
  const socketExists = await waitForFile(socketPath, 10000);
  if (!socketExists) {
    helperProcess.kill();
    throw new CCError(
      'Timeout waiting for cc-helper socket file to be created',
      ErrorCode.IO
    );
  }

  // Now try to connect (with retries for the socket to be ready)
  const deadline = Date.now() + 5000; // 5 second connection timeout
  let lastErr: Error | null = null;

  while (Date.now() < deadline) {
    try {
      const client = await connectTo(socketPath);
      return new HelperProcess(helperProcess, client, socketPath);
    } catch (err) {
      lastErr = err as Error;
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  }

  // Cleanup on failure
  helperProcess.kill();
  try {
    fs.unlinkSync(socketPath);
  } catch {
    // Ignore
  }

  throw new CCError(
    `Failed to connect to cc-helper: ${lastErr?.message ?? 'timeout'}`,
    ErrorCode.IO
  );
}
