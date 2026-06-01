/**
 * Signed one-tap action tokens for finding emails (ENT-73).
 *
 * A founder acts on a finding straight from their inbox — no session. Each CTA
 * button carries an HMAC-SHA256-signed, expiring token that names the finding
 * and the action; the `/api/findings/act` route verifies it and calls the
 * matching SECURITY DEFINER RPC via the service role.
 *
 * Token format: `base64url(payloadJson).base64url(hmac)`. The HMAC is computed
 * over the payload segment with `NOTIFICATION_TOKEN_SECRET`, so tampering or a
 * wrong secret fails verification. A short-by-default TTL bounds replay.
 */

import { createHmac, timingSafeEqual } from 'node:crypto'

import { DEFAULT_SNOOZE_DAYS } from '@/lib/feed/findings'

export type FindingAction = 'approve' | 'reject' | 'snooze'

export const FINDING_ACTIONS: readonly FindingAction[] = ['approve', 'reject', 'snooze']

/** How long a one-tap link stays valid. Findings outlive this; a stale link 410s. */
export const ACTION_TOKEN_TTL_SECONDS = 30 * 24 * 60 * 60 // 30 days

export interface ActionTokenPayload {
  /** Finding id the action targets. */
  findingId: string
  action: FindingAction
  /** Snooze duration; only meaningful for `snooze`. */
  days?: number
  /** Expiry, epoch seconds. */
  exp: number
}

export interface BuildActionTokenInput {
  findingId: string
  action: FindingAction
  days?: number
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

/** Build a signed, expiring token for a one-tap finding action. */
export function signActionToken(input: BuildActionTokenInput, secret: string): string {
  const payload: ActionTokenPayload = {
    findingId: input.findingId,
    action: input.action,
    ...(input.action === 'snooze'
      ? { days: input.days ?? DEFAULT_SNOOZE_DAYS }
      : {}),
    exp: input.nowSeconds + (input.ttlSeconds ?? ACTION_TOKEN_TTL_SECONDS),
  }
  const payloadSegment = base64url(JSON.stringify(payload))
  const signature = base64url(hmac(payloadSegment, secret))
  return `${payloadSegment}.${signature}`
}

export type VerifyResult =
  | { valid: true; payload: ActionTokenPayload }
  | { valid: false; reason: 'malformed' | 'bad_signature' | 'expired' }

/** Verify a token's signature and expiry against `nowSeconds`. */
export function verifyActionToken(
  token: string,
  secret: string,
  nowSeconds: number,
): VerifyResult {
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

  let payload: ActionTokenPayload
  try {
    payload = JSON.parse(Buffer.from(payloadSegment, 'base64url').toString('utf8'))
  } catch {
    return { valid: false, reason: 'malformed' }
  }
  if (
    typeof payload?.findingId !== 'string' ||
    !FINDING_ACTIONS.includes(payload.action) ||
    typeof payload.exp !== 'number'
  ) {
    return { valid: false, reason: 'malformed' }
  }
  if (payload.exp <= nowSeconds) {
    return { valid: false, reason: 'expired' }
  }
  return { valid: true, payload }
}

/** The absolute one-tap URL for a finding action. */
export function buildActionUrl(
  baseUrl: string,
  input: BuildActionTokenInput,
  secret: string,
): string {
  const token = signActionToken(input, secret)
  const url = new URL('/api/findings/act', baseUrl)
  url.searchParams.set('token', token)
  return url.toString()
}
