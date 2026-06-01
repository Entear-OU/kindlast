/**
 * Comms dispatcher — drains the notification outbox (ENT-73).
 *
 * Reads `pending` rows from `notification_outbox`, and for each loads the
 * finding, the owner's email, and their notification preference. The severity
 * gate (`shouldNotifyByEmail`) decides send-vs-skip; a passing row is rendered
 * (`renderFindingEmail`) and handed to the configured `EmailProvider`, then
 * marked `sent`. A gated row is marked `skipped`; a send failure increments
 * `attempts`, records `last_error`, and stays `pending` for the next drain.
 *
 * Runs under the service role (bypasses RLS) — invoked by the cron-driven
 * `/api/notifications/dispatch` route. Pure-ish and DI-friendly: the email
 * provider, base URL, token secret and clock are all injected, so it's testable
 * with an in-memory provider against the live stack.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import { getPlan } from '@/lib/billing/plan'
import type { EmailProvider } from '@/lib/email/types'
import type { FindingSeverity } from '@/lib/feed/findings'
import { renderFindingEmail, type FindingEmailInput } from '@/lib/notifications/finding-email'
import { shouldNotifyByEmail, type EmailFrequency } from '@/lib/notifications/preferences'

const DEFAULT_LIMIT = 50
const DEFAULT_FREQUENCY: EmailFrequency = 'daily'

const FINDING_COLUMNS =
  'id,detected,severity,proposed_action,regulatory_obligation,citation_url,effort_estimate,user_id'

interface OutboxRow {
  id: string
  finding_id: string
  user_id: string
}

type FindingRow = FindingEmailInput & { severity: FindingSeverity; user_id: string }

export interface DispatchOptions {
  supabase: SupabaseClient
  emailProvider: EmailProvider
  baseUrl: string
  tokenSecret: string
  /** Epoch seconds "now"; defaults to the wall clock. Injectable for tests. */
  nowSeconds?: number
  /** Max rows to drain in one pass. */
  limit?: number
  /** Restrict the drain to one user's queue. Keeps tests hermetic; also the
   *  seam a future per-user digest would use. Unset drains everyone. */
  userId?: string
}

export interface DispatchSummary {
  processed: number
  sent: number
  skipped: number
  failed: number
}

export async function dispatchPendingNotifications(
  options: DispatchOptions,
): Promise<DispatchSummary> {
  const {
    supabase,
    emailProvider,
    baseUrl,
    tokenSecret,
    limit = DEFAULT_LIMIT,
  } = options
  const nowSeconds = options.nowSeconds ?? Math.floor(Date.now() / 1000)

  const summary: DispatchSummary = { processed: 0, sent: 0, skipped: 0, failed: 0 }

  let pending = supabase
    .from('notification_outbox')
    .select('id,finding_id,user_id')
    .eq('status', 'pending')
    .eq('channel', 'email')
  if (options.userId) pending = pending.eq('user_id', options.userId)

  const { data: rows, error } = await pending
    .order('created_at', { ascending: true })
    .limit(limit)

  if (error) throw new Error(`dispatch: failed to read outbox: ${error.message}`)

  for (const row of (rows ?? []) as OutboxRow[]) {
    summary.processed += 1
    try {
      const outcome = await processRow(row, { supabase, emailProvider, baseUrl, tokenSecret, nowSeconds })
      summary[outcome] += 1
    } catch (err) {
      summary.failed += 1
      await recordFailure(supabase, row.id, err)
    }
  }

  return summary
}

async function processRow(
  row: OutboxRow,
  ctx: {
    supabase: SupabaseClient
    emailProvider: EmailProvider
    baseUrl: string
    tokenSecret: string
    nowSeconds: number
  },
): Promise<'sent' | 'skipped'> {
  const { supabase, emailProvider, baseUrl, tokenSecret, nowSeconds } = ctx

  const { data: finding, error: findingError } = await supabase
    .from('findings')
    .select(FINDING_COLUMNS)
    .eq('id', row.finding_id)
    .single<FindingRow>()
  if (findingError || !finding) {
    throw new Error(`finding ${row.finding_id} not found: ${findingError?.message ?? 'missing'}`)
  }

  const frequency = await loadFrequency(supabase, row.user_id)
  if (!shouldNotifyByEmail(finding.severity, frequency)) {
    await mark(supabase, row.id, { status: 'skipped' })
    return 'skipped'
  }

  const email = await loadEmail(supabase, row.user_id)
  if (!email) {
    // No deliverable address — skip rather than fail forever.
    await mark(supabase, row.id, { status: 'skipped', last_error: 'no email address' })
    return 'skipped'
  }

  // Free recipients get the weekly-briefing upsell footer (ENT-74).
  const plan = await getPlan(supabase, row.user_id)
  const { subject, html, text } = renderFindingEmail(finding, { baseUrl, tokenSecret, nowSeconds, plan })
  await emailProvider.send({ to: email, subject, html, text })

  await mark(supabase, row.id, { status: 'sent', sent_at: new Date().toISOString() })
  return 'sent'
}

async function loadFrequency(
  supabase: SupabaseClient,
  userId: string,
): Promise<EmailFrequency> {
  const { data } = await supabase
    .from('notification_preferences')
    .select('email_frequency')
    .eq('user_id', userId)
    .maybeSingle<{ email_frequency: EmailFrequency }>()
  return data?.email_frequency ?? DEFAULT_FREQUENCY
}

async function loadEmail(supabase: SupabaseClient, userId: string): Promise<string | null> {
  const { data, error } = await supabase.auth.admin.getUserById(userId)
  if (error || !data?.user?.email) return null
  return data.user.email
}

async function mark(
  supabase: SupabaseClient,
  outboxId: string,
  patch: { status: string; sent_at?: string; last_error?: string },
): Promise<void> {
  await supabase.from('notification_outbox').update(patch).eq('id', outboxId)
}

async function recordFailure(
  supabase: SupabaseClient,
  outboxId: string,
  err: unknown,
): Promise<void> {
  const message = err instanceof Error ? err.message : String(err)
  // Bump attempts and keep the row pending for the next pass.
  const { data } = await supabase
    .from('notification_outbox')
    .select('attempts')
    .eq('id', outboxId)
    .maybeSingle<{ attempts: number }>()
  await supabase
    .from('notification_outbox')
    .update({ attempts: (data?.attempts ?? 0) + 1, last_error: message })
    .eq('id', outboxId)
}
