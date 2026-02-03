/**
 * Exception hierarchy for the cc Node.js bindings.
 *
 * Maps IPC error codes to TypeScript exception types.
 */

/**
 * Error codes matching the IPC protocol.
 */
export const ErrorCode = {
  OK: 0,
  InvalidHandle: 1,
  InvalidArgument: 2,
  NotRunning: 3,
  AlreadyClosed: 4,
  Timeout: 5,
  HypervisorUnavailable: 6,
  IO: 7,
  Network: 8,
  Cancelled: 9,
  Unknown: 99,
} as const;

export type ErrorCodeType = (typeof ErrorCode)[keyof typeof ErrorCode];

/**
 * Base error for all cc errors.
 */
export class CCError extends Error {
  public readonly code: ErrorCodeType;
  public readonly op?: string;
  public readonly path?: string;

  constructor(
    message: string,
    code: ErrorCodeType = ErrorCode.Unknown,
    op?: string,
    path?: string
  ) {
    super(message);
    this.name = 'CCError';
    this.code = code;
    this.op = op;
    this.path = path;
  }

  override toString(): string {
    const parts = [this.message];
    if (this.op) parts.push(`op=${this.op}`);
    if (this.path) parts.push(`path=${this.path}`);
    return parts.join(' ');
  }
}

/**
 * Handle is NULL, zero, or already freed.
 */
export class InvalidHandleError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.InvalidHandle, op, path);
    this.name = 'InvalidHandleError';
  }
}

/**
 * Function argument is invalid.
 */
export class InvalidArgumentError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.InvalidArgument, op, path);
    this.name = 'InvalidArgumentError';
  }
}

/**
 * Instance has terminated.
 */
export class NotRunningError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.NotRunning, op, path);
    this.name = 'NotRunningError';
  }
}

/**
 * Resource was already closed.
 */
export class AlreadyClosedError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.AlreadyClosed, op, path);
    this.name = 'AlreadyClosedError';
  }
}

/**
 * Operation exceeded time limit.
 */
export class TimeoutError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.Timeout, op, path);
    this.name = 'TimeoutError';
  }
}

/**
 * No hypervisor support on this system.
 */
export class HypervisorUnavailableError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.HypervisorUnavailable, op, path);
    this.name = 'HypervisorUnavailableError';
  }
}

/**
 * Filesystem I/O error.
 */
export class IOError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.IO, op, path);
    this.name = 'IOError';
  }
}

/**
 * Network error.
 */
export class NetworkError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.Network, op, path);
    this.name = 'NetworkError';
  }
}

/**
 * Operation was cancelled via cancel token.
 */
export class CancelledError extends CCError {
  constructor(message: string, op?: string, path?: string) {
    super(message, ErrorCode.Cancelled, op, path);
    this.name = 'CancelledError';
  }
}

/**
 * Map from error code to error class constructor.
 */
const ERROR_MAP: Partial<Record<
  ErrorCodeType,
  new (message: string, op?: string, path?: string) => CCError
>> = {
  [ErrorCode.InvalidHandle]: InvalidHandleError,
  [ErrorCode.InvalidArgument]: InvalidArgumentError,
  [ErrorCode.NotRunning]: NotRunningError,
  [ErrorCode.AlreadyClosed]: AlreadyClosedError,
  [ErrorCode.Timeout]: TimeoutError,
  [ErrorCode.HypervisorUnavailable]: HypervisorUnavailableError,
  [ErrorCode.IO]: IOError,
  [ErrorCode.Network]: NetworkError,
  [ErrorCode.Cancelled]: CancelledError,
};

/**
 * Create an appropriate exception from an error code.
 */
export function errorFromCode(
  code: number,
  message: string,
  op?: string,
  path?: string
): CCError {
  const ErrorClass = ERROR_MAP[code as ErrorCodeType];
  if (ErrorClass) {
    return new ErrorClass(message, op, path);
  }
  // Fallback to base CCError for unknown codes
  return new CCError(message, code as ErrorCodeType, op, path);
}
