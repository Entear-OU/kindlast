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

  /**
   * The lockout, and why it is the proxy that has to end it.
   *
   * A session id lives in Redis and the cookie carries only the id, so a cookie
   * outlives its session whenever the session goes first: it expires, somebody
   * signs out everywhere, Redis restarts, or a developer runs `down -v`. The
   * browser then holds a cookie that resolves to nothing.
   *
   * That is the state where the proxy and the pages disagree. The proxy cannot
   * read Redis from the edge, so it reasons about the cookie EXISTING. Every
   * protected page reads the session and reasons about it RESOLVING. So:
   *
   *   /workspace  + dead cookie -> page finds no session -> /sign-in?returnTo=
   *   /sign-in    + dead cookie -> proxy sees a cookie   -> /workspace
   *
   * which is ERR_TOO_MANY_REDIRECTS, and a person in it cannot reach any page
   * of the product, including the one that would sign them in again. The only
   * escape is clearing cookies by hand, which no customer will think to do.
   *
   * Found in a browser rather than by any test, because curl sends no cookies
   * and Playwright starts every run with a fresh context. It needed a browser
   * somebody had actually been signed into.
   */
  it('renders sign-in rather than bouncing back when a page sent the visitor here', async () => {
    // `returnTo` is the signal, and it means "a protected surface sent me". A
    // cookie holder arriving with it has just been rejected by the page that
    // set it, so bouncing them back is the cycle.
    const { proxy } = await import('@/proxy')
    const response = await proxy(
      requestFor('/sign-in?returnTo=%2Fworkspace', { session: true }),
    )

    expect(response).toMatchObject({ type: 'next' })
  })

  it('breaks the cycle for any protected surface, not just /workspace', async () => {
    // The console is a growing list of pages and every one of them redirects
    // to sign-in when the session does not resolve. Fixing this per page would
    // be a fix that only works while every future page remembers, which is the
    // same bug waiting; the cycle is broken once, here.
    const { proxy } = await import('@/proxy')

    for (const attempted of [
      '/o/acme/logs',
      '/o/acme/regulation',
      '/o/acme/settings/billing',
    ]) {
      const response = await proxy(
        requestFor(`/sign-in?returnTo=${encodeURIComponent(attempted)}`, {
          session: true,
        }),
      )

      expect(response, attempted).toMatchObject({ type: 'next' })
    }
  })

  it('still sends a signed-in visitor who simply visited /sign-in to the workspace', async () => {
    // The convenience the redirect exists for is kept. Someone who navigated
    // to /sign-in of their own accord carries no `returnTo`, and nothing has
    // just rejected them, so there is no cycle to break.
    const { proxy } = await import('@/proxy')
    const response = await proxy(requestFor('/sign-in', { session: true }))

    expect(response).toMatchObject({ type: 'redirect' })
    expect(response.url).toContain('/workspace')
  })
})
