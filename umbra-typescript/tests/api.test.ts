import { afterEach, describe, expect, it } from 'vitest'
import { MemoryTokenStore, UmbraClient } from '../src'
import { json, startServer } from './helpers'

const servers: Array<{ close: () => Promise<void> }> = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => server.close()))
})

describe('api client', () => {
  it('refreshes and retries after invalid token', async () => {
    let quotaCalls = 0
    let tokenCalls = 0
    const server = await startServer((req, res) => {
      if (req.url === '/api/v1/user/quota') {
        quotaCalls++
        if (req.headers.authorization === 'Bearer old-token') {
          json(res, 401, { code: 1004, msg: 'Token invalid', data: null })
          return
        }
        expect(req.headers.authorization).toBe('Bearer new-token')
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: { quota_bytes: 10, used_bytes: 3, available_bytes: 7 },
        })
        return
      }
      if (req.url === '/oauth2/token') {
        tokenCalls++
        json(res, 200, {
          access_token: 'new-token',
          refresh_token: 'refresh-token',
          token_type: 'bearer',
          expires_in: 3600,
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(server)

    const store = new MemoryTokenStore()
    await store.save({
      accessToken: 'old-token',
      refreshToken: 'refresh-token',
      tokenType: 'bearer',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    })
    const client = new UmbraClient({
      baseUrl: server.url,
      clientId: 'client',
      tokenStore: store,
    })

    const quota = await client.user.quota()
    expect(quota.available_bytes).toBe(7)
    expect(quotaCalls).toBe(2)
    expect(tokenCalls).toBe(1)
  })
})

