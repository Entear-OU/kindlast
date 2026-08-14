/**
 * Sessions: the tokens live here, the browser gets an id.
 *
 * This is the Backend-for-Frontend arrangement in §1.2, and the property it
 * buys is worth stating precisely, because it is easy to overclaim. **No
 * access or refresh token ever reaches the browser**, in any form, readable or
 * otherwise. The cookie carries a session id and nothing else. So XSS cannot
 * exfiltrate a token from this client, and "sign out everywhere" is a DEL
 * rather than a wait for expiry.
 *
 * That is a property of the WEB client, not of the architecture. Mobile is a
 * public client and will hold tokens on the device; third parties will hold
 * API keys. core-api validates every token itself regardless of which door it
 * came through, which is what makes those clients possible without revisiting
 * anything here.
 */
import { cookies } from 'next/headers'
import { randomToken } from './pkce'
import { redis } from './redis'

const PREFIX = 'web:session:'
export const SESSION_COOKIE = 'kindlast_session'

export interface Session {
  accessToken: string
  refreshToken: string | null
  /** Epoch seconds. Used to refresh before a call rather than after a 401. */
  expiresAt: number
  idToken: string | null
  subject: string
  /** The active organisation, sent to core-api as a header on every call. */
  orgId: string | null
}

/**
 * Sessions outlive an access token deliberately: the refresh token is what
 * keeps someone signed in, and it is good for thirty days with rotation
 * (§1.2). The Redis TTL bounds the session itself, not the token inside it.
 */
const TTL_SECONDS = 30 * 24 * 60 * 60

export async function createSession(session: Session): Promise<string> {
  const id = randomToken(32)
  await redis().set(PREFIX + id, JSON.stringify(session), 'EX', TTL_SECONDS)
  return id
}

export async function readSession(id: string): Promise<Session | null> {
  const raw = await redis().get(PREFIX + id)
  if (!raw) return null

  try {
    return JSON.parse(raw) as Session
  } catch {
    return null
  }
}

export async function updateSession(id: string, session: Session): Promise<void> {
  // KEEPTTL, so refreshing an access token does not silently extend the
  // session's own lifetime. Otherwise an active user is never signed out.
  await redis().set(PREFIX + id, JSON.stringify(session), 'KEEPTTL')
}

export async function destroySession(id: string): Promise<void> {
  await redis().del(PREFIX + id)
}

/**
 * The cookie carrying the session id.
 *
 * httpOnly, so client-side JavaScript cannot read it. SameSite=Lax, which
 * still allows the top-level redirect back from the authorization server while
 * refusing the cross-site POSTs CSRF depends on. Secure outside development,
 * because a session id on a plaintext connection is a session id anyone on the
 * path can take.
 */
export function sessionCookieOptions() {
  return {
    httpOnly: true,
    sameSite: 'lax' as const,
    secure: process.env.NODE_ENV === 'production',
    path: '/',
    maxAge: TTL_SECONDS,
  }
}

export async function setSessionCookie(id: string): Promise<void> {
  const jar = await cookies()
  jar.set(SESSION_COOKIE, id, sessionCookieOptions())
}

export async function clearSessionCookie(): Promise<void> {
  const jar = await cookies()
  jar.set(SESSION_COOKIE, '', { ...sessionCookieOptions(), maxAge: 0 })
}

export async function currentSessionId(): Promise<string | null> {
  const jar = await cookies()
  return jar.get(SESSION_COOKIE)?.value ?? null
}

export async function currentSession(): Promise<Session | null> {
  const id = await currentSessionId()
  return id ? readSession(id) : null
}
