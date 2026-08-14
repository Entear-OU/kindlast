import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NextRequest } from 'next/server'

/**
 * The OIDC callback (ENT-197), replacing the Supabase code exchange that used
 * to live at this path.
 *
 * The cases worth having are the refusals. A callback that completes a sign-in
 * is one path; a callback that completes one it should not have is a session
 * handed to whoever asked, so each refusal is asserted separately and by
 * reason rather than merely as "did not sign in".
 */

const consumeState = vi.fn()
const exchangeCode = vi.fn()
const createSession = vi.fn()
const getCurrentUser = vi.fn()
const acceptInvitation = vi.fn()

vi.mock('@/lib/auth/client', () => ({
  getCurrentUser: (...a: unknown[]) => getCurrentUser(...a),
  acceptInvitation: (...a: unknown[]) => acceptInvitation(...a),
  activeOrgFrom: (me: { memberships?: { orgId: string }[] } | null) =>
    me?.memberships?.[0]?.orgId ?? null,
}))
vi.mock('@/lib/auth/state', () => ({
  consumeState: (...a: unknown[]) => consumeState(...a),
}))
vi.mock('@/lib/auth/flow', async () => {
  const actual =
    await vi.importActual<typeof import('@/lib/auth/flow')>('@/lib/auth/flow')
  return { ...actual, exchangeCode: (...a: unknown[]) => exchangeCode(...a) }
})
vi.mock('@/lib/auth/session', async () => {
  const actual =
    await vi.importActual<typeof import('@/lib/auth/session')>(
      '@/lib/auth/session',
    )
  return { ...actual, createSession: (...a: unknown[]) => createSession(...a) }
})

function callbackRequest(query: string): NextRequest {
  return new NextRequest(new URL(`http://localhost:3000/auth/callback${query}`))
}

/** A token whose payload carries a subject, which is all the callback reads. */
function accessTokenFor(subject: string): string {
  const payload = Buffer.from(JSON.stringify({ sub: subject })).toString(
    'base64url',
  )
  return `header.${payload}.signature`
}

describe('GET /auth/callback', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    consumeState.mockResolvedValue(null)
    createSession.mockResolvedValue('a-new-session-id')
    getCurrentUser.mockResolvedValue({ memberships: [] })
    acceptInvitation.mockResolvedValue(true)
  })

  it('sends someone who declined at the identity provider back, saying so', async () => {
    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?error=access_denied'))

    expect(response.headers.get('location')).toContain('/sign-in?error=denied')
    expect(exchangeCode).not.toHaveBeenCalled()
  })

  it.each([
    ['no code', '?state=abc'],
    ['no state', '?code=abc'],
    ['neither', ''],
  ])('refuses a callback with %s', async (_name, query) => {
    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest(query))

    expect(response.headers.get('location')).toContain('error=state')
    expect(exchangeCode).not.toHaveBeenCalled()
  })

  it('refuses a state this server never issued', async () => {
    // Unknown, expired and forged are the same answer on purpose: there is
    // nothing here for someone probing to learn.
    consumeState.mockResolvedValue(null)

    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?code=abc&state=not-ours'))

    expect(response.headers.get('location')).toContain('error=state')
    // The code must not be exchanged for a flow we cannot tie to a request we
    // started, which is the entire job of the state parameter.
    expect(exchangeCode).not.toHaveBeenCalled()
  })

  it('exchanges the code with the verifier that was stashed, never one from the URL', async () => {
    consumeState.mockResolvedValue({
      verifier: 'the-stashed-verifier',
      returnTo: '/dashboard',
      createdAt: Date.now(),
    })
    exchangeCode.mockResolvedValue({
      accessToken: accessTokenFor('subject-1'),
      refreshToken: 'r',
      idToken: 'i',
      expiresAt: 999,
    })

    const { GET } = await import('@/app/auth/callback/route')
    await GET(
      callbackRequest(
        '?code=the-code&state=ours&code_verifier=attacker-supplied',
      ),
    )

    expect(exchangeCode).toHaveBeenCalledWith(
      'the-code',
      'the-stashed-verifier',
    )
  })

  it('creates a session and sets the cookie on success', async () => {
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: '/feed',
      createdAt: Date.now(),
    })
    exchangeCode.mockResolvedValue({
      accessToken: accessTokenFor('subject-1'),
      refreshToken: 'r',
      idToken: 'i',
      expiresAt: 999,
    })

    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?code=abc&state=ours'))

    expect(createSession).toHaveBeenCalledWith(
      expect.objectContaining({ subject: 'subject-1', refreshToken: 'r' }),
    )
    expect(response.headers.get('location')).toContain('/feed')

    const cookie = response.cookies.get('kindlast_session')
    expect(cookie?.value).toBe('a-new-session-id')
    // The browser gets an id. Tokens stay in Redis (§1.2).
    expect(response.headers.get('set-cookie')).not.toContain(
      accessTokenFor('subject-1'),
    )
    expect(cookie?.httpOnly).toBe(true)
  })

  it('refuses an off-site returnTo rather than following it', async () => {
    // Otherwise the callback is an open redirect wearing our domain.
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: 'https://attacker.example/phish',
      createdAt: Date.now(),
    })
    exchangeCode.mockResolvedValue({
      accessToken: accessTokenFor('subject-1'),
      refreshToken: null,
      idToken: null,
      expiresAt: 999,
    })

    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?code=abc&state=ours'))

    expect(response.headers.get('location')).not.toContain('attacker.example')
  })

  it('sends the person back to sign-in when the exchange fails', async () => {
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: '/dashboard',
      createdAt: Date.now(),
    })
    exchangeCode.mockRejectedValue(new Error('token endpoint said no'))

    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?code=abc&state=ours'))

    expect(response.headers.get('location')).toContain('error=exchange')
    expect(createSession).not.toHaveBeenCalled()
  })
})

