/**
 * Type definitions and enums for the cc Node.js bindings.
 */

/**
 * Pull policy for image fetching.
 */
export const PullPolicy = {
  IfNotPresent: 0,
  Always: 1,
  Never: 2,
} as const;

export type PullPolicyType = (typeof PullPolicy)[keyof typeof PullPolicy];

/**
 * Seek origin for file operations.
 */
export const SeekWhence = {
  /** Seek from beginning of file */
  Set: 0,
  /** Seek from current position */
  Current: 1,
  /** Seek from end of file */
  End: 2,
} as const;

export type SeekWhenceType = (typeof SeekWhence)[keyof typeof SeekWhence];

/** File open flags (match POSIX) */
export const O_RDONLY = 0x0000;
export const O_WRONLY = 0x0001;
export const O_RDWR = 0x0002;
export const O_APPEND = 0x0008;
export const O_CREATE = 0x0200;
export const O_TRUNC = 0x0400;
export const O_EXCL = 0x0800;

/**
 * Progress information for downloads.
 */
export interface DownloadProgress {
  current: number;
  /** -1 if unknown */
  total: number;
  filename?: string;
  blobIndex: number;
  blobCount: number;
  bytesPerSecond: number;
  /** -1 if unknown */
  etaSeconds: number;
}

/**
 * Progress callback type.
 */
export type ProgressCallback = (progress: DownloadProgress) => void;

/**
 * Options for pulling images.
 */
export interface PullOptions {
  platformOs?: string;
  platformArch?: string;
  username?: string;
  password?: string;
  policy?: PullPolicyType;
}

/**
 * Mount configuration for virtio-fs.
 */
export interface MountConfig {
  tag: string;
  /** undefined for empty writable fs */
  hostPath?: string;
  writable?: boolean;
}

/**
 * Options for creating an instance.
 */
export interface InstanceOptions {
  memoryMb?: number;
  cpus?: number;
  /** 0 for no timeout */
  timeoutSeconds?: number;
  user?: string;
  enableDmesg?: boolean;
  mounts?: MountConfig[];
}

/**
 * File information structure.
 */
export interface FileInfo {
  name: string;
  size: number;
  mode: number;
  modTimeUnix: number;
  isDir: boolean;
  isSymlink: boolean;
}

/**
 * Directory entry.
 */
export interface DirEntry {
  name: string;
  isDir: boolean;
  mode: number;
}

/**
 * OCI image configuration.
 */
export interface ImageConfig {
  architecture?: string;
  env: string[];
  workingDir?: string;
  entrypoint: string[];
  cmd: string[];
  user?: string;
}

/**
 * System capabilities.
 */
export interface Capabilities {
  hypervisorAvailable: boolean;
  /** 0 if unknown */
  maxMemoryMb: number;
  /** 0 if unknown */
  maxCpus: number;
  architecture: string;
}

/**
 * Options for filesystem snapshots.
 */
export interface SnapshotOptions {
  excludes?: string[];
  cacheDir?: string;
}
