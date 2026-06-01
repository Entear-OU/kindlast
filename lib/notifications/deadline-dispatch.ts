/**
 * Deadline alert dispatcher (ENT-75).
 *
 * Invoked daily by /api/notifications/deadline-alerts. For each pending
 * deadline/DSAR finding it reads the live days-remaining (refreshed daily by the
 * Watcher→Analyst pipeline), computes the active 30/14/7/1-day threshold, and —
 * if that threshold hasn't fired yet — sends the deadline email. The
 * deadline_alert_log primary key (finding_id, threshold) is the per-threshold
 * dedup guard, claimed before send so a finding alerts once per threshold.
 *
 * Runs under the service role. Mirrors the claim-before-send + release-on-failure
 * shape of briefing-dispatch.ts; `userId` scopes it for hermetic tests.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import type { EmailProvider } from '@/lib/email/types'
import { activeThreshold, type DeadlineThreshold } from '@/lib/notifications/deadline-alert'
import { renderDeadlineEmail, type DeadlineEmailInput } from '@/lib/notifications/deadline-email'
import { loadResolvedPreferences } from '@/lib/notifications/load-preferences'

const DEADLINE_KINDS = ['deadline', 'dsar']

const FINDING_COLUMNS =
  'id,user_id,detected,regulatory_obligation,citation_url,proposed_action,metadata'

interface DeadlineFindingRow extends DeadlineEmailInput {
  user_id: string
  metadata: {
    signal_kind?: string
    signal_metadata?: {
      days_remaining?: number
      effective_date?: string
      response_due_at?: string
    }
  } | null
}

export interface DeadlineDispatchOptions {
  supabase: SupabaseClient
  emailProvider: EmailProvider
  baseUrl: string
  tokenSecret: string
  /** Epoch seconds "now"; defaults to the wall clock. */
  nowSeconds?: number
  /** Restrict to one user (hermetic tests). Unset evaluates everyone. */
  userId?: string
}

export interface DeadlineDispatchSummary {
  processed: number
  sent: number
  skipped: number
  failed: number
}

export async function dispatchDeadlineAlerts(
  options: DeadlineDispatchOptions,
): Promise<DeadlineDispatchSummary> {
  const { supabase, emailProvider, baseUrl, tokenSecret } = options
  const nowSeconds = options.nowSeconds ?? Math.floor(Date.now() / 1000)
  const summary: DeadlineDispatchSummary = { processed: 0, sent: 0, skipped: 0, failed: 0 }

  let query = supabase
    .from('findings')
    .select(FINDING_COLUMNS)
    .eq('status', 'pending')
    .in('metadata->>signal_kind', DEADLINE_KINDS)
  if (options.userId) query = query.eq('user_id', options.userId)

  const { data: rows, error } = await query
  if (error) throw new Error(`deadline dispatch: failed to read findings: ${error.message}`)

  for (const finding of (rows ?? []) as DeadlineFindingRow[]) {
    summary.processed += 1

    const sm = finding.metadata?.signal_metadata
    const days = sm?.days_remaining
    if (typeof days !== 'number') {
      summary.skipped += 1
      continue
    }

    const threshold = activeThreshold(days)
    if (threshold === null) {
      summary.skipped += 1
      continue
    }

    // Opt-out + recipient (ENT-76). Check before claiming the threshold so a
    // re-enable later can still fire it.
    const prefs = await loadResolvedPreferences(supabase, finding.user_id)
    if (!prefs.deadlineAlertsEnabled) {
      summary.skipped += 1
      continue
    }

    const claimed = await claimThreshold(supabase, finding.id, threshold, finding.user_id)
    if (!claimed) {
      summary.skipped += 1
      continue
    }

    try {
      if (!prefs.email) {
        await releaseThreshold(supabase, finding.id, threshold)
        summary.skipped += 1
        continue
      }
      const dueDate = sm?.effective_date ?? sm?.response_due_at ?? null
      const { subject, html, text } = renderDeadlineEmail(finding, {
        baseUrl,
        tokenSecret,
        nowSeconds,
        daysRemaining: days,
        dueDate,
      })
      await emailProvider.send({ to: prefs.email, subject, html, text })
      summary.sent += 1
    } catch {
      await releaseThreshold(supabase, finding.id, threshold)
      summary.failed += 1
    }
  }

  return summary
}

async function claimThreshold(
  supabase: SupabaseClient,
  findingId: string,
  threshold: DeadlineThreshold,
  userId: string,
): Promise<boolean> {
  const { data, error } = await supabase
    .from('deadline_alert_log')
    .upsert(
      { finding_id: findingId, threshold, user_id: userId },
      { onConflict: 'finding_id,threshold', ignoreDuplicates: true },
    )
    .select('finding_id')
  if (error) throw new Error(`deadline dispatch: claim failed: ${error.message}`)
  return (data ?? []).length > 0
}

async function releaseThreshold(
  supabase: SupabaseClient,
  findingId: string,
  threshold: DeadlineThreshold,
): Promise<void> {
  await supabase
    .from('deadline_alert_log')
    .delete()
    .eq('finding_id', findingId)
    .eq('threshold', threshold)
}
