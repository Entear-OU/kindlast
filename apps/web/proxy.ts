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

/**
 * Prefixes that need a session. Everything else is public.
 *
 * `/o/` covers the whole console in one entry, because every authenticated
 * route lives under `/o/{slug}/` now (ENT-198). That is the lasting benefit of
 * URL-scoped organisations here: surfaces return one by one as they are
 * rebuilt on core-api (ENT-200), and not one of them needs a line adding to
 * this list, so there is no way to ship a page that quietly is not covered.
 *
 * `/workspace` stays because it still resolves: it is in bookmarks and in
 * DEFAULT_RETURN_TO, and it redirects into the caller's organisation.
 */
const PROTECTED = ['/o/', '/workspace']

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

  // A convenience, and one that has to know when to stop.
  //
  // WHY `returnTo` GATES THIS
  //
  // The cookie carries a session id and the session itself lives in Redis, so
  // the cookie outlives the session whenever the session goes first: it
  // expires, somebody signs out everywhere, Redis restarts, or a developer
  // tears the stack down. The browser is then holding a cookie that resolves
  // to nothing, and the proxy cannot tell, because there is no Redis on the
  // edge.
  //
  // That is the state where this redirect and the pages disagree, and the
  // disagreement is a trap rather than a nuisance:
  //
  //   /workspace + dead cookie -> the page finds no session -> /sign-in?returnTo=
  //   /sign-in   + dead cookie -> this line sees a cookie    -> /workspace
  //
  // ERR_TOO_MANY_REDIRECTS, and the person is locked out of every page of the
  // product including the one that would sign them in again. The only escape
  // is clearing cookies by hand.
  //
  // `returnTo` is the signal that ends it. It means a protected surface sent
  // this visitor here, which is to say something has just read the session and
  // rejected it, so sending them back is sending them to be rejected again.
  // Rendering sign-in is both the loop-breaker and the honest answer.
  //
  // Gated here rather than fixed per page on purpose. Every protected surface
  // redirects to sign-in when the session does not resolve, the list of them
  // grows, and a fix that works only while every future page remembers is the
  // same bug waiting.
  const sentHereByAPage = request.nextUrl.searchParams.has('returnTo')

  if (hasSession && pathname === '/sign-in' && !sentHereByAPage) {
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
function withCsrfToken(
  request: NextRequest,
  response: NextResponse,
): NextResponse {
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
