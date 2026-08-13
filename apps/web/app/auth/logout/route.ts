import { NextResponse, type NextRequest } from 'next/server'
import { browserEndSessionEndpoint, discoverProvider } from '@/lib/auth/oidc'
import { revokeToken } from '@/lib/auth/flow'
import {
  clearSessionCookie,
  destroySession,
  readSession,
  SESSION_COOKIE,
  sessionCookieOptions,
} from '@/lib/auth/session'
import { safeEqual } from '@/lib/auth/pkce'
import { CSRF_COOKIE } from '@/lib/auth/csrf'

/**
 * Sign out. POST only, and this is not pedantry.
 *
 * A `GET /auth/logout` is the same bug class as a one-tap link in an email:
 * link prefetchers, mail scanners and security appliances issue GETs, and any
 * of them can silently end a session someone is in the middle of using. The
 * rule is the same wherever a GET would cause an effect, so it is applied the
 * same way (§1.7).
 *
 * There is deliberately no GET export in this file. A request that arrives as
 * a GET gets 405 from the framework, which is the correct answer rather than a
 * redirect that quietly does nothing.
 */
export async function POST(request: NextRequest) {
  const { origin } = request.nextUrl

  // Double-submit CSRF: the token is in an httpOnly-free cookie and must be
  // echoed in the form body. A cross-site POST can cause the browser to send
  // the cookie, but cannot read it to put it in the body.
  const form = await request.formData().catch(() => null)
  const submitted = form?.get('csrf')
  const expected = request.cookies.get(CSRF_COOKIE)?.value

  if (typeof submitted !== 'string' || !expected || !safeEqual(submitted, expected)) {
    return NextResponse.json({ error: 'invalid csrf token' }, { status: 403 })
  }

  const sessionId = request.cookies.get(SESSION_COOKIE)?.value
  let idToken: string | null = null

  if (sessionId) {
    const session = await readSession(sessionId)
    idToken = session?.idToken ?? null

    // Destroy first. Everything after this is best effort, and the person is
    // signed out of this application whether or not it succeeds.
    await destroySession(sessionId)

    if (session?.refreshToken) {
      await revokeToken(session.refreshToken)
    }
  }

  const response = NextResponse.redirect(await endSessionUrl(origin, idToken), { status: 303 })
  response.cookies.set(SESSION_COOKIE, '', { ...sessionCookieOptions(), maxAge: 0 })
  await clearSessionCookie()
  return response
}

/**
 * RP-initiated logout, so the session ends at the identity provider too.
 *
 * Without it, signing out here and clicking sign-in again walks straight back
 * in through the IdP's own session, which looks like sign-out being broken.
 */
async function endSessionUrl(origin: string, idToken: string | null): Promise<string> {
  try {
    const provider = await discoverProvider()
    const endSession = browserEndSessionEndpoint(provider)
    if (!endSession) return `${origin}/sign-in`

    const url = new URL(endSession)
    url.searchParams.set('post_logout_redirect_uri', `${origin}/sign-in`)
    if (idToken) {
      url.searchParams.set('id_token_hint', idToken)
    }
    return url.toString()
  } catch {
    return `${origin}/sign-in`
  }
}
