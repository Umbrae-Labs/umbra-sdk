import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { describe, expect, it, vi } from 'vitest'
import {
  buildWindowsDeviceMetadata,
  detectWindowsDeviceMetadata,
  loadOrCreateWindowsInstallId,
  parseRegQueryValue,
} from '../src/electron'

const execFileMock = vi.hoisted(() => vi.fn((
  _command: string,
  args: string[],
  _options: { windowsHide?: boolean },
  callback: (error: Error | null, result: { stdout: string, stderr: string }) => void,
) => {
  const value = args.at(-1) ?? ''
  callback(null, {
    stdout: `${value}    REG_SZ    test-value`,
    stderr: '',
  })
}))

vi.mock('node:child_process', () => ({ execFile: execFileMock }))

describe('electron windows metadata helpers', () => {
  it('parses reg query output', () => {
    const output = String.raw`
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion
    ProductName    REG_SZ    Windows 11 Pro
`
    expect(parseRegQueryValue(output, 'ProductName')).toBe('Windows 11 Pro')
  })

  it('builds device metadata from windows source values', () => {
    const metadata = buildWindowsDeviceMetadata({
      hostname: 'LunaBook',
      arch: 'x64',
      installId: 'install-123',
      machineGuid: 'machine-guid',
      registry: {
        ProductName: 'Windows 11 Pro',
        DisplayVersion: '23H2',
        CurrentBuildNumber: '22631',
        UBR: '3593',
        EditionID: 'Professional',
      },
    }, {
      appVersion: '1.0.0',
    })

    expect(metadata).toMatchObject({
      name: 'LunaBook',
      platform: 'windows-amd64',
      os_version: 'Windows 11 Pro 23H2 build 22631.3593',
      app_version: '1.0.0',
      fingerprint: 'windows:v1:dedf1fa9f8b1d3b4826b2a935b09f6fca8280863881c1989d21c86885fdecf16',
    })
    expect(metadata.metadata?.install_id).toBe('install-123')
    expect(metadata.metadata?.windows).toMatchObject({
      product_name: 'Windows 11 Pro',
      display_version: '23H2',
      build: '22631',
      ubr: '3593',
      edition_id: 'Professional',
    })
  })

  it('keeps the device fingerprint stable across formatting and collection options', () => {
    const first = buildWindowsDeviceMetadata({
      hostname: 'LunaBook',
      arch: 'x64',
      installId: 'first-install',
      machineGuid: '4C4C4544-0038-3710-8051-CAC04F4B4332',
      registry: {},
    }, { appVersion: '1.0.0' })
    const second = buildWindowsDeviceMetadata({
      hostname: 'Renamed-LunaBook',
      arch: 'x64',
      installId: 'second-install',
      machineGuid: ' {4c4c4544-0038-3710-8051-cac04f4b4332} ',
      registry: {},
    }, { appVersion: '2.0.0' })

    expect(first.fingerprint).toBe('windows:v1:068442b331fed45178be4b7e7a403f261b19e55ff789340babc97e60cdcb414f')
    expect(second.fingerprint).toBe(first.fingerprint)
  })

  it('requires MachineGuid for Windows metadata', () => {
    expect(() => buildWindowsDeviceMetadata({
      hostname: 'LunaBook',
      arch: 'x64',
      registry: {},
    })).toThrow('windows MachineGuid is unavailable')
  })

  it('hides every registry query subprocess window', async () => {
    const platform = vi.spyOn(process, 'platform', 'get').mockReturnValue('win32')
    execFileMock.mockClear()

    try {
      await detectWindowsDeviceMetadata({ installId: 'install-123' })
    }
    finally {
      platform.mockRestore()
    }

    expect(execFileMock).toHaveBeenCalledTimes(6)
    for (const call of execFileMock.mock.calls) {
      expect(call[0]).toBe('reg')
      expect(call[2]).toMatchObject({ windowsHide: true })
    }
  })

  it('persists generated install ids', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'umbra-sdk-'))
    try {
      const path = join(dir, 'install_id')
      const first = await loadOrCreateWindowsInstallId(path)
      const second = await loadOrCreateWindowsInstallId(path)
      expect(second).toBe(first)
      await expect(readFile(path, 'utf8')).resolves.toBe(`${first}\n`)
    }
    finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
