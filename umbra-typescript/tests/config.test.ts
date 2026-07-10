import { describe, expect, it } from 'vitest'
import { gameBackup, normalizeConfig, validateAddress } from '../src'

describe('config', () => {
  it('derives same-origin endpoints', () => {
    const config = normalizeConfig({
      baseUrl: 'https://umbra.example.com/',
      clientId: 'client',
      fetch: (() => Promise.reject(new Error('unused'))) as typeof fetch,
    })

    expect(config.baseUrl).toBe('https://umbra.example.com')
    expect(config.apiBaseUrl).toBe('https://umbra.example.com/api/v1')
    expect(config.authorizationEndpoint).toBe('https://umbra.example.com/oauth2/auth')
    expect(config.tokenEndpoint).toBe('https://umbra.example.com/oauth2/token')
    expect(config.revocationEndpoint).toBe('https://umbra.example.com/oauth2/revoke')
  })

  it('validates backup addresses', () => {
    expect(() => validateAddress(gameBackup('mc', 'v1'))).not.toThrow()
    expect(() => validateAddress(gameBackup('bad/slash', 'v1'))).toThrow()
    expect(() => validateAddress({ category: 'db', subject: 'x', version: 'v1' })).toThrow()
    expect(() => validateAddress({ category: 'sync', subject: 'library', version: 'manifest' } as any)).toThrow()
  })
})