/**
 * The bootstrap, which is the reason the callback calls core-api at all.
 *
 * GetCurrentUser is where just-in-time provisioning happens, so for a person
 * signing in for the first time it is the call that creates their
 * organisation. Until it has run there is nothing to put in the
 * active-organisation header, which makes it the difference between a session
 * that exists and a session that can do anything.
 */
describe('GET /auth/callback, provisioning', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createSession.mockResolvedValue('a-new-session-id')
    getCurrentUser.mockResolvedValue({ memberships: [] })
    acceptInvitation.mockResolvedValue(true)
    exchangeCode.mockResolvedValue({
      accessToken: accessTokenFor('subject-1'),
      refreshToken: 'r',
      idToken: 'i',
      expiresAt: 999,
    })
  })

  it('records the organisation provisioning returned on the session', async () => {
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: '/feed',
      createdAt: Date.now(),
    })
    getCurrentUser.mockResolvedValue({
      memberships: [{ orgId: 'personal-org', role: 'owner' }],
    })

    const { GET } = await import('@/app/auth/callback/route')
    await GET(callbackRequest('?code=abc&state=ours'))

    expect(getCurrentUser).toHaveBeenCalledWith(accessTokenFor('subject-1'))
    expect(createSession).toHaveBeenCalledWith(
      expect.objectContaining({ orgId: 'personal-org' }),
    )
  })

  it('accepts an invitation before provisioning, never after', async () => {
    // §1.8, and the ordering is the whole point. Backwards, provisioning sees
    // a subject with no membership and creates a personal organisation
    // alongside the one they were invited to, which is a wrong tenancy rather
    // than an untidy one.
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: '/feed',
      invitationToken: 'invite-token',
      createdAt: Date.now(),
    })

    const order: string[] = []
    acceptInvitation.mockImplementation(async () => {
      order.push('accept')
      return true
    })
    getCurrentUser.mockImplementation(async () => {
      order.push('me')
      return { memberships: [{ orgId: 'invited-org' }] }
    })

    const { GET } = await import('@/app/auth/callback/route')
    await GET(callbackRequest('?code=abc&state=ours'))

    expect(order).toEqual(['accept', 'me'])
    expect(acceptInvitation).toHaveBeenCalledWith(
      accessTokenFor('subject-1'),
      'invite-token',
    )
  })

  it('signs the person in anyway when provisioning fails', async () => {
    // A failed bootstrap is a degraded session, not a locked door. They hold a
    // valid token; the call can be retried on the next navigation.
    consumeState.mockResolvedValue({
      verifier: 'v',
      returnTo: '/feed',
      createdAt: Date.now(),
    })
    getCurrentUser.mockResolvedValue(null)

    const { GET } = await import('@/app/auth/callback/route')
    const response = await GET(callbackRequest('?code=abc&state=ours'))

    expect(createSession).toHaveBeenCalledWith(
      expect.objectContaining({ orgId: null }),
    )
    expect(response.headers.get('location')).toContain('/feed')
    expect(response.cookies.get('kindlast_session')?.value).toBe(
      'a-new-session-id',
    )
  })
})
