import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NextRequest } from 'next/server'

/**
 * The invitation link (ENT-202).
 *
 * Two branches, and the interesting assertions are about which one runs. The
 * signed-out path is the one §1.8 designed and it must keep its ordering: the
 * token travels with the pre-auth state so the callback redeems it BEFORE the
 * first GetCurrentUser, or provisioning hands the invitee a personal
 * organisation alongside the one they were invited to.
 *
 * The signed-in path exists because sending an authenticated person through
 * registration asks them to create an account they already have. It is not a
 * convenience: an IdP honouring `prompt=create` shows them a signup form.
 */

const startAuthorization = vi.fn()
const acceptInvitation = vi.fn()
const currentSession = vi.fn()

vi.mock('@/lib/auth/flow', () => ({
  startAuthorization: (...a: unknown[]) => startAuthorization(...a),
}))
// getCurrentUser is mocked too, though nothing here calls it: lib/auth/org
// wraps it in React's cache() at module load, and a partial mock leaves that
// wrapping undefined before a single test runs.
vi.mock('@/lib/auth/client', () => ({
  acceptInvitation: (...a: unknown[]) => acceptInvitation(...a),
  getCurrentUser: vi.fn(),
}))
vi.mock('@/lib/auth/session', () => ({
  currentSession: () => currentSession(),
}))

const { GET } = await import('@/app/invite/[token]/route')

function request(token: string): NextRequest {
  return new NextRequest(new URL(`https://console.test/invite/${token}`))
}

function params(token: string) {
  return { params: Promise.resolve({ token }) }
}

beforeEach(() => {
  vi.clearAllMocks()
  currentSession.mockResolvedValue(null)
  startAuthorization.mockResolvedValue('https://idp.test/authorize?x=1')
})

describe('a recipient who is signed out', () => {
  it('hands off to registration carrying the token', async () => {
    const response = await GET(request('tok-abc'), params('tok-abc'))

    expect(acceptInvitation).not.toHaveBeenCalled()
    expect(startAuthorization).toHaveBeenCalledWith(
      expect.objectContaining({ register: true, invitationToken: 'tok-abc' }),
    )
    expect(response.headers.get('location')).toBe(
      'https://idp.test/authorize?x=1',
    )
  })

  // The token is not validated here and must not be. Only core-api can say
  // whether one is real, and asking before the person has authenticated would
  // make this route an oracle reporting which invitations exist.
  it('does not ask core-api whether the token is real', async () => {
    await GET(request('probably-fake'), params('probably-fake'))
    expect(acceptInvitation).not.toHaveBeenCalled()
  })
})

describe('a recipient who is already signed in', () => {
  beforeEach(() => {
    currentSession.mockResolvedValue({ accessToken: 'at-1', orgId: null })
  })

  it('redeems immediately and lands in the organisation just joined', async () => {
    acceptInvitation.mockResolvedValue({
      orgId: 'org-1',
      orgSlug: 'acme-gmbh',
      role: 'member',
    })

    const response = await GET(request('tok-live'), params('tok-live'))

    expect(acceptInvitation).toHaveBeenCalledWith('at-1', 'tok-live')
    expect(response.headers.get('location')).toBe(
      'https://console.test/o/acme-gmbh',
    )
    // The whole point: no trip to the authorization server.
    expect(startAuthorization).not.toHaveBeenCalled()
  })

  // The bug this pins. Falling through to the handoff would ask an
  // authenticated person to register, which is what the signed-in branch
  // exists to avoid, so a failure must not take that route.
  it('does not send them to registration when the token is unusable', async () => {
    acceptInvitation.mockResolvedValue(null)

    const response = await GET(request('tok-expired'), params('tok-expired'))

    expect(startAuthorization).not.toHaveBeenCalled()
    expect(response.headers.get('location')).toBe(
      'https://console.test/workspace?error=invitation',
    )
  })

  // Expired, already redeemed and never real are one answer from core-api, so
  // they are one answer here. A route that distinguished them would leak
  // which tokens exist to anyone holding a session.
  it('answers the same way whichever kind of unusable it was', async () => {
    for (const failure of [null, undefined, {}]) {
      acceptInvitation.mockResolvedValue(failure)
      const response = await GET(request('tok-x'), params('tok-x'))
      expect(response.headers.get('location')).toBe(
        'https://console.test/workspace?error=invitation',
      )
    }
  })
})

describe('a link with no token', () => {
  it('goes to sign-in rather than anywhere authenticated', async () => {
    const response = await GET(request(''), params(''))
    expect(response.headers.get('location')).toBe(
      'https://console.test/sign-in',
    )
    expect(startAuthorization).not.toHaveBeenCalled()
    expect(acceptInvitation).not.toHaveBeenCalled()
  })
})
