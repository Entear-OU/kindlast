/**
 * Weekly-briefing unsubscribe handler (ENT-74).
 *
 * Verifies a signed unsubscribe token from a briefing footer and flips
 * `notification_preferences.weekly_briefing_enabled` to false for that user —
 * upserting the row if none exists yet. No session: runs under the service role,
 * keyed entirely by the token's user id. Extracted from the route so it's
 * testable without HTTP.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import { verifyUnsubscribeToken } from '@/lib/notifications/unsubscribe-token'

export type UnsubscribeResultKind = 'ok' | 'invalid' | 'expired'

export interface PerformUnsubscribeOptions {
  supabase: SupabaseClient
  token: string
  secret: string
  /** Epoch seconds "now"; defaults to the wall clock. */
  nowSeconds?: number
}

export async function performUnsubscribe({
  supabase,
  token,
  secret,
  nowSeconds = Math.floor(Date.now() / 1000),
}: PerformUnsubscribeOptions): Promise<UnsubscribeResultKind> {
  const verified = verifyUnsubscribeToken(token, secret, nowSeconds)
  if (!verified.valid) {
    return verified.reason === 'expired' ? 'expired' : 'invalid'
  }

  const { error } = await supabase
    .from('notification_preferences')
    .upsert(
      { user_id: verified.payload.userId, weekly_briefing_enabled: false },
      { onConflict: 'user_id' },
    )
  if (error) throw new Error(`unsubscribe: ${error.message}`)
  return 'ok'
}
