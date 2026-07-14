import type { AuthClient } from './auth'
import type { NormalizedConfig } from './config'
import { joinUrl } from './config'
import { createDeviceSignatureHeaders, registrationDeviceId } from './device'
import { isInvalidTokenError, UmbraError } from './errors'

interface Envelope<T> {
  code: number
  msg: string
  data: T | null
}

export class ApiClient {
  readonly #config: NormalizedConfig
  readonly #auth: AuthClient

  constructor(config: NormalizedConfig, auth: AuthClient) {
    this.#config = config
    this.#auth = auth
  }

  get<T>(path: string, query?: URLSearchParams) {
    const next = query && query.size > 0 ? `${path}?${query.toString()}` : path
    return this.#send<T>('GET', next, undefined, true)
  }

  post<T>(path: string, body: unknown) {
    return this.#send<T>('POST', path, body, true)
  }

  delete<T>(path: string, body: unknown) {
    return this.#send<T>('DELETE', path, body, true)
  }

  registerDevice<T>(body: unknown, registrationSecret: string) {
    return this.#send<T>('POST', '/client/devices/register', body, true, {
      deviceId: registrationDeviceId,
      deviceSecret: registrationSecret,
      includeDeviceId: false,
    })
  }

  async #send<T>(
    method: string,
    path: string,
    body: unknown,
    retryAuth: boolean,
    explicitCredential?: { deviceId: string, deviceSecret: string, includeDeviceId: boolean },
  ): Promise<T> {
    const token = await this.#auth.token()
    const bodyText = body === undefined ? '' : JSON.stringify(body)
    const url = new URL(joinUrl(this.#config.apiBaseUrl, path))
    const headers: Record<string, string> = {
      Accept: 'application/json',
      Authorization: `Bearer ${token.accessToken}`,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    }
    const credential = explicitCredential ?? await this.#credentialForPath(path)
    if (credential) {
      const signatureInput = {
        method,
        url,
        body: bodyText,
        credential,
      }
      Object.assign(headers, await createDeviceSignatureHeaders(
        explicitCredential
          ? { ...signatureInput, includeDeviceId: explicitCredential.includeDeviceId }
          : signatureInput,
      ))
    }

    const init: RequestInit = {
      method,
      headers,
      ...(body === undefined ? {} : { body: bodyText }),
    }

    let response: Response
    try {
      response = await this.#config.fetch(url.toString(), init)
    }
    catch (error) {
      throw UmbraError.network(error)
    }

    try {
      return await decodeEnvelope<T>(response)
    }
    catch (error) {
      if (retryAuth && isInvalidTokenError(error)) {
        await this.#auth.refresh()
        return this.#send<T>(method, path, body, false, explicitCredential)
      }
      throw error
    }
  }

  async #credentialForPath(path: string) {
    if (!path.startsWith('/client/backup/')
      && !path.startsWith('/client/sync/')
      && path !== '/client/devices/logout') return null
    const credential = await this.#config.deviceStore.load()
    if (!credential?.deviceId || !credential.deviceSecret) {
      throw UmbraError.auth('device credentials are required for protected client requests')
    }
    return credential
  }
}

async function decodeEnvelope<T>(response: Response): Promise<T> {
  const text = await response.text()
  let envelope: Envelope<T>
  try {
    envelope = JSON.parse(text) as Envelope<T>
  }
  catch (error) {
    if (!response.ok) {
      throw UmbraError.api(response.status, undefined, `request failed with status ${response.status}`)
    }
    throw error
  }
  if (!response.ok || envelope.code !== 0) {
    throw UmbraError.api(response.status, envelope.code, envelope.msg)
  }
  if (envelope.data === null || envelope.data === undefined) {
    throw UmbraError.invalidInput('response data is missing')
  }
  return envelope.data
}
