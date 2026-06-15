import { afterEach, describe, expect, it } from 'vitest'
import { gameBackup, MemoryDeviceCredentialStore, MemoryTokenStore, UmbraClient } from '../src'
import { json, readBody, startServer } from './helpers'

const servers: Array<{ close: () => Promise<void> }> = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => server.close()))
})

describe('backup client', () => {
  it('uploads a blob through presign PUT and confirm', async () => {
    let putBody = ''
    const object = await startServer(async (req, res) => {
      if (req.method === 'PUT' && req.url === '/object') {
        putBody = await readBody(req)
        res.writeHead(200)
        res.end()
        return
      }
      res.writeHead(404)
      res.end()
    })
    servers.push(object)

    const api = await startServer(async (req, res) => {
      if (req.method === 'POST' && req.url === '/api/v1/client/backup/presign') {
        const body = JSON.parse(await readBody(req)) as Record<string, unknown>
        expect(body).toMatchObject({
          category: 'game',
          subject: 'mc',
          version: 'v1',
          file_size: 5,
          content_type: 'text/plain',
        })
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            backup_id: 42,
            presigned_url: `${object.url}/object`,
            expires_in: 3600,
          },
        })
        return
      }
      if (req.method === 'POST' && req.url === '/api/v1/client/backup/confirm') {
        const body = JSON.parse(await readBody(req)) as Record<string, unknown>
        expect(body).toEqual({ backup_id: 42 })
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            backup_id: 42,
            size_bytes: 5,
            etag: 'etag',
            quota: { quota_bytes: 10, used_bytes: 5, available_bytes: 5 },
          },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(api)

    const store = new MemoryTokenStore()
    await store.save({
      accessToken: 'token',
      tokenType: 'bearer',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    })
    const client = new UmbraClient({
      baseUrl: api.url,
      clientId: 'client',
      tokenStore: store,
      deviceStore: new MemoryDeviceCredentialStore({
        deviceId: 'dev_test',
        deviceSecret: 'device-secret',
      }),
    })

    const result = await client.backups.uploadBlob(
      gameBackup('mc', 'v1'),
      new Blob(['hello'], { type: 'text/plain' }),
    )

    expect(result.backupId).toBe(42)
    expect(result.sizeBytes).toBe(5)
    expect(putBody).toBe('hello')
  })
})
