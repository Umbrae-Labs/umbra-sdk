import { afterEach, describe, expect, it } from 'vitest'
import type { IncomingHttpHeaders } from 'node:http'
import {
  createDeviceSignature,
  MemoryDeviceCredentialStore,
  MemoryTokenStore,
  newUpsertMutation,
  UmbraClient,
} from '../src'
import { json, readBody, startServer } from './helpers'

const servers: Array<{ close: () => Promise<void> }> = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => server.close()))
})

describe('structured sync', () => {
  it('signs exchange and returns conflicts as result data', async () => {
    const client = await signedClient(async (req, res) => {
      if (req.method === 'POST' && req.url === '/api/v1/client/sync/exchange') {
        const body = await readBody(req)
        const parsed = JSON.parse(body)
        expect(parsed.protocol_version).toBe(1)
        expect(parsed.space).toEqual({ name: 'library' })
        await expectSignature(req, body)
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            accepted: [],
            conflicts: [{ mutation_id: 'm-1', reason: 'base_version_mismatch' }],
            rejected: [],
            changes: [],
            next_cursor: 'cursor-1',
            has_more: false,
            reset_required: false,
          },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })

    const result = await client.sync.exchange({
      space: { name: 'library' },
      mutations: [newUpsertMutation(
        'm-1',
        { namespace: 'lunabox.library', collection: 'games', record_id: 'game-1' },
        1,
        1,
        { name: 'Example' },
      )],
    })
    expect(result.conflicts).toEqual([{ mutation_id: 'm-1', reason: 'base_version_mismatch' }])
  })

  it('signs the canonical snapshot query', async () => {
    const client = await signedClient(async (req, res) => {
      if (req.method === 'GET' && req.url === '/api/v1/client/sync/snapshot?protocol_version=1&space=library&cursor=page-1&limit=25') {
        const body = await readBody(req)
        expect(body).toBe('')
        await expectSignature(req, body)
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: { records: [], exchange_cursor: 'cursor-2', has_more: false },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })

    await expect(client.sync.snapshot({ spaceName: 'library', cursor: 'page-1', limit: 25 })).resolves.toEqual({
      records: [],
      exchange_cursor: 'cursor-2',
      has_more: false,
    })
  })
})

async function signedClient(handler: Parameters<typeof startServer>[0]) {
  const tokenStore = new MemoryTokenStore()
  await tokenStore.save({ accessToken: 'token', tokenType: 'bearer', expiresAt: new Date(Date.now() + 3600_000).toISOString() })
  const server = await startServer(handler)
  servers.push(server)
  return new UmbraClient({
    baseUrl: server.url,
    clientId: 'client',
    tokenStore,
    deviceStore: new MemoryDeviceCredentialStore({ deviceId: 'device-1', deviceSecret: 'device-secret' }),
  })
}

async function expectSignature(
  req: { method?: string | undefined, url?: string | undefined, headers: IncomingHttpHeaders },
  body: string,
) {
  expect(req.headers['x-umbra-device-id']).toBe('device-1')
  const timestamp = Number(req.headers['x-umbra-timestamp'])
  const nonce = String(req.headers['x-umbra-nonce'] || '')
  const expected = await createDeviceSignature({
    method: req.method || '',
    pathWithQuery: req.url || '',
    timestamp,
    nonce,
    body,
    deviceId: 'device-1',
    secret: 'device-secret',
  })
  expect(req.headers['x-umbra-body-sha256']).toBe(expected.bodyHash)
  expect(req.headers['x-umbra-signature']).toBe(`v1=${expected.signature}`)
}
