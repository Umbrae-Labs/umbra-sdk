import { UmbraError } from './errors'

const contentHashPattern = /^[a-f0-9]{64}$/

export function normalizeContentHash(hash: string | undefined, allowEmpty: boolean) {
  const normalized = hash?.trim().toLowerCase() || ''
  if (!normalized) {
    if (allowEmpty) return undefined
    throw UmbraError.invalidInput('contentHash is required')
  }
  if (!contentHashPattern.test(normalized)) {
    throw UmbraError.invalidInput('contentHash must be lowercase SHA-256 hex')
  }
  return normalized
}
