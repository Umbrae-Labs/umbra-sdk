import type { ApiClient } from './api'
import type { DeviceCredential } from './store'
import type { DeviceCredentialStore } from './store'
import { isDeviceSessionClosedError, UmbraError } from './errors'

export const registrationDeviceId = 'registration'
const registrationTokenPrefix = 'umbra_reg_v1_'

export interface DeviceMetadata {
  name: string
  platform?: string
  os_version?: string
  app_version?: string
  fingerprint?: string
  metadata?: Record<string, unknown>
  readonly __umbraAutoCollectedDeviceMetadata: true
}

export interface DeviceRegistrationInput {
  device: DeviceMetadata
  credentialId?: string
  credentialSecret?: string
  registrationToken?: string
}

interface DeviceRegistrationRequest {
  credential_id?: string
  registration_token?: string
  device: DeviceMetadata
}

export type CollectedDeviceMetadataFields = Omit<DeviceMetadata, '__umbraAutoCollectedDeviceMetadata'>

const autoCollectedDeviceMetadata = new WeakSet<object>()

export function markAutoCollectedDeviceMetadata(metadata: CollectedDeviceMetadataFields): DeviceMetadata {
  autoCollectedDeviceMetadata.add(metadata)
  Object.defineProperty(metadata, '__umbraAutoCollectedDeviceMetadata', {
    value: true,
    enumerable: false,
    configurable: false,
  })
  return metadata as DeviceMetadata
}

function isAutoCollectedDeviceMetadata(metadata: DeviceMetadata) {
  return typeof metadata === 'object' && metadata !== null && autoCollectedDeviceMetadata.has(metadata)
}

export interface ClientDevice {
  id?: number
  device_id: string
  user_id?: number
  tenant_id?: number
  client_id?: string
  distribution_credential_id?: number
  distribution_credential_key?: string
  name?: string
  platform?: string
  os_version?: string
  app_version?: string
  fingerprint?: string
  metadata?: Record<string, unknown>
  status?: number
  created_at?: string
  updated_at?: string
  last_active_at?: string
  rotated_at?: string
  revoked_at?: string
}

export interface DeviceRegistrationResult {
  device: ClientDevice
  device_secret: string
  secret_once: boolean
}

export interface DeviceSignatureInput {
  method: string
  pathWithQuery: string
  timestamp: number
  nonce: string
  body: string
  deviceId: string
  secret: string
}

export interface DeviceSignatureResult {
  canonicalString: string
  bodyHash: string
  signature: string
}

export interface DeviceSignatureHeadersInput {
  method: string
  url: URL
  body: string
  credential: DeviceCredential
  includeDeviceId?: boolean
}

export class DeviceClient {
  readonly #api: ApiClient
  readonly #store: DeviceCredentialStore
  readonly #defaultRegistration: DeviceRegistrationInput | undefined

  constructor(api: ApiClient, store: DeviceCredentialStore, defaultRegistration?: DeviceRegistrationInput) {
    this.#api = api
    this.#store = store
    this.#defaultRegistration = defaultRegistration
  }

  async register(input: DeviceRegistrationInput) {
    const credential = resolveRegistrationCredential(input)
    if (!isAutoCollectedDeviceMetadata(input.device)) {
      throw UmbraError.invalidInput('device metadata must be collected by the SDK')
    }
    const credentialId = input.credentialId?.trim() || ''
    const request: DeviceRegistrationRequest = {
      device: input.device,
      ...(credentialId ? { credential_id: credentialId } : {}),
      ...(input.registrationToken ? { registration_token: input.registrationToken } : {}),
    }
    const response = await this.#api.registerDevice<DeviceRegistrationResult>(request, credential.secret)
    if (!response.device.device_id || !response.device_secret) {
      throw UmbraError.invalidInput('device registration response is missing credentials')
    }
    await this.#store.save({
      deviceId: response.device.device_id,
      deviceSecret: response.device_secret,
    })
    return response
  }

  async ensureRegistered(input: DeviceRegistrationInput) {
    const credential = await this.#store.load()
    if (credential?.deviceId && credential.deviceSecret) return credential
    const response = await this.register(input)
    return {
      deviceId: response.device.device_id,
      deviceSecret: response.device_secret,
    }
  }

  async registerDefault() {
    if (!this.#defaultRegistration) {
      throw UmbraError.invalidInput('device registration is not configured')
    }
    return this.register(this.#defaultRegistration)
  }

  async ensureDefaultRegistered() {
    if (!this.#defaultRegistration) {
      throw UmbraError.invalidInput('device registration is not configured')
    }
    return this.ensureRegistered(this.#defaultRegistration)
  }

  async logout() {
    const credential = await this.#store.load()
    let reportError: unknown
    try {
      if (credential?.deviceId && credential.deviceSecret) {
        await this.#api.post<ClientDevice>('/client/devices/logout', {})
      }
    }
    catch (error) {
      if (!isDeviceSessionClosedError(error)) reportError = error
    }
    let clearError: unknown
    try {
      await this.#store.clear()
    }
    catch (error) {
      clearError = error
    }
    if (reportError && clearError) throw new AggregateError([reportError, clearError], 'device logout failed')
    if (reportError) throw reportError
    if (clearError) throw clearError
  }

  async rotateSecret(deviceId?: string) {
    const stored = await this.#store.load()
    const targetDeviceId = deviceId?.trim() || stored?.deviceId || ''
    if (!targetDeviceId) {
      throw UmbraError.invalidInput('deviceId is required')
    }
    const response = await this.#api.post<DeviceRegistrationResult>(
      `/user/devices/${encodeURIComponent(targetDeviceId)}/rotate-secret`,
      {},
    )
    if (!response.device.device_id || !response.device_secret) {
      throw UmbraError.invalidInput('device secret rotation response is missing credentials')
    }
    await this.#store.save({
      deviceId: response.device.device_id,
      deviceSecret: response.device_secret,
    })
    return response
  }

  loadCredential() {
    return this.#store.load()
  }

  saveCredential(credential: DeviceCredential) {
    return this.#store.save(credential)
  }

  clearCredential() {
    return this.#store.clear()
  }
}

