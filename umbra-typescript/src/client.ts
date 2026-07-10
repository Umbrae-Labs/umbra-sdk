import type { UmbraConfig } from './config'
import type { Session } from './auth'
import { ApiClient } from './api'
import { AuthClient } from './auth'
import { BackupClient } from './backup'
import { normalizeConfig } from './config'
import { DeviceClient } from './device'
import { SyncClient } from './sync'
import { UserClient } from './user'

export class UmbraClient {
  readonly auth: AuthClient
  readonly user: UserClient
  readonly backups: BackupClient
  readonly devices: DeviceClient
  readonly sync: SyncClient
  readonly #deviceRegistration: UmbraConfig['deviceRegistration'] | undefined

  constructor(config: UmbraConfig) {
    const normalized = normalizeConfig(config)
    this.auth = new AuthClient(normalized)
    const api = new ApiClient(normalized, this.auth)
    this.user = new UserClient(api)
    this.backups = new BackupClient(api, normalized)
    this.devices = new DeviceClient(api, normalized.deviceStore, normalized.deviceRegistration)
    this.sync = new SyncClient(api)
    this.#deviceRegistration = normalized.deviceRegistration
  }

  async login(): Promise<Session> {
    const session = await this.auth.login()
    if (this.#deviceRegistration) {
      await this.devices.ensureRegistered(this.#deviceRegistration)
    }
    return session
  }

  async logout(): Promise<void> {
    try {
      await this.auth.logout()
    }
    finally {
      await this.devices.clearCredential()
    }
  }
}
