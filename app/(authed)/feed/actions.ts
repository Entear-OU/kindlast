'use server'

import { revalidatePath } from 'next/cache'

import { getPlan } from '@/lib/billing/plan'
import { DEFAULT_SNOOZE_DAYS } from '@/lib/feed/findings'
import { createClient } from '@/lib/supabase/server'

/**
 * Server actions for the Agent feed (ENT-63) — Approve / Reject / Snooze.
 *
 * The server action is the source of truth: the tier gate and the ownership /
 * status rules live here and in the SECURITY DEFINER RPCs (whose actor is
 * auth.uid()), so a client cannot bypass them. Each write delegates to its RPC
 * and revalidates the feed; the client layers the optimistic update + toast.
 *
 * Approve carries an `upgrade` flag instead of an opaque error so the UI can
 * raise the Pro prompt rather than a generic failure toast. The tier check goes
 * through the `getPlan` seam (ENT-63), now backed by the real `subscriptions`
 * lookup (ENT-81): Free users are gated, Pro users pass through.
 */

export type FeedActionResult =
  | { ok: true }
  | { ok: false; error: string; upgrade?: boolean }

const FEED_PATH = '/feed'

export async function approveFinding(id: string): Promise<FeedActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  // Tier gate (AC: free users see the Pro upgrade prompt instead of firing the
  // Executor). Authoritative here, not in the client.
  if ((await getPlan(supabase, user.id)) !== 'pro') {
    return { ok: false, error: 'Approving a finding is a Pro feature.', upgrade: true }
  }

  const { error } = await supabase.rpc('approve_finding', {
    p_finding_id: id,
    p_approving_user_id: user.id,
  })
  if (error) return { ok: false, error: error.message }

  revalidatePath(FEED_PATH)
  return { ok: true }
}

export async function rejectFinding(id: string, reason?: string): Promise<FeedActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  const { error } = await supabase.rpc('reject_finding', {
    p_finding_id: id,
    p_reason: reason ?? null,
  })
  if (error) return { ok: false, error: error.message }

  revalidatePath(FEED_PATH)
  return { ok: true }
}

export async function snoozeFinding(
  id: string,
  days: number = DEFAULT_SNOOZE_DAYS,
): Promise<FeedActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  const { error } = await supabase.rpc('snooze_finding', {
    p_finding_id: id,
    p_days: days,
  })
  if (error) return { ok: false, error: error.message }

  revalidatePath(FEED_PATH)
  return { ok: true }
}
