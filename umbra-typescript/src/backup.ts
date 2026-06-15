import type { ApiClient } from './api'
import type { NormalizedConfig } from './config'
import type { QuotaInfo } from './user'
import { UmbraError } from './errors'
import { normalizeContentHash } from './hash'

export type BackupCategory = 'db' | 'full' | 'game' | 'asset' | 'sync'

export interface BackupAddress {
  category: BackupCategory
  subject?: string
  version: string
}

export function dbBackup(version: string): BackupAddress {
  return { category: 'db', version }
}

export function fullBackup(version: string): BackupAddress {
  return { category: 'full', version }
}

export function gameBackup(subject: string, version: string): BackupAddress {
  return { category: 'game', subject, version }
}

export function assetBackup(subject: string, version: string): BackupAddress {
  return { category: 'asset', subject, version }
}

// syncBackup 与 assetBackup 一样允许同三元组覆盖（LWW），
// 内容协议（manifest / tombstones / 分表记录等）由调用方自行定义。
export function syncBackup(subject: string, version: string): BackupAddress {
  return { category: 'sync', subject, version }
}

const subjectPattern = /^[A-Za-z0-9_-]{1,64}$/
const versionPattern = /^[A-Za-z0-9_\-.:]{1,64}$/

export function validateAddress(address: BackupAddress) {
  if (address.category === 'db' || address.category === 'full') {
    if (address.subject?.trim()) {
      throw UmbraError.invalidInput(`subject must be empty for ${address.category} backups`)
    }
  }
  else if (address.category === 'game' || address.category === 'asset' || address.category === 'sync') {
    if (!address.subject || !subjectPattern.test(address.subject)) {
      throw UmbraError.invalidInput('subject must match ^[A-Za-z0-9_-]{1,64}$')
    }
  }
  else {
    throw UmbraError.invalidInput('invalid backup category')
  }
  if (!versionPattern.test(address.version)) {
    throw UmbraError.invalidInput('version must match ^[A-Za-z0-9_\\-.:]{1,64}$')
  }
}

export interface PresignUploadInput {
  address: BackupAddress
  fileSize: number
  contentType: string
  contentHash?: string
}

export interface BackupTarget {
  backupId?: number
  address?: BackupAddress
}

export interface BackupListFilter {
  category?: BackupCategory
  subject?: string
}

export interface PresignUploadResult {
  backup_id: number
  presigned_url: string
  expires_in: number
}

export interface PresignDownloadResult {
  backup_id: number
  presigned_url: string
  expires_in: number
  size_bytes: number
  etag: string
}

export interface ConfirmUploadResult {
  quota: QuotaInfo
  backup_id: number
  size_bytes: number
  etag: string
}

export interface BatchItemError {
  code: string
  message: string
}

export interface BatchPresignResultItem {
  backup_id?: number
  presigned_url?: string
  expires_in?: number
  error?: BatchItemError
}

export interface BatchConfirmResultItem {
  backup_id?: number
  size_bytes?: number
  etag?: string
  error?: BatchItemError
}

export interface BatchConfirmResult {
  items: BatchConfirmResultItem[]
  total: number
  quota: QuotaInfo
}

export interface BackupRecord {
  backup_id: number
  category: string
  subject: string
  version: string
  size_bytes: number
  content_hash?: string
  etag?: string
  uploaded_at: string
}

export interface NegotiateItem {
  category: BackupCategory
  subject: string
  content_hash: string
}

export interface NegotiateResult {
  category: string
  subject: string
  content_hash: string
  exists: boolean
  backup_id?: number
  version?: string
  size_bytes?: number
}

export interface DeleteResult {
  freed_bytes: number
  available_bytes: number
}

export interface UploadOptions {
  contentType?: string
  contentHash?: string
  computeHash?: boolean
  negotiateByHash?: boolean
  progress?: (done: number, total: number) => void
}

export interface UploadResult {
  backupId: number
  sizeBytes: number
  etag?: string
  quota?: QuotaInfo
  skipped: boolean
}

export interface DownloadOptions {
  progress?: (done: number, total: number) => void
}

export interface DownloadResult {
  backupId: number
  sizeBytes: number
  etag: string
}

interface PresignUploadRequest {
  category: string
  subject: string
  version: string
  file_size: number
  content_type: string
  content_hash?: string
}

interface BackupTargetRequest {
  backup_id?: number
  category?: string
  subject?: string
  version?: string
}

interface ListResponse {
  files: BackupRecord[]
  total: number
}

interface ItemsResponse<T> {
  items: T[]
  total: number
}

export class BackupClient {
  readonly #api: ApiClient
  readonly #config: NormalizedConfig

  constructor(api: ApiClient, config: NormalizedConfig) {
    this.#api = api
    this.#config = config
  }

  presignUpload(input: PresignUploadInput) {
    return this.#api.post<PresignUploadResult>('/client/backup/presign', makePresignRequest(input))
  }

