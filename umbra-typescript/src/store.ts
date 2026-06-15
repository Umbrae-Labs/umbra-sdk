import type { TokenSet } from './auth'

export interface TokenStore {
  load(): Promise<TokenSet | null>
  save(token: TokenSet): Promise<void>
  clear(): Promise<void>
}

export interface DeviceCredential {
  deviceId: string
  deviceSecret: string
}

export interface DeviceCredentialStore {
  load(): Promise<DeviceCredential | null>
  save(credential: DeviceCredential): Promise<void>
  clear(): Promise<void>
}

export class MemoryTokenStore implements TokenStore {
  #token: TokenSet | null = null

  async load() {
    return this.#token ? { ...this.#token } : null
  }

  async save(token: TokenSet) {
    this.#token = { ...token }
  }

  async clear() {
    this.#token = null
  }
}

export class MemoryDeviceCredentialStore implements DeviceCredentialStore {
  #credential: DeviceCredential | null = null

  constructor(credential?: DeviceCredential) {
    if (credential) this.#credential = { ...credential }
  }

  async load() {
    return this.#credential ? { ...this.#credential } : null
  }

  async save(credential: DeviceCredential) {
    this.#credential = { ...credential }
  }

  async clear() {
    this.#credential = null
  }
}
