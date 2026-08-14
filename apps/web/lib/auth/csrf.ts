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

export const CSRF_COOKIE = 'kindlast_csrf'

/**
 * Reads the current token. Does not mint one, and cannot.
 *
 * This is called from the server component that renders the sign-out form,
 * and Next refuses cookie writes during a page render: the earlier version
 * minted on a miss, which threw inside the authenticated layout and took every
 * page rendered in it down with it. The proxy issues the cookie instead, on
 * the response, before any page renders.
 *
 * Empty when there is no session, which is correct rather than a fallback: a
 * signed-out visitor has nothing to sign out of.
 */
export async function csrfToken(): Promise<string> {
  const jar = await cookies()
  return jar.get(CSRF_COOKIE)?.value ?? ''
}
