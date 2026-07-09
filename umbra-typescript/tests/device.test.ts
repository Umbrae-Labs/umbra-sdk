import { afterEach, describe, expect, it } from 'vitest'
import type { IncomingHttpHeaders } from 'node:http'
import {
  createDeviceSignature,
  MemoryDeviceCredentialStore,
  MemoryTokenStore,
  parseRegistrationToken,
  registrationDeviceId,
  sha256Base64Url,
  UmbraClient,
} from '../src'
import { markAutoCollectedDeviceMetadata } from '../src/device'
import { json, readBody, startServer } from './helpers'

const servers: Array<{ close: () => Promise<void> }> = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => server.close()))
})

describe('device signing', () => {
  it('matches the documented signature vector', async () => {
    const result = await createDeviceSignature({
      method: 'POST',
      pathWithQuery: '/api/v1/client/backup/presign?category=world&version=42',
      timestamp: 1716200000,
      nonce: 'nonce-001',
      body: '{"name":"LunaBox"}',
      deviceId: 'dev_test_123',
      secret: 'test-device-secret',
    })

    expect(result.bodyHash).toBe('njjTtBgg9nmsDBrctFvuSK8L6lsXJW7eeRFtarJC20M')
    expect(result.canonicalString).toBe([
      'v1',
      'POST',
      '/api/v1/client/backup/presign?category=world&version=42',
      '1716200000',
      'nonce-001',
      'njjTtBgg9nmsDBrctFvuSK8L6lsXJW7eeRFtarJC20M',
      'dev_test_123',
    ].join('\n'))
    expect(result.signature).toBe('HGr3hoz1CHqufCk3xd43kwfHBi3XhTMTtfKpVy2POZA')
  })

  it('hashes an empty body', async () => {
    await expect(sha256Base64Url('')).resolves.toBe('47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU')
  })

  it('parses registration tokens', () => {
    expect(parseRegistrationToken('umbra_reg_v1_ucd_test.secret-value')).toEqual({
      credentialId: 'ucd_test',
      secret: 'secret-value',
    })
    expect(() => parseRegistrationToken('umbra_reg_v1_ucd_test.secret.value')).toThrow()
  })

  it('registers a device with registration credential signing and stores the result', async () => {
    const tokenStore = new MemoryTokenStore()
    await tokenStore.save({
      accessToken: 'token',
      tokenType: 'bearer',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    })
    const deviceStore = new MemoryDeviceCredentialStore()

    const server = await startServer(async (req, res) => {
      if (req.method === 'POST' && req.url === '/api/v1/client/devices/register') {
        const body = await readBody(req)
        const parsed = JSON.parse(body) as Record<string, unknown>
        expect(parsed).toMatchObject({
          registration_token: 'umbra_reg_v1_ucd_test.registration-secret',
          device: { name: 'LunaBook' },
        })
        expect(parsed.credential_id).toBeUndefined()
        expect(req.headers['x-umbra-device-id']).toBeUndefined()
        await expectRequestSignature(req, {
          body,
          deviceId: registrationDeviceId,
          secret: 'registration-secret',
        })
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            device: {
              device_id: 'dev_registered',
              name: 'LunaBook',
              status: 0,
            },
            device_secret: 'device-secret',
            secret_once: true,
          },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(server)

    const client = new UmbraClient({
      baseUrl: server.url,
      clientId: 'client',
      tokenStore,
      deviceStore,
    })

    const result = await client.devices.register({
      registrationToken: 'umbra_reg_v1_ucd_test.registration-secret',
      device: markAutoCollectedDeviceMetadata({ name: 'LunaBook' }),
    })

    expect(result.device.device_id).toBe('dev_registered')
    await expect(deviceStore.load()).resolves.toEqual({
      deviceId: 'dev_registered',
      deviceSecret: 'device-secret',
    })
  })

  it('rejects manually constructed device metadata', async () => {
    const client = new UmbraClient({
      baseUrl: 'https://umbra.example.com',
      clientId: 'client',
      tokenStore: new MemoryTokenStore(),
      deviceStore: new MemoryDeviceCredentialStore(),
    })

    await expect(client.devices.register({
      registrationToken: 'umbra_reg_v1_ucd_test.registration-secret',
      device: { name: 'LunaBook' } as any,
    })).rejects.toThrow('device metadata must be collected by the SDK')
  })

  it('rotates a device secret and stores the replacement', async () => {
    const tokenStore = new MemoryTokenStore()
    await tokenStore.save({
      accessToken: 'token',
      tokenType: 'bearer',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    })
    const deviceStore = new MemoryDeviceCredentialStore({
      deviceId: 'dev_registered',
      deviceSecret: 'old-secret',
    })

    const server = await startServer(async (req, res) => {
      if (req.method === 'POST' && req.url === '/api/v1/user/devices/dev_registered/rotate-secret') {
        expect(req.headers.authorization).toBe('Bearer token')
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: {
            device: {
              device_id: 'dev_registered',
              name: 'LunaBook',
              status: 0,
            },
            device_secret: 'new-secret',
            secret_once: true,
          },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(server)

    const client = new UmbraClient({
      baseUrl: server.url,
      clientId: 'client',
      tokenStore,
      deviceStore,
    })

    await expect(client.devices.rotateSecret()).resolves.toMatchObject({
      device: { device_id: 'dev_registered' },
      device_secret: 'new-secret',
    })
    await expect(deviceStore.load()).resolves.toEqual({
      deviceId: 'dev_registered',
      deviceSecret: 'new-secret',
    })
  })

  it('automatically signs protected backup requests with stored device credentials', async () => {
    const tokenStore = new MemoryTokenStore()
    await tokenStore.save({
      accessToken: 'token',
      tokenType: 'bearer',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    })

    const server = await startServer(async (req, res) => {
      if (req.method === 'GET' && req.url === '/api/v1/client/backup/list?category=game') {
        const body = await readBody(req)
        expect(req.headers['x-umbra-device-id']).toBe('dev_test')
        await expectRequestSignature(req, {
          body,
          deviceId: 'dev_test',
          secret: 'device-secret',
        })
        expect(req.headers['x-umbra-body-sha256']).toBe('47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU')
        json(res, 200, {
          code: 0,
          msg: 'success',
          data: { files: [], total: 0 },
        })
        return
      }
      json(res, 404, { code: 1001, msg: 'not found', data: null })
    })
    servers.push(server)

    const client = new UmbraClient({
      baseUrl: server.url,
      clientId: 'client',
      tokenStore,
      deviceStore: new MemoryDeviceCredentialStore({
        deviceId: 'dev_test',
        deviceSecret: 'device-secret',
      }),
    })

    await expect(client.backups.list({ category: 'game' })).resolves.toEqual([])
  })
})

async function expectRequestSignature(
  req: { method?: string | undefined, url?: string | undefined, headers: IncomingHttpHeaders },
  input: { body: string, deviceId: string, secret: string },
) {
  const timestamp = Number(req.headers['x-umbra-timestamp'])
  const nonce = String(req.headers['x-umbra-nonce'] || '')
  const bodyHash = String(req.headers['x-umbra-body-sha256'] || '')
  const signatureHeader = String(req.headers['x-umbra-signature'] || '')
  expect(Number.isSafeInteger(timestamp)).toBe(true)
  expect(nonce).not.toBe('')
  expect(bodyHash).not.toBe('')
  expect(signatureHeader.startsWith('v1=')).toBe(true)

  const expected = await createDeviceSignature({
    method: req.method || '',
    pathWithQuery: req.url || '',
    timestamp,
    nonce,
    body: input.body,
    deviceId: input.deviceId,
    secret: input.secret,
  })
  expect(bodyHash).toBe(expected.bodyHash)
  expect(signatureHeader).toBe(`v1=${expected.signature}`)
}
