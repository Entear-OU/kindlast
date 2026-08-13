/**
 * The double-submit CSRF token for POST /auth/logout.
 *
 * Deliberately NOT httpOnly, which looks wrong and is the whole mechanism: the
 * page has to read it to put it in the form body, and a cross-site attacker
 * cannot, because reading a cookie requires same-origin script. The browser
 * will happily send the cookie on a forged POST; it cannot produce the
 * matching body field.
 *
 * This carries no authority of its own. It proves a request came from a page
 * on this origin, nothing more, so a leaked value is worth nothing without a
 * session cookie alongside it.
 */
import { cookies } from 'next/headers'
import { randomToken } from './pkce'

export const CSRF_COOKIE = 'kindlast_csrf'

/**
 * Returns the current token, minting one if there is none.
 *
 * Called from a server component that renders a form, so the cookie and the
 * rendered value are always the same token.
 */
export async function csrfToken(): Promise<string> {
  const jar = await cookies()
  const existing = jar.get(CSRF_COOKIE)?.value
  if (existing) return existing

  const token = randomToken(32)
  jar.set(CSRF_COOKIE, token, {
    httpOnly: false,
    sameSite: 'lax',
    secure: process.env.NODE_ENV === 'production',
    path: '/',
  })
  return token
}
