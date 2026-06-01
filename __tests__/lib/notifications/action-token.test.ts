import { describe, expect, it } from 'vitest'

import {
  ACTION_TOKEN_TTL_SECONDS,
  buildActionUrl,
  signActionToken,
  verifyActionToken,
} from '@/lib/notifications/action-token'

/**
 * ENT-73 — signed one-tap action tokens.
 *
 * The email's Approve/Reject/Snooze links carry these. They must round-trip,
 * reject tampering and a wrong secret, and expire — so a leaked stale link
 * can't act on a finding forever.
 */

const SECRET = 'test-notification-secret'
const NOW = 1_700_000_000
const FINDING = '11111111-1111-1111-1111-111111111111'

describe('action-token (ENT-73)', () => {
  it('round-trips a valid token', () => {
    const token = signActionToken({ findingId: FINDING, action: 'approve', nowSeconds: NOW }, SECRET)
    const result = verifyActionToken(token, SECRET, NOW + 10)
    expect(result.valid).toBe(true)
    if (result.valid) {
      expect(result.payload.findingId).toBe(FINDING)
      expect(result.payload.action).toBe('approve')
    }
  })

  it('defaults snooze to the feed default-snooze days', () => {
    const token = signActionToken({ findingId: FINDING, action: 'snooze', nowSeconds: NOW }, SECRET)
    const result = verifyActionToken(token, SECRET, NOW)
    expect(result.valid).toBe(true)
    if (result.valid) expect(result.payload.days).toBe(7)
  })

  it('rejects a tampered payload', () => {
    const token = signActionToken({ findingId: FINDING, action: 'reject', nowSeconds: NOW }, SECRET)
    const [, sig] = token.split('.')
    const forgedPayload = Buffer.from(
      JSON.stringify({ findingId: FINDING, action: 'approve', exp: NOW + 100 }),
    ).toString('base64url')
    const result = verifyActionToken(`${forgedPayload}.${sig}`, SECRET, NOW)
    expect(result).toEqual({ valid: false, reason: 'bad_signature' })
  })

  it('rejects a wrong secret', () => {
    const token = signActionToken({ findingId: FINDING, action: 'approve', nowSeconds: NOW }, SECRET)
    const result = verifyActionToken(token, 'other-secret', NOW)
    expect(result).toEqual({ valid: false, reason: 'bad_signature' })
  })

  it('rejects an expired token', () => {
    const token = signActionToken({ findingId: FINDING, action: 'approve', nowSeconds: NOW }, SECRET)
    const result = verifyActionToken(token, SECRET, NOW + ACTION_TOKEN_TTL_SECONDS + 1)
    expect(result).toEqual({ valid: false, reason: 'expired' })
  })

  it('rejects a malformed token', () => {
    expect(verifyActionToken('not-a-token', SECRET, NOW)).toEqual({ valid: false, reason: 'malformed' })
  })

  it('builds an absolute one-tap URL carrying a verifiable token', () => {
    const url = buildActionUrl(
      'https://app.kindlast.com',
      { findingId: FINDING, action: 'snooze', nowSeconds: NOW },
      SECRET,
    )
    const parsed = new URL(url)
    expect(parsed.origin + parsed.pathname).toBe('https://app.kindlast.com/api/findings/act')
    const token = parsed.searchParams.get('token')!
    expect(verifyActionToken(token, SECRET, NOW).valid).toBe(true)
  })
})
