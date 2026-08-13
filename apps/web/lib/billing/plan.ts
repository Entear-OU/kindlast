import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * Billing tier lookup (seam introduced ENT-63, made real in ENT-81).
 *
 * The single place every tier decision flows through. Reads the caller's own
 * `subscriptions` row through an authenticated client, so the select-own RLS
 * policy is the source of truth. The signup trigger guarantees a row exists, but
 * this stays defensive: a missing row or a read error resolves to `free` — never
 * an accidental Pro unlock.
 */

export type Plan = 'free' | 'pro'

export async function getPlan(
  supabase: SupabaseClient,
  userId: string,
): Promise<Plan> {
  const { data, error } = await supabase
    .from('subscriptions')
    .select('plan')
    .eq('user_id', userId)
    .maybeSingle()

  if (error || !data) return 'free'
  return data.plan === 'pro' ? 'pro' : 'free'
}
