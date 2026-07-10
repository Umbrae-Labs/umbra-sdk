import type { ApiClient } from './api'

export const syncProtocolVersion = 1

export type JsonPrimitive = string | number | boolean | null
export type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue }

export interface SyncSpace {
  name: string
}

export interface SyncRecordKey {
  namespace: string
  collection: string
  record_id: string
}

export type SyncOperation = 'upsert' | 'delete'

export interface SyncMutation<T extends JsonValue = JsonValue> {
  mutation_id: string
  key: SyncRecordKey
  schema_version: number
  base_version: number
  operation: SyncOperation
  payload?: T
}

export interface SyncExchangeInput<T extends JsonValue = JsonValue> {
  protocol_version?: number
  space: SyncSpace
  cursor?: string
  mutations?: SyncMutation<T>[]
  pull_limit?: number
}

export interface SyncAcceptedMutation {
  mutation_id: string
  record_version: number
  cursor: string
}

export interface SyncChange<T extends JsonValue = JsonValue> {
  cursor: string
  key: SyncRecordKey
  schema_version: number
  record_version: number
  operation: SyncOperation
  payload?: T
  writer_device_id: string
}

export interface SyncConflict<T extends JsonValue = JsonValue> {
  mutation_id: string
  reason: string
  current?: SyncChange<T>
}

export interface SyncRejectedMutation {
  mutation_id: string
  reason: string
}

export interface SyncExchangeResult<T extends JsonValue = JsonValue> {
  accepted: SyncAcceptedMutation[]
  conflicts: SyncConflict<T>[]
  rejected: SyncRejectedMutation[]
  changes: SyncChange<T>[]
  next_cursor: string
  has_more: boolean
  reset_required: boolean
  reason?: string
  snapshot_cursor?: string
}

export interface SyncSnapshotInput {
  protocolVersion?: number
  spaceName: string
  cursor?: string
  limit?: number
}

export interface SyncSnapshotPage<T extends JsonValue = JsonValue> {
  records: SyncChange<T>[]
  next_cursor?: string
  exchange_cursor: string
  has_more: boolean
}

export function newUpsertMutation<T extends JsonValue>(
  mutationId: string,
  key: SyncRecordKey,
  schemaVersion: number,
  baseVersion: number,
  payload: T,
): SyncMutation<T> {
  return {
    mutation_id: mutationId,
    key,
    schema_version: schemaVersion,
    base_version: baseVersion,
    operation: 'upsert',
    payload,
  }
}

export function newDeleteMutation(
  mutationId: string,
  key: SyncRecordKey,
  schemaVersion: number,
  baseVersion: number,
): SyncMutation {
  return {
    mutation_id: mutationId,
    key,
    schema_version: schemaVersion,
    base_version: baseVersion,
    operation: 'delete',
  }
}

export class SyncClient {
  readonly #api: ApiClient

  constructor(api: ApiClient) {
    this.#api = api
  }

  exchange<T extends JsonValue = JsonValue>(input: SyncExchangeInput<T>) {
    return this.#api.post<SyncExchangeResult<T>>('/client/sync/exchange', {
      protocol_version: input.protocol_version || syncProtocolVersion,
      space: input.space,
      cursor: input.cursor || '',
      mutations: input.mutations || [],
      ...(input.pull_limit ? { pull_limit: input.pull_limit } : {}),
    })
  }

  snapshot<T extends JsonValue = JsonValue>(input: SyncSnapshotInput) {
    const query = new URLSearchParams()
    query.set('protocol_version', String(input.protocolVersion || syncProtocolVersion))
    query.set('space', input.spaceName)
    if (input.cursor) query.set('cursor', input.cursor)
    if (input.limit) query.set('limit', String(input.limit))
    return this.#api.get<SyncSnapshotPage<T>>('/client/sync/snapshot', query)
  }
}
