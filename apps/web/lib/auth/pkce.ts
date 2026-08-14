/**
 * PKCE (RFC 7636) and the other random values the authorization code flow
 * needs.
 *
 * `web` is a confidential client and holds a secret, so PKCE is not strictly
 * required of it. It is used anyway, because the protection it gives is
 * against a different attack than the client secret is: an authorization code
 * intercepted in the redirect cannot be exchanged without the verifier, which
 * never leaves this server. Belt and braces on the one leg of the flow that
 * travels through a browser.
 */

/**
 * Verifier length. RFC 7636 §4.1 permits 43 to 128 characters; 64 random bytes
 * base64url-encoded lands at 86, comfortably inside and with more entropy than
 * the minimum.
 */
const VERIFIER_BYTES = 64

export interface Pkce {
  verifier: string
  challenge: string
  method: 'S256'
}

export async function createPkce(): Promise<Pkce> {
  const verifier = randomToken(VERIFIER_BYTES)
  return { verifier, challenge: await challengeFor(verifier), method: 'S256' }
}

/**
 * S256 only. `plain` is in the RFC and is worth nothing: it sends the verifier
 * itself as the challenge, so anyone who can see the authorization request can
 * complete the exchange. There is no configuration switch for it here on
 * purpose.
 */
export async function challengeFor(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(verifier),
  )
  return base64url(new Uint8Array(digest))
}

/**
 * A URL-safe random token, used for the PKCE verifier, the `state` parameter
 * and session ids.
 *
 * `crypto.getRandomValues` rather than `Math.random`, which is not a
 * cryptographic source and would make every one of those values guessable.
 */
export function randomToken(bytes = 32): string {
  return base64url(crypto.getRandomValues(new Uint8Array(bytes)))
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/**
 * Constant-time string comparison, for the `state` check on the callback.
 *
 * A plain `===` on a secret returns as soon as two characters differ, so the
 * time it takes leaks how much of a guess was correct. The value compared here
 * is short-lived, which makes the attack impractical rather than impossible,
 * and the correct comparison costs nothing.
 */
export function safeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false

  let difference = 0
  for (let i = 0; i < a.length; i++) {
    difference |= a.charCodeAt(i) ^ b.charCodeAt(i)
  }
  return difference === 0
}