  async presignUploadBatch(items: PresignUploadInput[]) {
    const response = await this.#api.post<ItemsResponse<BatchPresignResultItem>>(
      '/client/backup/presign-batch',
      { items: items.map(makePresignRequest) },
    )
    return response.items
  }

  confirmUpload(target: BackupTarget) {
    return this.#api.post<ConfirmUploadResult>('/client/backup/confirm', makeTargetRequest(target))
  }

  confirmUploadBatch(targets: BackupTarget[]) {
    return this.#api.post<BatchConfirmResult>('/client/backup/confirm-batch', {
      items: targets.map(makeTargetRequest),
    })
  }

  presignDownload(target: BackupTarget) {
    return this.#api.post<PresignDownloadResult>('/client/backup/presign-download', makeTargetRequest(target))
  }

  async list(filter: BackupListFilter = {}) {
    const query = new URLSearchParams()
    if (filter.category) query.set('category', filter.category)
    if (filter.subject) query.set('subject', filter.subject)
    const response = await this.#api.get<ListResponse>('/client/backup/list', query)
    return response.files
  }

  async negotiate(items: NegotiateItem[]) {
    const normalized = items.map(item => ({
      ...item,
      content_hash: normalizeContentHash(item.content_hash, false)!,
    }))
    const response = await this.#api.post<ItemsResponse<NegotiateResult>>('/client/backup/negotiate', {
      items: normalized,
    })
    return response.items
  }

  delete(target: BackupTarget) {
    return this.#api.delete<DeleteResult>('/client/backup/file', makeTargetRequest(target))
  }

  async uploadBlob(address: BackupAddress, blob: Blob, options: UploadOptions = {}): Promise<UploadResult> {
    const contentType = options.contentType || blob.type || 'application/octet-stream'
    let contentHash = normalizeContentHash(options.contentHash, true)
    if (!contentHash && (options.computeHash || options.negotiateByHash)) {
      contentHash = await sha256Blob(blob)
    }

    if (options.negotiateByHash && contentHash) {
      const [result] = await this.negotiate([{
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

    const presign = await this.presignUpload({
      address,
      fileSize: blob.size,
      contentType,
      ...(contentHash ? { contentHash } : {}),
    })
    await this.#putObject(presign.presigned_url, blob, contentType, blob.size, options.progress)
    const confirmed = await this.confirmUpload({ backupId: presign.backup_id })
    return {
      backupId: confirmed.backup_id,
      sizeBytes: confirmed.size_bytes,
      etag: confirmed.etag,
      quota: confirmed.quota,
      skipped: false,
    }
  }

  async downloadBlob(target: BackupTarget, options: DownloadOptions = {}) {
    const presign = await this.presignDownload(target)
    const response = await this.#config.fetch(presign.presigned_url, {
      method: 'GET',
    })
    if (!response.ok) {
      throw new UmbraError('object storage download failed', {
        kind: 'storage_unavailable',
        status: response.status,
      })
    }

    if (!options.progress || !response.body) {
      return {
        result: {
          backupId: presign.backup_id,
          sizeBytes: presign.size_bytes,
          etag: presign.etag,
        },
        blob: await response.blob(),
      }
    }

    const blob = await readResponseBlobWithProgress(response, presign.size_bytes, options.progress)
    return {
      result: {
        backupId: presign.backup_id,
        sizeBytes: presign.size_bytes,
        etag: presign.etag,
      },
      blob,
    }
  }

  async #putObject(url: string, blob: Blob, contentType: string, total: number, progress?: (done: number, total: number) => void) {
    const body = progress ? streamBlobWithProgress(blob, total, progress) : blob
    const response = await this.#config.fetch(url, {
      method: 'PUT',
      headers: {
        'Content-Type': contentType,
      },
      body,
      ...(body instanceof ReadableStream ? { duplex: 'half' } as RequestInit & { duplex: 'half' } : {}),
    })
    if (!response.ok) {
      throw new UmbraError('object storage upload failed', {
        kind: 'storage_unavailable',
        status: response.status,
      })
    }
  }
}

function makePresignRequest(input: PresignUploadInput): PresignUploadRequest {
  validateAddress(input.address)
  if (!Number.isSafeInteger(input.fileSize) || input.fileSize <= 0) {
    throw UmbraError.invalidInput('fileSize must be greater than zero')
  }
  if (!input.contentType.trim()) {
    throw UmbraError.invalidInput('contentType is required')
  }
  const contentHash = normalizeContentHash(input.contentHash, true)
  return {
    category: input.address.category,
    subject: input.address.subject || '',
    version: input.address.version,
    file_size: input.fileSize,
    content_type: input.contentType,
    ...(contentHash ? { content_hash: contentHash } : {}),
  }
}

function makeTargetRequest(target: BackupTarget): BackupTargetRequest {
  if (target.backupId && target.backupId > 0) {
    return { backup_id: target.backupId }
  }
  if (!target.address) {
    throw UmbraError.invalidInput('backupId or address is required')
  }
  validateAddress(target.address)
  return {
    category: target.address.category,
    subject: target.address.subject || '',
    version: target.address.version,
  }
}

async function sha256Blob(blob: Blob) {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', await blob.arrayBuffer())
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
}

function streamBlobWithProgress(blob: Blob, total: number, progress: (done: number, total: number) => void) {
  let done = 0
  return blob.stream().pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      done += chunk.byteLength
      progress(done, total)
      controller.enqueue(chunk)
    },
  }))
}

async function readResponseBlobWithProgress(response: Response, total: number, progress: (done: number, total: number) => void) {
  const reader = response.body!.getReader()
  const chunks: ArrayBuffer[] = []
  let done = 0
  while (true) {
    const { value, done: finished } = await reader.read()
    if (finished) break
    if (value) {
      done += value.byteLength
      progress(done, total)
      const chunk = new Uint8Array(value.byteLength)
      chunk.set(value)
      chunks.push(chunk.buffer)
    }
  }
  return new Blob(chunks, {
    type: response.headers.get('Content-Type') || 'application/octet-stream',
  })
}