export async function createDeviceSignature(input: DeviceSignatureInput): Promise<DeviceSignatureResult> {
  const bodyHash = await sha256Base64Url(input.body)
  const canonicalString = buildDeviceCanonicalString({
    method: input.method,
    pathWithQuery: input.pathWithQuery,
    timestamp: input.timestamp,
    nonce: input.nonce,
    bodyHash,
    deviceId: input.deviceId,
  })
  const signature = await hmacSha256Base64Url(input.secret, canonicalString)
  return { canonicalString, bodyHash, signature }
}

export function buildDeviceCanonicalString(input: {
  method: string
  pathWithQuery: string
  timestamp: number
  nonce: string
  bodyHash: string
  deviceId: string
}) {
  return [
    'v1',
    input.method.toUpperCase(),
    input.pathWithQuery,
    String(input.timestamp),
    input.nonce,
    input.bodyHash,
    input.deviceId,
  ].join('\n')
}

export async function createDeviceSignatureHeaders(input: DeviceSignatureHeadersInput) {
  const timestamp = Math.floor(Date.now() / 1000)
  const nonce = randomNonce()
  const pathWithQuery = `${input.url.pathname}${input.url.search}`
  const signature = await createDeviceSignature({
    method: input.method,
    pathWithQuery,
    timestamp,
    nonce,
    body: input.body,
    deviceId: input.credential.deviceId,
    secret: input.credential.deviceSecret,
  })

  return {
    ...(input.includeDeviceId === false ? {} : { 'X-Umbra-Device-Id': input.credential.deviceId }),
    'X-Umbra-Timestamp': String(timestamp),
    'X-Umbra-Nonce': nonce,
    'X-Umbra-Body-SHA256': signature.bodyHash,
    'X-Umbra-Signature': `v1=${signature.signature}`,
  }
}

export function parseRegistrationToken(token: string) {
  const trimmed = token.trim()
  if (!trimmed.startsWith(registrationTokenPrefix)) {
    throw UmbraError.invalidInput('registrationToken must start with umbra_reg_v1_')
  }
  const rest = trimmed.slice(registrationTokenPrefix.length)
  const dot = rest.indexOf('.')
  if (dot <= 0 || dot === rest.length - 1) {
    throw UmbraError.invalidInput('registrationToken must include credential id and secret')
  }
  const secret = rest.slice(dot + 1)
  if (secret.includes('.')) {
    throw UmbraError.invalidInput('registrationToken secret must not contain dots')
  }
  return {
    credentialId: rest.slice(0, dot),
    secret,
  }
}

export async function sha256Base64Url(body: string) {
  const digest = await cryptoSubtle().digest('SHA-256', new TextEncoder().encode(body))
  return base64Url(new Uint8Array(digest))
}

function resolveRegistrationCredential(input: DeviceRegistrationInput) {
  const tokenCredential = input.registrationToken ? parseRegistrationToken(input.registrationToken) : null
  const credentialId = input.credentialId?.trim() || tokenCredential?.credentialId || ''
  if (!credentialId) {
    throw UmbraError.invalidInput('credentialId or registrationToken is required')
  }
  if (tokenCredential && input.credentialId?.trim() && input.credentialId.trim() !== tokenCredential.credentialId) {
    throw UmbraError.invalidInput('credentialId does not match registrationToken')
  }
  const secret = input.credentialSecret?.trim() || tokenCredential?.secret || ''
  if (!secret) {
    throw UmbraError.invalidInput('credentialSecret or registrationToken is required')
  }
  return { credentialId, secret }
}

async function hmacSha256Base64Url(secret: string, message: string) {
  const subtle = cryptoSubtle()
  const key = await subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  const signature = await subtle.sign('HMAC', key, new TextEncoder().encode(message))
  return base64Url(new Uint8Array(signature))
}

function randomNonce() {
  return base64Url(randomBytes(24))
}

function randomBytes(size: number) {
  const crypto = cryptoObject()
  const bytes = new Uint8Array(size)
  crypto.getRandomValues(bytes)
  return bytes
}

function cryptoObject() {
  if (!globalThis.crypto?.getRandomValues) {
    throw UmbraError.invalidInput('Web Crypto getRandomValues is not available')
  }
  return globalThis.crypto
}

function cryptoSubtle() {
  if (!globalThis.crypto?.subtle) {
    throw UmbraError.invalidInput('Web Crypto subtle digest is not available')
  }
  return globalThis.crypto.subtle
}

function base64Url(bytes: Uint8Array) {
  let base64: string
  if (typeof btoa === 'function') {
    let binary = ''
    for (const byte of bytes) binary += String.fromCharCode(byte)
    base64 = btoa(binary)
  }
  else {
    base64 = Buffer.from(bytes).toString('base64')
  }
  return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
