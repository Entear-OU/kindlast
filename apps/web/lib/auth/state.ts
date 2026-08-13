/**
 * Pre-auth state: what has to survive the round trip to the authorization
 * server and back.
 *
 * In Redis rather than in a cookie, for one reason that matters and one that
 * is convenience. The PKCE verifier must never reach the browser, or the
 * protection it provides is gone: an intercepted authorization code plus the
 * verifier sitting in a cookie is a complete exchange. The convenience is that
 * a stashed invitation token then rides along for free.
 *
 * Ten minutes, because it exists only for the span of a redirect, a login form
 * and a redirect back. Anything longer is a window for a state value to be
 * replayed.
 */
import { redis } from './redis'

const PREFIX = 'web:preauth:'
const TTL_SECONDS = 600

export interface PreAuthState {
  /** The PKCE verifier. Never leaves this server. */
  verifier: string
  /** Where to send the user once they are signed in. */
  returnTo: string
  /**
   * An invitation token, when the flow started at /invite/{token}.
   *
   * This is why the state survives the round trip at all rather than being
   * regenerated on the way back: the ordering in §1.8 requires accepting the
   * invitation before the first /api/v1/me, and by the time the callback runs
   * the only place that token can have come from is here.
   */
  invitationToken?: string
  createdAt: number
}

export async function stashState(state: string, value: PreAuthState): Promise<void> {
  await redis().set(PREFIX + state, JSON.stringify(value), 'EX', TTL_SECONDS)
}

/**
 * Reads the state and deletes it in the same step.
 *
 * Single use, and that is the point rather than tidiness: a state value that
 * survives its callback can be replayed, and the whole job of the parameter is
 * to be usable exactly once. GETDEL is atomic, so two concurrent callbacks
 * cannot both succeed.
 */
export async function consumeState(state: string): Promise<PreAuthState | null> {
  const raw = await redis().getdel(PREFIX + state)
  if (!raw) return null

  try {
    return JSON.parse(raw) as PreAuthState
  } catch {
    // Unparseable is the same answer as absent: this flow cannot continue.
    return null
  }
}
