import type { IncomingMessage, Server, ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { BackupAddress, BackupClient, BackupTarget, DownloadOptions, DownloadResult, UploadOptions, UploadResult } from './backup'
import type { AuthCallback, CallbackReceiver } from './callback'
import type { BrowserOpener } from './opener'
import type { TokenSet } from './auth'
import type { DeviceCredential, DeviceCredentialStore, TokenStore } from './store'
import { createServer } from 'node:http'
import { createReadStream, createWriteStream } from 'node:fs'
import { mkdir, readFile, rename, rm, stat, writeFile } from 'node:fs/promises'
import { dirname, extname } from 'node:path'
import { spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { Readable, Transform } from 'node:stream'
import { pipeline } from 'node:stream/promises'
import { UmbraError } from './errors'
import { normalizeContentHash } from './hash'

export class FileTokenStore implements TokenStore {
  readonly #path: string

  constructor(path: string) {
    this.#path = path
  }

  async load(): Promise<TokenSet | null> {
    try {
      const data = await readFile(this.#path, 'utf8')
      if (!data.trim()) return null
      return JSON.parse(data) as TokenSet
    }
    catch (error) {
      if (isNodeError(error) && error.code === 'ENOENT') return null
      throw error
    }
  }

  async save(token: TokenSet): Promise<void> {
    const dir = dirname(this.#path)
    if (dir && dir !== '.') await mkdir(dir, { recursive: true })
    await writeFile(this.#path, `${JSON.stringify(token, null, 2)}\n`, { mode: 0o600 })
  }

  async clear(): Promise<void> {
    await rm(this.#path, { force: true })
  }
}

export class FileDeviceCredentialStore implements DeviceCredentialStore {
  readonly #path: string

  constructor(path: string) {
    this.#path = path
  }

  async load(): Promise<DeviceCredential | null> {
    try {
      const data = await readFile(this.#path, 'utf8')
      if (!data.trim()) return null
      return JSON.parse(data) as DeviceCredential
    }
    catch (error) {
      if (isNodeError(error) && error.code === 'ENOENT') return null
      throw error
    }
  }

  async save(credential: DeviceCredential): Promise<void> {
    const dir = dirname(this.#path)
    if (dir && dir !== '.') await mkdir(dir, { recursive: true })
    await writeFile(this.#path, `${JSON.stringify(credential, null, 2)}\n`, { mode: 0o600 })
  }

  async clear(): Promise<void> {
    await rm(this.#path, { force: true })
  }
}

export class SystemBrowserOpener implements BrowserOpener {
  openUrl(url: string) {
    const command = process.platform === 'win32'
      ? 'rundll32'
      : process.platform === 'darwin'
        ? 'open'
        : 'xdg-open'
    const args = process.platform === 'win32'
      ? ['url.dll,FileProtocolHandler', url]
      : [url]
    const child = spawn(command, args, {
      detached: true,
      stdio: 'ignore',
      windowsHide: true,
    })
    child.unref()
  }
}

export class LoopbackCallbackReceiver implements CallbackReceiver {
  #server: Server | null = null
  #callback: Promise<AuthCallback> | null = null

  async prepare(redirectUri: string) {
    const parsed = new URL(redirectUri)
    if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1') {
      throw UmbraError.invalidInput('redirectUri must be http://127.0.0.1:<port>/<path>')
    }
    const port = parsed.port ? Number(parsed.port) : 0
    const path = parsed.pathname || '/auth/callback'
    const server = createServer((req, res) => this.#handleRequest(req, res, path))
    this.#server = server
    this.#callback = new Promise<AuthCallback>((resolve, reject) => {
      server.once('error', reject)
      server.on('request', (req) => {
        if (!req.url) return
        const requestUrl = new URL(req.url, 'http://127.0.0.1')
        if (requestUrl.pathname !== path) return
        resolve({
          code: requestUrl.searchParams.get('code') || '',
          state: requestUrl.searchParams.get('state') || '',
          ...(requestUrl.searchParams.get('error')
            ? { error: requestUrl.searchParams.get('error')! }
            : {}),
        })
      })
    })
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject)
      server.listen(port, '127.0.0.1', resolve)
    })
    const address = server.address() as AddressInfo
    parsed.host = `127.0.0.1:${address.port}`
    parsed.pathname = path
    parsed.search = ''
    parsed.hash = ''
    return parsed.toString()
  }

  async receive(expectedState: string) {
    if (!this.#callback) {
      throw UmbraError.auth('callback receiver was not prepared')
    }
    const callback = await this.#callback
    if (callback.state !== expectedState) {
      throw UmbraError.auth('authorization state mismatch')
    }
    return callback
  }

  async close() {
    if (!this.#server) return
    const server = this.#server
    this.#server = null
    await new Promise<void>((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve())
    })
  }

  #handleRequest(req: IncomingMessage, res: ServerResponse, path: string) {
    const requestUrl = new URL(req.url || '/', 'http://127.0.0.1')
    if (requestUrl.pathname !== path) {
      res.writeHead(404)
      res.end('not found')
      return
    }
    const body = '<!doctype html><title>Umbra</title><p>Authorization completed. You can close this window.</p>'
    res.writeHead(200, {
      'Content-Type': 'text/html; charset=utf-8',
      'Content-Length': Buffer.byteLength(body),
    })
    res.end(body)
  }
}

export async function uploadFile(
  backups: BackupClient,
  address: BackupAddress,
  path: string,
  options: UploadOptions = {},
): Promise<UploadResult> {
  const info = await stat(path)
  if (!info.isFile()) throw UmbraError.invalidInput('path must point to a file')
  const contentType = options.contentType || contentTypeForPath(path)
  const contentHash = await contentHashForFile(path, options)

  if (options.negotiateByHash && contentHash) {
    const [result] = await backups.negotiate([{
      category: address.category,
      subject: address.subject || '',
      content_hash: contentHash,
    }])
    if (result?.exists) {
      return {
        backupId: result.backup_id || 0,
        sizeBytes: result.size_bytes || 0,
        skipped: true,
      }
    }
  }

  const presign = await backups.presignUpload({
    address,
    fileSize: info.size,
    contentType,
    ...(contentHash ? { contentHash } : {}),
  })
  await putFile(presign.presigned_url, path, info.size, contentType, options.progress)
  const confirmed = await backups.confirmUpload({ backupId: presign.backup_id })
  return {
    backupId: confirmed.backup_id,
    sizeBytes: confirmed.size_bytes,
    etag: confirmed.etag,
    quota: confirmed.quota,
    skipped: false,
  }
}

export async function downloadFile(
  backups: BackupClient,
  target: BackupTarget,
  path: string,
  options: DownloadOptions & { overwrite?: boolean } = {},
): Promise<DownloadResult> {
  if (!options.overwrite) {
    try {
      await stat(path)
      throw UmbraError.invalidInput('target file already exists')
    }
    catch (error) {
      if (!(isNodeError(error) && error.code === 'ENOENT')) throw error
    }
  }
  const dir = dirname(path)
  if (dir && dir !== '.') await mkdir(dir, { recursive: true })
  const presign = await backups.presignDownload(target)
  const response = await fetch(presign.presigned_url, { method: 'GET' })
  if (!response.ok) {
    throw new UmbraError('object storage download failed', {
      kind: 'storage_unavailable',
      status: response.status,
    })
  }

  const temporaryPath = `${path}.umbra-${process.pid}-${Date.now()}.tmp`
  try {
    await writeResponseToFile(response, temporaryPath, presign.size_bytes, options.progress)
    if (options.overwrite) await rm(path, { force: true })
    await rename(temporaryPath, path)
  }
  catch (error) {
    await rm(temporaryPath, { force: true }).catch(() => {})
    throw error
  }

  return {
    backupId: presign.backup_id,
    sizeBytes: presign.size_bytes,
    etag: presign.etag,
  }
}

function contentTypeForPath(path: string) {
  switch (extname(path).toLowerCase()) {
    case '.json':
      return 'application/json'
    case '.txt':
    case '.log':
      return 'text/plain'
    case '.zip':
      return 'application/zip'
    case '.gz':
      return 'application/gzip'
    default:
      return 'application/octet-stream'
  }
}

async function contentHashForFile(path: string, options: UploadOptions) {
  let contentHash = normalizeContentHash(options.contentHash, true)
  if (!contentHash && (options.computeHash || options.negotiateByHash)) {
    contentHash = await sha256File(path)
  }
  return contentHash
}

async function sha256File(path: string) {
  const hash = createHash('sha256')
  await new Promise<void>((resolve, reject) => {
    const stream = createReadStream(path)
    stream.on('data', chunk => hash.update(chunk))
    stream.once('error', reject)
    stream.once('end', resolve)
  })
  return hash.digest('hex')
}

async function putFile(
  url: string,
  path: string,
  total: number,
  contentType: string,
  progress?: (done: number, total: number) => void,
) {
  const source = createReadStream(path)
  const body = progress ? source.pipe(progressTransform(total, progress)) : source
  const response = await fetch(url, {
    method: 'PUT',
    headers: {
      'Content-Type': contentType,
      'Content-Length': String(total),
    },
    body: body as unknown as BodyInit,
    duplex: 'half',
  } as RequestInit & { duplex: 'half' })
  if (!response.ok) {
    throw new UmbraError('object storage upload failed', {
      kind: 'storage_unavailable',
      status: response.status,
    })
  }
}

async function writeResponseToFile(
  response: Response,
  path: string,
  total: number,
  progress?: (done: number, total: number) => void,
) {
  if (!response.body) {
    const bytes = Buffer.from(await response.arrayBuffer())
    await writeFile(path, bytes, { flag: 'wx' })
    progress?.(bytes.byteLength, total)
    return
  }

  const source = Readable.fromWeb(response.body as Parameters<typeof Readable.fromWeb>[0])
  const destination = createWriteStream(path, { flags: 'wx' })
  if (progress) {
    await pipeline(source, progressTransform(total, progress), destination)
    return
  }
  await pipeline(source, destination)
}

function progressTransform(total: number, progress: (done: number, total: number) => void) {
  let done = 0
  return new Transform({
    transform(chunk: Buffer, _encoding, callback) {
      done += chunk.byteLength
      progress(done, total)
      callback(null, chunk)
    },
  })
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error
}
