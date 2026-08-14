import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NextRequest } from 'next/server'

/**
 * The proxy after Supabase (ENT-197).
 *
 * What it does now is deliberately less than what it did before, and the
 * reduction is the design. Middleware runs on the edge runtime, where there is
 * no Redis and no session store, so it cannot know whether a session is real.
 * It checks for the presence of a cookie and nothing else.
 *
 * **A cookie's presence is not authorization**, and nothing here should ever
 * be mistaken for it. Every server component and route handler reads the
 * session from Redis, and core-api verifies the token independently of both.
 * This exists to save a signed-out person a round trip into a page that would
 * only redirect them back, which is a convenience rather than a control.
 */

vi.mock('next/server', async () => {
  const actual =
    await vi.importActual<typeof import('next/server')>('next/server')
  return {
    ...actual,
    NextResponse: {
      next: vi.fn(() => ({
        type: 'next',
        cookies: { set: vi.fn() },
        headers: new Headers(),
      })),
      redirect: vi.fn((url: URL) => ({
        type: 'redirect',
        status: 307,
        headers: new Headers({ Location: url.toString() }),
        cookies: { set: vi.fn() },
        url: url.toString(),
      })),
    },
  }
})

const SESSION_COOKIE = 'kindlast_session'

const CSRF_COOKIE = 'kindlast_csrf'

function requestFor(
  pathname: string,
  options: { session?: boolean; csrf?: string } = {},
): NextRequest {
  const request = new NextRequest(new URL(`http://localhost:3000${pathname}`))
  if (options.session) {
    request.cookies.set(SESSION_COOKIE, 'a-session-id')
  }
  if (options.csrf) {
    request.cookies.set(CSRF_COOKIE, options.csrf)
  }
  return request
}

describe('proxy', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it.each(['/workspace'])(
    'sends a signed-out visitor from %s to sign-in',
    async (path) => {
      const { proxy } = await import('@/proxy')
      const response = await proxy(requestFor(path))

      expect(response).toMatchObject({ type: 'redirect' })
      expect(response.url).toContain('/sign-in')
    },
  )

  /**
   * The CSRF token is minted here because here is the only place that can.
   *
   * It is a double-submit token, so the page rendering the sign-out form has
   * to read it and echo it in the body. Minting it in that server component
   * cannot work: Next refuses cookie writes during a page render, which threw
   * on every authenticated page and took the whole layout down with it. A
   * route handler could set it, but only for requests that reach one, and a
   * signed-in person navigating between pages never does.
   *
   * Middleware sets cookies on the response and runs before any page renders,
   * which makes it the one place the token is guaranteed to exist by the time
   * a form needs it.
   */
  it('issues a CSRF token to a signed-in visitor who has none', async () => {
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/workspace', { session: true }))

    expect(response.cookies.set).toHaveBeenCalledWith(
      CSRF_COOKIE,
      expect.any(String),
      expect.objectContaining({ httpOnly: false }),
    )
  })

  it('leaves an existing CSRF token alone, so a rendered form stays valid', async () => {
    // Re-minting on every navigation would invalidate the token already
    // embedded in a form on the page the person is looking at.
    const { proxy } = await import('@/proxy')
    const response = await proxy(
      requestFor('/workspace', { session: true, csrf: 'already-issued' }),
    )

    expect(response.cookies.set).not.toHaveBeenCalled()
  })

  it('does not issue a CSRF token to someone with no session', async () => {
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/'))

    expect(response.cookies.set).not.toHaveBeenCalled()
  })

  it('carries the attempted path so sign-in can return there', async () => {
    // A nested path rather than the bare prefix, because the whole point is
    // that the person lands back where they were going.
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/workspace/reports'))

    expect(response.url).toContain('returnTo=%2Fworkspace%2Freports')
  })

  it('lets a visitor carrying a session cookie through', async () => {
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/workspace', { session: true }))

    expect(response).toMatchObject({ type: 'next' })
  })

  it('sends someone who already has a session to the workspace', async () => {
    // The console pages that used to be the destination are gone (ENT-200):
    // they gated on a Supabase session the OIDC path no longer creates, so
    // sending anyone there bounced them straight back out.
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/sign-in', { session: true }))

    expect(response).toMatchObject({ type: 'redirect' })
    expect(response.url).toContain('/workspace')
  })

  it.each(['/', '/sign-in', '/features', '/auth/login', '/auth/callback'])(
    'leaves %s alone for a signed-out visitor',
    async (path) => {
      const { proxy } = await import('@/proxy')
      const response = await proxy(requestFor(path))

      expect(response).toMatchObject({ type: 'next' })
    },
  )

  it('never redirects the callback away, even carrying a session', async () => {
    // The callback sets the session cookie itself, so a visitor can arrive
    // here already holding one from an earlier sign-in. Redirecting them would
    // strand the authorization code and break re-authentication.
    const { proxy } = await import('@/proxy')
    const response = await proxy(
      requestFor('/auth/callback', { session: true }),
    )

    expect(response).toMatchObject({ type: 'next' })
  })
})
