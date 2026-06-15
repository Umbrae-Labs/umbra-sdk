import type { AuthCallback, CallbackReceiver } from './callback'
import type { NormalizedConfig } from './config'
import { UmbraError } from './errors'

export interface TokenSet {
  accessToken: string
  refreshToken?: string
  tokenType: string
  scope?: string
  expiresAt?: string
}

interface HydraTokenResponse {
  access_token?: string
  refresh_token?: string
  token_type?: string
  scope?: string
  expires_in?: number
}

export interface Session {
  token: TokenSet
}

export class AuthClient {
  readonly #config: NormalizedConfig
  #refreshing: Promise<TokenSet> | null = null

  constructor(config: NormalizedConfig) {
    this.#config = config
  }

  async login(): Promise<Session> {
    const { verifier, challenge } = await createPkce()
    const state = randomHex(16)
    const receiver = this.#config.callbackReceiver
    if (!receiver) {
      throw UmbraError.invalidInput('callbackReceiver is required for login in this runtime')
    }

    let redirectUri = this.#config.redirectUri
    if (receiver.prepare) {
      redirectUri = await receiver.prepare(redirectUri)
    }

    const authUrl = this.#buildAuthorizeUrl(redirectUri, state, challenge)
    await this.#config.browserOpener.openUrl(authUrl)

    let callback: AuthCallback
    try {
      callback = await receiver.receive(state)
    }
    finally {
      await receiver.close?.()
    }
    if (callback.error) throw UmbraError.auth(`authorization failed: ${callback.error}`)
    if (!callback.code) throw UmbraError.auth('authorization callback missing code')
    if (callback.state !== state) throw UmbraError.auth('authorization state mismatch')

    const token = await this.#exchangeCode(callback.code, verifier, redirectUri)
    await this.#config.tokenStore.save(token)
    return { token }
  }

  async refresh(): Promise<TokenSet> {
    if (this.#refreshing) return this.#refreshing
    this.#refreshing = this.#refreshInner().finally(() => {
      this.#refreshing = null
    })
    return this.#refreshing
  }

  async logout(): Promise<void> {
    const token = await this.#config.tokenStore.load()
    if (token?.refreshToken) {
      await this.#revoke(token.refreshToken, 'refresh_token').catch(() => {})
    }
    if (token?.accessToken) {
      await this.#revoke(token.accessToken, 'access_token').catch(() => {})
    }
    await this.#config.tokenStore.clear()
  }

  async token(): Promise<TokenSet> {
    const token = await this.#config.tokenStore.load()
    if (!token?.accessToken) throw UmbraError.auth('not authenticated')
    if (!this.#shouldRefresh(token)) return token
    if (!token.refreshToken) return token
    return this.refresh()
  }

  async isAuthenticated() {
    try {
      const token = await this.token()
      return Boolean(token.accessToken)
    }
    catch {
      return false
    }
  }

  #buildAuthorizeUrl(redirectUri: string, state: string, challenge: string) {
    const url = new URL(this.#config.authorizationEndpoint)
    url.searchParams.set('response_type', 'code')
    url.searchParams.set('client_id', this.#config.clientId)
    url.searchParams.set('redirect_uri', redirectUri)
    url.searchParams.set('scope', this.#config.scope)
    url.searchParams.set('state', state)
    url.searchParams.set('code_challenge', challenge)
    url.searchParams.set('code_challenge_method', 'S256')
    return url.toString()
  }

  #exchangeCode(code: string, verifier: string, redirectUri: string) {
    const form = new URLSearchParams()
    form.set('grant_type', 'authorization_code')
    form.set('client_id', this.#config.clientId)
    form.set('code', code)
    form.set('redirect_uri', redirectUri)
    form.set('code_verifier', verifier)
    return this.#tokenRequest(form)
  }

  async #refreshInner() {
    const current = await this.#config.tokenStore.load()
    if (!current?.refreshToken) throw UmbraError.auth('refresh token is not available')
    const form = new URLSearchParams()
    form.set('grant_type', 'refresh_token')
    form.set('client_id', this.#config.clientId)
    form.set('refresh_token', current.refreshToken)
    const next = await this.#tokenRequest(form)
    if (!next.refreshToken) next.refreshToken = current.refreshToken
    await this.#config.tokenStore.save(next)
    return next
  }

  async #tokenRequest(form: URLSearchParams): Promise<TokenSet> {
    const response = await this.#config.fetch(this.#config.tokenEndpoint, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: form,
    })
    const text = await response.text()
    if (!response.ok) {
      throw new UmbraError(`token endpoint returned ${response.status}: ${text.slice(0, 300)}`, {
        kind: 'auth',
        status: response.status,
      })
    }
    const body = JSON.parse(text) as HydraTokenResponse
    if (!body.access_token) throw UmbraError.auth('token response missing access_token')
    return {
      accessToken: body.access_token,
      ...(body.refresh_token ? { refreshToken: body.refresh_token } : {}),
      tokenType: body.token_type || 'bearer',
      ...(body.scope ? { scope: body.scope } : {}),
      ...(body.expires_in && body.expires_in > 0
        ? { expiresAt: new Date(Date.now() + body.expires_in * 1000).toISOString() }
        : {}),
    }
  }

  async #revoke(token: string, tokenTypeHint: string) {
    const form = new URLSearchParams()
    form.set('token', token)
    form.set('token_type_hint', tokenTypeHint)
    form.set('client_id', this.#config.clientId)
    const response = await this.#config.fetch(this.#config.revocationEndpoint, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: form,
    })
    if (!response.ok) throw UmbraError.auth('token revocation failed')
  }

  #shouldRefresh(token: TokenSet) {
    if (!token.expiresAt) return false
    return Date.parse(token.expiresAt) - Date.now() <= this.#config.refreshSkewMs
  }
}

async function createPkce() {
  const verifier = base64Url(randomBytes(48))
  const digest = await cryptoSubtle().digest('SHA-256', new TextEncoder().encode(verifier))
  return {
    verifier,
    challenge: base64Url(new Uint8Array(digest)),
  }
}

function randomHex(size: number) {
  return Array.from(randomBytes(size), b => b.toString(16).padStart(2, '0')).join('')
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

