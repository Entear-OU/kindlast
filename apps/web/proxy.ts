import { NextResponse, type NextRequest } from 'next/server'
import { SESSION_COOKIE } from '@/lib/auth/session'
import { DEFAULT_RETURN_TO } from '@/lib/auth/return-to'
import { CSRF_COOKIE } from '@/lib/auth/csrf'
import { randomToken } from '@/lib/auth/pkce'

/**
 * The proxy, after Supabase.
 *
 * It does deliberately less than the Supabase version did, and the reduction
 * is the design rather than a regression. Middleware runs on the edge runtime,
 * where there is no Redis, so it cannot read the session store and cannot know
 * whether a session id is real. It checks whether a cookie is present.
 *
 * **A cookie's presence is not authorization.** Every server component and
 * route handler reads the session from Redis before trusting it, and core-api
 * verifies the token independently of both, so a forged cookie gets someone
 * exactly as far as a page that then finds no session. What this saves is a
 * signed-out person a round trip into a page that would only bounce them back,
 * which is a convenience, not a control.
 *
 * The previous version called Supabase on every request to refresh a session.
 * Nothing here talks to a network at all.
 */

/** Prefixes that need a session. Everything else is public. */
const PROTECTED = [
  '/workspace',
  '/dashboard',
  '/onboarding',
  '/feed',
  '/records',
  '/settings',
]

/**
 * Paths the auth flow owns, which must never be redirected.
 *
 * The callback sets the session cookie itself, so someone can arrive there
 * already holding one from an earlier sign-in; bouncing them would strand the
 * authorization code and break re-authentication.
 */
const AUTH_PATHS = ['/auth/', '/invite/']

export async function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl
  const hasSession = Boolean(request.cookies.get(SESSION_COOKIE)?.value)

  if (AUTH_PATHS.some((prefix) => pathname.startsWith(prefix))) {
    return NextResponse.next()
  }

  if (!hasSession && PROTECTED.some((prefix) => pathname.startsWith(prefix))) {
    const url = request.nextUrl.clone()
    url.pathname = '/sign-in'
    url.search = ''
    // Carried so sign-in returns someone to what they asked for rather than to
    // a dashboard they then have to navigate away from.
    url.searchParams.set('returnTo', pathname)
    return NextResponse.redirect(url)
  }

  if (hasSession && pathname === '/sign-in') {
    const url = request.nextUrl.clone()
    url.pathname = DEFAULT_RETURN_TO
    url.search = ''
    return NextResponse.redirect(url)
  }

  return withCsrfToken(request, NextResponse.next())
}

/**
 * Mint the double-submit CSRF token, because this is the only place that can.
 *
 * The sign-out form is a server component and has to read this value to echo
 * it in the request body. Minting it there is impossible: Next refuses cookie
 * writes during a page render, and the attempt threw, which took down the
 * layout and every authenticated page rendered inside it. A route handler can
 * set cookies but a person navigating between pages never reaches one.
 *
 * Not httpOnly, deliberately, which is the entire mechanism: the page must be
 * able to read it, and a cross-site attacker cannot, because reading a cookie
 * requires same-origin script. The value carries no authority by itself.
 */
function withCsrfToken(request: NextRequest, response: NextResponse): NextResponse {
  const hasSession = Boolean(request.cookies.get(SESSION_COOKIE)?.value)
  if (!hasSession) return response

  // Left alone when it exists: re-minting on every navigation would invalidate
  // the token already embedded in a form on the page being looked at.
  if (request.cookies.get(CSRF_COOKIE)?.value) return response

  response.cookies.set(CSRF_COOKIE, randomToken(32), {
    httpOnly: false,
    sameSite: 'lax',
    secure: process.env.NODE_ENV === 'production',
    path: '/',
  })
  return response
}
