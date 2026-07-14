export type ErrorKind =
  | 'invalid_params'
  | 'invalid_token'
  | 'forbidden'
  | 'quota_insufficient'
  | 'file_not_found'
  | 'file_already_exists'
  | 'upload_not_found'
  | 'storage_unavailable'
  | 'network'
  | 'timeout'
  | 'internal'
  | 'auth'

export class UmbraError extends Error {
  readonly kind: ErrorKind
  readonly status?: number
  readonly code?: number
  readonly cause?: unknown

  constructor(message: string, options: {
    kind: ErrorKind
    status?: number
    code?: number
    cause?: unknown
  }) {
    super(message)
    this.name = 'UmbraError'
    this.kind = options.kind
    if (options.status !== undefined) this.status = options.status
    if (options.code !== undefined) this.code = options.code
    if (options.cause !== undefined) this.cause = options.cause
  }

  static invalidInput(message: string) {
    return new UmbraError(message, { kind: 'invalid_params' })
  }

  static auth(message: string) {
    return new UmbraError(message, { kind: 'auth' })
  }

  static api(status: number, code: number | undefined, message: string) {
    const options: {
      kind: ErrorKind
      status: number
      code?: number
    } = {
      kind: kindForCode(status, code),
      status,
    }
    if (code !== undefined) options.code = code
    return new UmbraError(message, options)
  }

  static network(cause: unknown) {
    return new UmbraError(cause instanceof Error ? cause.message : 'network error', {
      kind: 'network',
      cause,
    })
  }
}

export function isInvalidTokenError(error: unknown) {
  return error instanceof UmbraError
    && (error.kind === 'invalid_token' || error.status === 401 || error.code === 1004)
}

export function isDeviceSessionClosedError(error: unknown) {
  return error instanceof UmbraError
    && (error.code === 3004 || error.code === 3005 || error.code === 3006)
}

function kindForCode(status: number, code: number | undefined): ErrorKind {
  switch (code) {
    case 1001:
      return 'invalid_params'
    case 1004:
      return 'invalid_token'
    case 1005:
      return 'forbidden'
    case 2001:
      return 'quota_insufficient'
    case 2002:
      return 'file_not_found'
    case 2010:
      return 'upload_not_found'
    case 2005:
      return 'storage_unavailable'
    case 5000:
      return 'internal'
  }
  if (status === 401) return 'invalid_token'
  if (status === 403) return 'forbidden'
  if (status >= 500) return 'internal'
  return 'invalid_params'
}
