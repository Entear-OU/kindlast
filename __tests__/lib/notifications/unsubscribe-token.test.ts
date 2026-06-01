import { describe, expect, it } from 'vitest'

import {
  UNSUBSCRIBE_TOKEN_TTL_SECONDS,
  buildUnsubscribeUrl,
  signUnsubscribeToken,
  verifyUnsubscribeToken,
} from '@/lib/notifications/unsubscribe-token'

/**
 * ENT-74 — signed unsubscribe tokens for the weekly briefing footer.
 */

const SECRET = 'unsub-secret'
const NOW = 1_700_000_000
const USER = '33333333-3333-3333-3333-333333333333'

describe('unsubscribe-token (ENT-74)', () => {
  it('round-trips a valid token preserving user + scope', () => {
    const token = signUnsubscribeToken({ userId: USER, scope: 'weekly_briefing', nowSeconds: NOW }, SECRET)
    const result = verifyUnsubscribeToken(token, SECRET, NOW + 10)
    expect(result.valid).toBe(true)
    if (result.valid) {
      expect(result.payload.userId).toBe(USER)
      expect(result.payload.scope).toBe('weekly_briefing')
    }
  })

  it('rejects a wrong secret', () => {
    const token = signUnsubscribeToken({ userId: USER, scope: 'weekly_briefing', nowSeconds: NOW }, SECRET)
    expect(verifyUnsubscribeToken(token, 'other', NOW)).toEqual({ valid: false, reason: 'bad_signature' })
  })

  it('rejects an expired token', () => {
    const token = signUnsubscribeToken({ userId: USER, scope: 'weekly_briefing', nowSeconds: NOW }, SECRET)
    const result = verifyUnsubscribeToken(token, SECRET, NOW + UNSUBSCRIBE_TOKEN_TTL_SECONDS + 1)
    expect(result).toEqual({ valid: false, reason: 'expired' })
  })

  it('rejects a malformed token', () => {
    expect(verifyUnsubscribeToken('nope', SECRET, NOW)).toEqual({ valid: false, reason: 'malformed' })
  })

  it('builds an absolute unsubscribe URL carrying a verifiable token', () => {
    const url = buildUnsubscribeUrl(
      'https://app.kindlast.com',
      { userId: USER, scope: 'weekly_briefing', nowSeconds: NOW },
      SECRET,
    )
    const parsed = new URL(url)
    expect(parsed.origin + parsed.pathname).toBe('https://app.kindlast.com/api/notifications/unsubscribe')
    expect(verifyUnsubscribeToken(parsed.searchParams.get('token')!, SECRET, NOW).valid).toBe(true)
  })
})
