/**
 * One-tap finding action handler (ENT-73).
 *
 * Verifies a signed action token from a finding email and applies the action
 * through the same SECURITY DEFINER RPCs the in-app feed uses — but without a
 * session, so it runs under the service role and passes the finding owner as the
 * explicit acting user (the RPCs were parameterized for exactly this in the
 * ENT-73 migration).
 *
 * Approve mirrors the in-app Pro gate (app/(authed)/feed/actions.ts): a Free
 * owner gets an `upgrade` result instead of a silent approve. Reject and Snooze
 * are open to all tiers. Extracted from the route so it's testable without HTTP.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import { getPlan } from '@/lib/billing/plan'
import { DEFAULT_SNOOZE_DAYS } from '@/lib/feed/findings'
import { verifyActionToken, type FindingAction } from '@/lib/notifications/action-token'

export type ActionResultKind =
  | 'ok' // the action changed the finding
  | 'noop' // valid, but the finding was already in that state
  | 'upgrade' // approve blocked by the Free-tier gate
  | 'invalid' // malformed / bad signature
  | 'expired' // token past its TTL
  | 'not_found' // finding gone

export interface ActionResult {
  kind: ActionResultKind
  action?: FindingAction
}

interface FindingRow {
  id: string
  user_id: string
  status: string
}

export interface PerformActionOptions {
  supabase: SupabaseClient
  token: string
  secret: string
  /** Epoch seconds "now"; defaults to the wall clock. */
  nowSeconds?: number
}

export async function performFindingAction({
  supabase,
  token,
  secret,
  nowSeconds = Math.floor(Date.now() / 1000),
}: PerformActionOptions): Promise<ActionResult> {
  const verified = verifyActionToken(token, secret, nowSeconds)
  if (!verified.valid) {
    return { kind: verified.reason === 'expired' ? 'expired' : 'invalid' }
  }
  const { findingId, action, days } = verified.payload

  const { data: finding, error } = await supabase
    .from('findings')
    .select('id,user_id,status')
    .eq('id', findingId)
    .maybeSingle<FindingRow>()
  if (error || !finding) return { kind: 'not_found', action }

  switch (action) {
    case 'approve': {
      if (finding.status === 'approved') return { kind: 'noop', action }
      if ((await getPlan(supabase, finding.user_id)) !== 'pro') {
        return { kind: 'upgrade', action }
      }
      // approve_finding returns the created executor record's id (null for a
      // plain 'review' finding) — not a success flag — so the prior status above
      // is what distinguishes ok from noop.
      const { error: rpcError } = await supabase.rpc('approve_finding', {
        p_finding_id: findingId,
        p_approving_user_id: finding.user_id,
      })
      if (rpcError) throw new Error(`approve_finding: ${rpcError.message}`)
      return { kind: 'ok', action }
    }
    case 'reject': {
      const { data: changed, error: rpcError } = await supabase.rpc('reject_finding', {
        p_finding_id: findingId,
        p_reason: 'Rejected from email',
        p_acting_user_id: finding.user_id,
      })
      if (rpcError) throw new Error(`reject_finding: ${rpcError.message}`)
      return { kind: changed ? 'ok' : 'noop', action }
    }
    case 'snooze': {
      const { data: until, error: rpcError } = await supabase.rpc('snooze_finding', {
        p_finding_id: findingId,
        p_days: days ?? DEFAULT_SNOOZE_DAYS,
        p_acting_user_id: finding.user_id,
      })
      if (rpcError) throw new Error(`snooze_finding: ${rpcError.message}`)
      return { kind: until ? 'ok' : 'noop', action }
    }
  }
}
