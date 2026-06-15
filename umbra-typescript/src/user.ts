import type { ApiClient } from './api'

export interface QuotaInfo {
  quota_bytes: number
  used_bytes: number
  available_bytes: number
}

export interface UserProfile {
  id: number
  username: string
  quota_bytes: number
  used_bytes: number
  available_bytes: number
  storage_end_id?: number
}

export class UserClient {
  readonly #api: ApiClient

  constructor(api: ApiClient) {
    this.#api = api
  }

  quota() {
    return this.#api.get<QuotaInfo>('/user/quota')
  }

  profile() {
    return this.#api.get<UserProfile>('/user/profile')
  }
}

