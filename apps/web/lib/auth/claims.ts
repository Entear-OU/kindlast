/**
 * Reading claims out of an access token, for the two things `web` legitimately
 * needs from one.
 *
 * # This does not verify anything, and that is deliberate
 *
 * `web` received this token seconds ago, over TLS, on the back channel, from
 * the token endpoint, in exchange for a code and a client secret. It is not
 * attacker-supplied input at this point, so decoding it to read the subject is
 * safe.
 *
 * What `web` must never do is treat that as authorization. **core-api verifies
 * every token itself** — signature, issuer, audience, expiry, revocation and
 * scope — and it does not care that `web` looked at one first. If this file
 * ever grows a function that decides whether someone may do something, that
 * decision is in the wrong service.
 */

interface Claims {
  sub?: string
  exp?: number
}

function decode(token: string): Claims | null {
  const parts = token.split('.')
  if (parts.length !== 3) return null

  try {
    const payload = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    const padded = payload.padEnd(payload.length + ((4 - (payload.length % 4)) % 4), '=')
    return JSON.parse(atob(padded)) as Claims
  } catch {
    return null
  }
}

/** The subject, used to key the session. Null when the token is unreadable. */
export function subjectOf(token: string): string | null {
  return decode(token)?.sub ?? null
}

/** Expiry in epoch seconds, used to refresh before a call rather than after a 401. */
export function expiryOf(token: string): number | null {
  return decode(token)?.exp ?? null
}
