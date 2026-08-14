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

/**
 * How close to expiry a token has to be before it is worth refreshing.
 *
 * Refreshing *before* expiry rather than after a 401 is the decision that
 * makes everything below simple. The user never meets the failure, no caller
 * has to learn to retry, and the concurrency case stops being dangerous:
 * a request that loses the race still holds a token with slack left on it.
 */
const REFRESH_SLACK_SECONDS = 60

/** Held only for the duration of a refresh, so two requests cannot both spend
 * the same refresh token. */
const REFRESH_LOCK_PREFIX = 'web:refresh:'

/**
 * Returns a session whose access token is usable, refreshing it if not.
 *
 * This exists because it did not, and the gap was found by looking rather
 * than by testing: a browser signed in the day before rendered the workspace
 * degraded, with core-api refusing a token that had expired eleven minutes
 * earlier while the refresh token sat here unused for another month. The
 * comment on `expiresAt` already promised this behaviour. Nothing did it.
 *
 * Failure is deliberately quiet. A transient error at the authorization
 * server must not destroy a session that is otherwise good: the call this was
 * for will fail, and the next one may well succeed.
 */
export async function ensureFreshSession(id: string, session: Session): Promise<Session> {
  const now = Math.floor(Date.now() / 1000)
  if (session.expiresAt - now > REFRESH_SLACK_SECONDS) return session
  if (!session.refreshToken) return session

  const lockKey = REFRESH_LOCK_PREFIX + id

  // Rotation makes a concurrent refresh dangerous rather than merely wasteful.
  // The second use of a rotated token is refused, and a server that treats
  // replay as theft revokes the whole grant, signing the person out everywhere.
  const acquired = await redis().set(lockKey, '1', 'EX', 10, 'NX')
  if (acquired !== 'OK') {
    // Someone else is refreshing. Re-read rather than wait: they may have
    // written already, and if they have not, the token still has slack on it.
    return (await readSession(id)) ?? session
  }

  try {
    // Imported here rather than at the top so the module graph stays free of
    // the network layer. `proxy.ts` imports this file and runs on the edge
    // runtime, where discovery and ioredis have no business being bundled.
    const { refreshTokens } = await import('./flow')
    const tokens = await refreshTokens(session.refreshToken)

    const refreshed: Session = {
      ...session,
      accessToken: tokens.accessToken,
      // Keep what we hold when the server does not rotate, and take the new
      // one when it does. Losing a rotated token strands the next refresh.
      refreshToken: tokens.refreshToken ?? session.refreshToken,
      idToken: tokens.idToken ?? session.idToken,
      expiresAt: tokens.expiresAt,
    }

    // Spread above, so `subject` and `orgId` survive. Neither comes back from
    // the token endpoint, and dropping orgId would silently un-scope every
    // subsequent call.
    await updateSession(id, refreshed)
    return refreshed
  } catch (error) {
    console.error('auth/session refresh', error)
    return session
  } finally {
    // Released even on failure, or one bad refresh blocks every later attempt
    // until the lock expires, turning a transient error into a stuck session.
    await redis().del(lockKey)
  }
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

/**
 * The signed-in person's session, with a token that works.
 *
 * The refresh happens here rather than at each call site on purpose. The bug
 * this closes was a caller not doing something it was supposed to, and the
 * fix that only works when every future caller remembers is the same bug
 * waiting. `readSession` stays a plain read for anything that wants one.
 */
export async function currentSession(): Promise<Session | null> {
  const id = await currentSessionId()
  if (!id) return null

  const session = await readSession(id)
  if (!session) return null

  return ensureFreshSession(id, session)
}
