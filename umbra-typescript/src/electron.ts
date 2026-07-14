import type { CollectedDeviceMetadataFields, DeviceMetadata } from './device'
import { execFile } from 'node:child_process'
import { randomBytes, createHash } from 'node:crypto'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { hostname } from 'node:os'
import { dirname } from 'node:path'
import { promisify } from 'node:util'
import { markAutoCollectedDeviceMetadata } from './device'
import { UmbraError } from './errors'

export * from './node'

const execFileAsync = promisify(execFile)
const windowsCurrentVersionKey = String.raw`HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`
const windowsDeviceFingerprintDomain = 'umbra-device-fingerprint:v1\0windows\0'

export interface WindowsDeviceMetadataOptions {
  appVersion?: string
  installId?: string
  installIdPath?: string
  metadata?: Record<string, unknown>
}

export interface WindowsDeviceMetadataSource {
  hostname: string
  arch: string
  registry: Record<string, string>
  installId?: string
  machineGuid?: string
}

export async function detectWindowsDeviceMetadata(options: WindowsDeviceMetadataOptions = {}): Promise<DeviceMetadata> {
  if (process.platform !== 'win32') {
    throw UmbraError.invalidInput('windows device metadata detection is only supported on Windows')
  }

  const registry = await readWindowsCurrentVersionRegistry()
  const installId = options.installId?.trim()
    || await loadOrCreateWindowsInstallId(options.installIdPath)
    || undefined
  const machineGuid = await readWindowsRegistryValue(String.raw`HKLM\SOFTWARE\Microsoft\Cryptography`, 'MachineGuid')
  if (!machineGuid.trim()) {
    throw UmbraError.invalidInput('windows MachineGuid is unavailable')
  }

  return markAutoCollectedDeviceMetadata(buildWindowsDeviceMetadata({
    hostname: hostname(),
    arch: process.arch,
    registry,
    ...(installId ? { installId } : {}),
    machineGuid,
  }, options))
}

export function buildWindowsDeviceMetadata(
  source: WindowsDeviceMetadataSource,
  options: WindowsDeviceMetadataOptions = {},
): CollectedDeviceMetadataFields {
  if (!source.machineGuid?.trim()) {
    throw UmbraError.invalidInput('windows MachineGuid is unavailable')
  }
  const metadata: Record<string, unknown> = { ...(options.metadata ?? {}) }
  const installId = options.installId?.trim() || source.installId?.trim()
  if (installId) metadata.install_id = installId
  metadata.windows = {
    product_name: source.registry.ProductName || '',
    display_version: source.registry.DisplayVersion || '',
    build: source.registry.CurrentBuildNumber || '',
    ubr: source.registry.UBR || '',
    edition_id: source.registry.EditionID || '',
  }

  return {
    name: source.hostname.trim(),
    platform: `windows-${normalizeWindowsArch(source.arch)}`,
    os_version: windowsOsVersion(source.registry),
    ...(options.appVersion?.trim() ? { app_version: options.appVersion.trim() } : {}),
    fingerprint: windowsDeviceFingerprint(source.machineGuid),
    metadata,
  }
}

export async function loadOrCreateWindowsInstallId(path?: string): Promise<string> {
  const target = path?.trim()
  if (!target) return ''
  try {
    const existing = (await readFile(target, 'utf8')).trim()
    if (existing) return existing
  }
  catch (error) {
    if (!isNodeError(error) || error.code !== 'ENOENT') throw error
  }
  const installId = randomBase64Url(18)
  const dir = dirname(target)
  if (dir && dir !== '.') await mkdir(dir, { recursive: true })
  await writeFile(target, `${installId}\n`, { mode: 0o600 })
  return installId
}

export function parseRegQueryValue(output: string, value: string): string {
  const wanted = value.toLowerCase()
  for (const rawLine of output.split(/\r?\n/)) {
    const line = rawLine.trim()
    if (!line.toLowerCase().startsWith(wanted)) continue
    const parts = line.split(/\s+/)
    if (parts.length < 3) return ''
    return parts.slice(2).join(' ').trim()
  }
  return ''
}

async function readWindowsCurrentVersionRegistry() {
  const values: Record<string, string> = {}
  for (const value of ['ProductName', 'DisplayVersion', 'CurrentBuildNumber', 'UBR', 'EditionID']) {
    const got = await readWindowsRegistryValue(windowsCurrentVersionKey, value).catch(() => '')
    if (got) values[value] = got
  }
  return values
}

async function readWindowsRegistryValue(key: string, value: string) {
  const { stdout } = await execFileAsync('reg', ['query', key, '/v', value], { windowsHide: true })
  return parseRegQueryValue(stdout, value)
}

function windowsOsVersion(values: Record<string, string>) {
  let version = values.ProductName?.trim() || 'Windows'
  const displayVersion = values.DisplayVersion?.trim()
  const build = values.CurrentBuildNumber?.trim()
  const ubr = values.UBR?.trim()
  if (displayVersion) version += ` ${displayVersion}`
  if (build) version += ` build ${build}${ubr && /^\d+$/.test(ubr) ? `.${ubr}` : ''}`
  return version.trim()
}

function windowsDeviceFingerprint(machineGuid: string) {
  const normalized = machineGuid.trim().replace(/^\{\s*|\s*\}$/g, '').trim().toLowerCase()
  if (!normalized) return ''
  return `windows:v1:${createHash('sha256')
    .update(windowsDeviceFingerprintDomain)
    .update(normalized)
    .digest('hex')}`
}

function normalizeWindowsArch(arch: string) {
  switch (arch) {
    case 'x64':
      return 'amd64'
    case 'ia32':
      return '386'
    default:
      return arch
  }
}

function randomBase64Url(size: number) {
  return randomBytes(size).toString('base64url')
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error
}
