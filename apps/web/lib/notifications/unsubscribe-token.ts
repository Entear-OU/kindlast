/**
 * Signed unsubscribe tokens for the weekly briefing (ENT-74).
 *
 * The briefing email's footer carries a one-tap opt-out link. Like the
 * finding-action token (action-token.ts) it's an HMAC-SHA256-signed, expiring
 * token — `base64url(payloadJson).base64url(hmac)` — but keyed on the user and a
 * scope rather than a finding, so `/api/notifications/unsubscribe` can flip the
 * preference without a session.
 */

import { createHmac, timingSafeEqual } from 'node:crypto'

export type UnsubscribeScope = 'weekly_briefing'

/** Opt-out links are long-lived — a briefing can sit in an inbox for weeks. */
export const UNSUBSCRIBE_TOKEN_TTL_SECONDS = 180 * 24 * 60 * 60 // 180 days

export interface UnsubscribeTokenPayload {
  userId: string
  scope: UnsubscribeScope
  exp: number
}

export interface BuildUnsubscribeTokenInput {
  userId: string
  scope: UnsubscribeScope
  /** Epoch seconds "now" — passed in so callers control the clock (testable). */
  nowSeconds: number
  ttlSeconds?: number
}

function base64url(input: Buffer | string): string {
  return Buffer.from(input).toString('base64url')
}

function hmac(payloadSegment: string, secret: string): Buffer {
  return createHmac('sha256', secret).update(payloadSegment).digest()
}

export function signUnsubscribeToken(input: BuildUnsubscribeTokenInput, secret: string): string {
  const payload: UnsubscribeTokenPayload = {
    userId: input.userId,
    scope: input.scope,
    exp: input.nowSeconds + (input.ttlSeconds ?? UNSUBSCRIBE_TOKEN_TTL_SECONDS),
  }
  const payloadSegment = base64url(JSON.stringify(payload))
  const signature = base64url(hmac(payloadSegment, secret))
  return `${payloadSegment}.${signature}`
}

export type VerifyUnsubscribeResult =
  | { valid: true; payload: UnsubscribeTokenPayload }
  | { valid: false; reason: 'malformed' | 'bad_signature' | 'expired' }

export function verifyUnsubscribeToken(
  token: string,
  secret: string,
  nowSeconds: number,
): VerifyUnsubscribeResult {
  const parts = token.split('.')
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return { valid: false, reason: 'malformed' }
  }
  const [payloadSegment, signatureSegment] = parts

  const expected = hmac(payloadSegment, secret)
  let provided: Buffer
  try {
    provided = Buffer.from(signatureSegment, 'base64url')
  } catch {
    return { valid: false, reason: 'malformed' }
  }
  if (provided.length !== expected.length || !timingSafeEqual(provided, expected)) {
    return { valid: false, reason: 'bad_signature' }
  }

  let payload: UnsubscribeTokenPayload
  try {
    payload = JSON.parse(Buffer.from(payloadSegment, 'base64url').toString('utf8'))
  } catch {
    return { valid: false, reason: 'malformed' }
  }
  if (
    typeof payload?.userId !== 'string' ||
    payload.scope !== 'weekly_briefing' ||
    typeof payload.exp !== 'number'
  ) {
    return { valid: false, reason: 'malformed' }
  }
  if (payload.exp <= nowSeconds) {
    return { valid: false, reason: 'expired' }
  }
  return { valid: true, payload }
}

/** The absolute one-tap unsubscribe URL for a user + scope. */
export function buildUnsubscribeUrl(
  baseUrl: string,
  input: BuildUnsubscribeTokenInput,
  secret: string,
): string {
  const token = signUnsubscribeToken(input, secret)
  const url = new URL('/api/notifications/unsubscribe', baseUrl)
  url.searchParams.set('token', token)
  return url.toString()
}
