import { afterEach, describe, expect, it } from 'vitest'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { gameBackup, MemoryDeviceCredentialStore, MemoryTokenStore, UmbraClient } from '../src'
import { downloadFile, uploadFile } from '../src/node'
import { json, readBody, startServer } from './helpers'

const servers: Array<{ close: () => Promise<void> }> = []
const tempDirs: string[] = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => server.close()))
  await Promise.all(tempDirs.splice(0).map(dir => rm(dir, { recursive: true, force: true })))
})

describe('node helpers', () => {
  it('uploads a file through a streaming presigned PUT', async () => {
    const tempDir = await mkdtemp(join(tmpdir(), 'umbra-sdk-'))
    tempDirs.push(tempDir)
    const path = join(tempDir, 'world.txt')
    await writeFile(path, 'hello')

    let putBody = ''
    let contentLength = ''
    const object = await startServer(async (req, res) => {
      if (req.method === 'PUT' && req.url === '/object') {
        contentLength = String(req.headers['content-length'] || '')
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

    const client = new UmbraClient({
      baseUrl: api.url,
      clientId: 'client',
      tokenStore: await authenticatedStore(),
      deviceStore: authenticatedDeviceStore(),
    })

    const result = await uploadFile(client.backups, gameBackup('mc', 'v1'), path)

    expect(result.backupId).toBe(42)
    expect(putBody).toBe('hello')
    expect(contentLength).toBe('5')
  })

  it('downloads a presigned object to a file', async () => {
    const tempDir = await mkdtemp(join(tmpdir(), 'umbra-sdk-'))
    tempDirs.push(tempDir)
    const path = join(tempDir, 'world.txt')

    const object = await startServer((req, res) => {
      if (req.method === 'GET' && req.url === '/object') {
        res.writeHead(200, {
          'Content-Type': 'text/plain',
          'Content-Length': '5',
        })
        res.end('hello')
        return
      }
      res.writeHead(404)
      res.end()
    })
    servers.push(object)

    const api = await startServer(async (req, res) => {
      if (req.method === 'POST' && req.url === '/api/v1/client/backup/presign-download') {
        expect(JSON.parse(await readBody(req))).toEqual({ backup_id: 42 })
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            backup_id: 42,
            presigned_url: `${object.url}/object`,
            expires_in: 3600,
            size_bytes: 5,
            etag: 'etag',
          },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(api)

    const client = new UmbraClient({
      baseUrl: api.url,
      clientId: 'client',
      tokenStore: await authenticatedStore(),
      deviceStore: authenticatedDeviceStore(),
    })

    const result = await downloadFile(client.backups, { backupId: 42 }, path)

    expect(result.backupId).toBe(42)
    expect(await readFile(path, 'utf8')).toBe('hello')
  })
})

async function authenticatedStore() {
  const store = new MemoryTokenStore()
  await store.save({
    accessToken: 'token',
    tokenType: 'bearer',
    expiresAt: new Date(Date.now() + 3600_000).toISOString(),
  })
  return store
}

function authenticatedDeviceStore() {
  return new MemoryDeviceCredentialStore({
    deviceId: 'dev_test',
    deviceSecret: 'device-secret',
  })
}
