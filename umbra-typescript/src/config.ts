import type { BrowserOpener } from './opener'
import type { CallbackReceiver } from './callback'
import type { DeviceRegistrationInput } from './device'
import type { DeviceCredentialStore, TokenStore } from './store'
import { MemoryDeviceCredentialStore, MemoryTokenStore } from './store'
import { WebBrowserOpener } from './opener'
import { UmbraError } from './errors'

const defaultScope = 'openid offline_access'
const defaultRefreshSkewMs = 60_000

export interface UmbraConfig {
  baseUrl: string
  clientId: string
  redirectUri?: string
  scope?: string

  apiBaseUrl?: string
  authorizationEndpoint?: string
  tokenEndpoint?: string
  revocationEndpoint?: string

  fetch?: typeof globalThis.fetch
  tokenStore?: TokenStore
  deviceStore?: DeviceCredentialStore
  deviceRegistration?: DeviceRegistrationInput
  browserOpener?: BrowserOpener
  callbackReceiver?: CallbackReceiver
  refreshSkewMs?: number
}

export interface NormalizedConfig {
  baseUrl: string
  clientId: string
  redirectUri: string
  scope: string
  apiBaseUrl: string
  authorizationEndpoint: string
  tokenEndpoint: string
  revocationEndpoint: string
  fetch: typeof globalThis.fetch
  tokenStore: TokenStore
  deviceStore: DeviceCredentialStore
  deviceRegistration?: DeviceRegistrationInput
  browserOpener: BrowserOpener
  callbackReceiver?: CallbackReceiver
  refreshSkewMs: number
}

export function normalizeConfig(config: UmbraConfig): NormalizedConfig {
  const baseUrl = trimRightSlash(config.baseUrl)
  assertAbsoluteUrl(baseUrl, 'baseUrl')
  const clientId = config.clientId.trim()
  if (!clientId) throw UmbraError.invalidInput('clientId is required')

  const fetchImpl = config.fetch ?? globalThis.fetch
  if (typeof fetchImpl !== 'function') {
    throw UmbraError.invalidInput('fetch is not available; provide config.fetch')
  }

  const normalized: NormalizedConfig = {
    baseUrl,
    clientId,
    redirectUri: config.redirectUri?.trim() || 'http://127.0.0.1:0/auth/callback',
    scope: config.scope?.trim() || defaultScope,
    apiBaseUrl: trimRightSlash(config.apiBaseUrl ?? joinUrl(baseUrl, '/api/v1')),
    authorizationEndpoint: config.authorizationEndpoint?.trim() || joinUrl(baseUrl, '/oauth2/auth'),
    tokenEndpoint: config.tokenEndpoint?.trim() || joinUrl(baseUrl, '/oauth2/token'),
    revocationEndpoint: config.revocationEndpoint?.trim() || joinUrl(baseUrl, '/oauth2/revoke'),
    fetch: fetchImpl.bind(globalThis),
    tokenStore: config.tokenStore ?? new MemoryTokenStore(),
    deviceStore: config.deviceStore ?? new MemoryDeviceCredentialStore(),
    ...(config.deviceRegistration ? { deviceRegistration: config.deviceRegistration } : {}),
    browserOpener: config.browserOpener ?? new WebBrowserOpener(),
    refreshSkewMs: config.refreshSkewMs && config.refreshSkewMs > 0
      ? config.refreshSkewMs
      : defaultRefreshSkewMs,
  }
  if (config.callbackReceiver) {
    normalized.callbackReceiver = config.callbackReceiver
  }

  assertAbsoluteUrl(normalized.apiBaseUrl, 'apiBaseUrl')
  assertAbsoluteUrl(normalized.authorizationEndpoint, 'authorizationEndpoint')
  assertAbsoluteUrl(normalized.tokenEndpoint, 'tokenEndpoint')
  assertAbsoluteUrl(normalized.revocationEndpoint, 'revocationEndpoint')
  return normalized
}

export function joinUrl(base: string, path: string) {
  return `${base.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`
}

function trimRightSlash(value: string) {
  return value.trim().replace(/\/+$/, '')
}

function assertAbsoluteUrl(value: string, name: string) {
  try {
    const parsed = new URL(value)
    if (!parsed.protocol || !parsed.host) throw new Error('not absolute')
  }
  catch {
    throw UmbraError.invalidInput(`${name} must be an absolute URL`)
  }
}
