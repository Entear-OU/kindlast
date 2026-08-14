import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Session } from '@/lib/auth/session'

/**
 * Keeping a session's access token usable for as long as the session lasts.
 *
 * The bug this closes was found by looking rather than by testing. A real
 * browser, signed in the day before, rendered the workspace in its degraded
 * state: session intact, cookie accepted, and core-api refusing the token
 * because it had expired eleven minutes earlier. The refresh token sat in
 * Redis, unused, valid for another month.
 *
 * `session.ts` already documented the intended behaviour, which is the part
 * worth noticing: `expiresAt` carried the comment "used to refresh before a
 * call rather than after a 401" and nothing implemented it. Same shape as the
 * unwired bootstrap: the field exists, the comment promises, no code does it.
 *
 * Refreshing **before** expiry rather than after a 401 is what makes the rest
 * of this simple, and the concurrency case below is where that pays off.
 */

const refreshTokens = vi.fn()
const store = new Map<string, string>()
const setCalls: unknown[][] = []
let lockHeld = false

vi.mock('@/lib/auth/flow', () => ({
  refreshTokens: (...a: unknown[]) => refreshTokens(...a),
}))

vi.mock('@/lib/auth/redis', () => ({
  redis: () => ({
    get: async (key: string) => store.get(key) ?? null,
    del: async (key: string) => void store.delete(key),
    set: async (key: string, value: string, ...rest: unknown[]) => {
      setCalls.push([key, value, ...rest])
      // The lock is the only NX write, and it is the one that can fail.
      if (rest.includes('NX')) {
        if (lockHeld) return null
        lockHeld = true
        return 'OK'
      }
      store.set(key, value)
      return 'OK'
    },
  }),
}))

const SESSION_ID = 'a-session-id'
const KEY = 'web:session:' + SESSION_ID

function sessionExpiringIn(seconds: number, overrides: Partial<Session> = {}): Session {
  return {
    accessToken: 'old-access',
    refreshToken: 'old-refresh',
    idToken: 'old-id',
    expiresAt: Math.floor(Date.now() / 1000) + seconds,
    subject: 'subject-1',
    orgId: 'org-1',
    ...overrides,
  }
}

function put(session: Session) {
  store.set(KEY, JSON.stringify(session))
}

describe('ensureFreshSession', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    store.clear()
    setCalls.length = 0
    lockHeld = false
  })

  it('leaves a token with life left alone, and calls nothing', async () => {
    // The common case by far. A refresh on every request would put the
    // authorization server in the hot path of every page render.
    const session = sessionExpiringIn(3600)
    put(session)

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(fresh.accessToken).toBe('old-access')
    expect(refreshTokens).not.toHaveBeenCalled()
  })

  it('refreshes a token that has already expired', async () => {
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockResolvedValue({
      accessToken: 'new-access',
      refreshToken: 'rotated',
      idToken: 'new-id',
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    })

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(refreshTokens).toHaveBeenCalledWith('old-refresh')
    expect(fresh.accessToken).toBe('new-access')
  })

  it('refreshes just before expiry rather than just after', async () => {
    // The whole point. Waiting for a 401 means the user sees the failure
    // first, and every caller has to learn to retry.
    const session = sessionExpiringIn(15)
    put(session)
    refreshTokens.mockResolvedValue({
      accessToken: 'new-access',
      refreshToken: null,
      idToken: null,
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    })

    const { ensureFreshSession } = await import('@/lib/auth/session')
    await ensureFreshSession(SESSION_ID, session)

    expect(refreshTokens).toHaveBeenCalled()
  })

  it('writes the new tokens back, so the next request does not refresh again', async () => {
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockResolvedValue({
      accessToken: 'new-access',
      refreshToken: 'rotated',
      idToken: 'new-id',
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    })

    const { ensureFreshSession } = await import('@/lib/auth/session')
    await ensureFreshSession(SESSION_ID, session)

    const stored = JSON.parse(store.get(KEY)!) as Session
    expect(stored.accessToken).toBe('new-access')
    expect(stored.refreshToken).toBe('rotated')

    // KEEPTTL: refreshing a token must not extend the session's own lifetime,
    // or an active person is never signed out.
    const write = setCalls.find((c) => c[0] === KEY && c.includes('KEEPTTL'))
    expect(write).toBeDefined()
  })

  it('keeps the existing refresh token when the server does not rotate', async () => {
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockResolvedValue({
      accessToken: 'new-access',
      refreshToken: null,
      idToken: null,
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    })

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(fresh.refreshToken).toBe('old-refresh')
  })

  it('preserves the tenancy across a refresh', async () => {
    // orgId is not in the token and does not come back from the IdP. Dropping
    // it would silently un-scope every subsequent call.
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockResolvedValue({
      accessToken: 'new-access',
      refreshToken: null,
      idToken: null,
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    })

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(fresh.orgId).toBe('org-1')
    expect(fresh.subject).toBe('subject-1')
  })

  it('returns the session unchanged when the refresh fails', async () => {
    // A transient failure at the IdP must not destroy a session that is good
    // for another month. The call it was for will fail; the next may not.
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockRejectedValue(new Error('token endpoint said no'))

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(fresh.accessToken).toBe('old-access')
    expect(store.has(KEY)).toBe(true)
  })

  it('does nothing for a session that has no refresh token', async () => {
    const session = sessionExpiringIn(-600, { refreshToken: null })
    put(session)

    const { ensureFreshSession } = await import('@/lib/auth/session')
    await ensureFreshSession(SESSION_ID, session)

    expect(refreshTokens).not.toHaveBeenCalled()
  })

  it('does not spend the refresh token twice when requests race', async () => {
    // Rotation makes concurrency dangerous rather than merely wasteful: the
    // second use of a rotated token is refused, and some servers treat replay
    // as theft and revoke the whole grant.
    //
    // The loser re-reads instead of waiting, which is safe precisely because
    // the refresh is proactive: the token it finds still has slack left on it.
    const session = sessionExpiringIn(20)
    put(session)
    lockHeld = true // another request is already refreshing

    const { ensureFreshSession } = await import('@/lib/auth/session')
    const fresh = await ensureFreshSession(SESSION_ID, session)

    expect(refreshTokens).not.toHaveBeenCalled()
    expect(fresh.accessToken).toBe('old-access')
  })

  it('releases the lock even when the refresh throws', async () => {
    // Otherwise one failure blocks every later refresh until the lock expires,
    // turning a transient error into a stuck session.
    const session = sessionExpiringIn(-600)
    put(session)
    refreshTokens.mockRejectedValue(new Error('nope'))

    const { ensureFreshSession } = await import('@/lib/auth/session')
    await ensureFreshSession(SESSION_ID, session)

    expect(store.has('web:refresh:' + SESSION_ID)).toBe(false)
  })
})
