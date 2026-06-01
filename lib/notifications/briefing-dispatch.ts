/**
 * Weekly briefing dispatcher (ENT-74).
 *
 * Invoked hourly by /api/notifications/briefing. For each onboarded user it
 * decides whether *right now* is Monday 09:00 in that user's timezone; if so (and
 * they're Pro, opted in, and not already briefed this week) it builds and sends
 * the posture digest. The once-per-week guarantee is the weekly_briefing_log
 * primary key (user_id, period_start) — claimed before send so concurrent ticks
 * can't double-send.
 *
 * Runs under the service role. The email provider, base URL, token secret and
 * clock are injected; `force` + `userId` make it deterministic in tests without
 * waiting for a real Monday.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import { getPlan } from '@/lib/billing/plan'
import type { EmailProvider } from '@/lib/email/types'
import { buildBriefing } from '@/lib/notifications/briefing'
import { renderBriefingEmail } from '@/lib/notifications/briefing-email'

const DEFAULT_TIMEZONE = 'Europe/Tallinn'
const BRIEFING_HOUR = 9 // 09:00 local
const MONDAY = 'Mon'

export interface BriefingDispatchOptions {
  supabase: SupabaseClient
  emailProvider: EmailProvider
  baseUrl: string
  tokenSecret: string
  /** Epoch ms "now"; defaults to the wall clock. Injectable for tests. */
  nowMs?: number
  /** Restrict to one user (hermetic tests). Unset evaluates every onboarded user. */
  userId?: string
  /** Bypass the Monday-09:00 window gate (tests). Pro + opt-in + dedup still apply. */
  force?: boolean
}

export interface BriefingDispatchSummary {
  processed: number
  sent: number
  skipped: number
  failed: number
}

interface BriefingWindow {
  isWindow: boolean
  /** The local calendar date (YYYY-MM-DD) — the Monday when in-window. */
  periodStart: string
}

/**
 * Is `nowMs` Monday in the 09:00 hour of `timeZone`, and what's the local date?
 * Uses Intl with the timeZone option so DST is handled by the platform.
 */
export function briefingWindow(nowMs: number, timeZone: string): BriefingWindow {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    weekday: 'short',
    hour: '2-digit',
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date(nowMs))

  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  const weekday = get('weekday')
  const hour = Number(get('hour')) % 24 // some engines emit '24' at midnight
  const periodStart = `${get('year')}-${get('month')}-${get('day')}`

  return { isWindow: weekday === MONDAY && hour === BRIEFING_HOUR, periodStart }
}

export async function dispatchWeeklyBriefing(
  options: BriefingDispatchOptions,
): Promise<BriefingDispatchSummary> {
  const { supabase, emailProvider, baseUrl, tokenSecret } = options
  const nowMs = options.nowMs ?? Date.now()
  const nowSeconds = Math.floor(nowMs / 1000)

  const summary: BriefingDispatchSummary = { processed: 0, sent: 0, skipped: 0, failed: 0 }

  // Candidate set: onboarded users (those with a compliance profile).
  let profileQuery = supabase.from('compliance_profiles').select('user_id')
  if (options.userId) profileQuery = profileQuery.eq('user_id', options.userId)
  const { data: profiles, error: profilesError } = await profileQuery
  if (profilesError) throw new Error(`briefing: failed to read profiles: ${profilesError.message}`)

  const userIds = [...new Set(((profiles ?? []) as { user_id: string }[]).map((p) => p.user_id))]
  if (userIds.length === 0) return summary

  // Preferences in one read; a missing row means defaults (enabled, default tz).
  let prefsQuery = supabase
    .from('notification_preferences')
    .select('user_id,timezone,weekly_briefing_enabled')
  if (options.userId) prefsQuery = prefsQuery.eq('user_id', options.userId)
  const { data: prefRows, error: prefsError } = await prefsQuery
  if (prefsError) throw new Error(`briefing: failed to read preferences: ${prefsError.message}`)

  const prefs = new Map(
    ((prefRows ?? []) as { user_id: string; timezone: string | null; weekly_briefing_enabled: boolean }[]).map(
      (r) => [r.user_id, r],
    ),
  )

  for (const userId of userIds) {
    summary.processed += 1
    const pref = prefs.get(userId)
    const timezone = pref?.timezone ?? DEFAULT_TIMEZONE
    const enabled = pref?.weekly_briefing_enabled ?? true

    if (!enabled) {
      summary.skipped += 1
      continue
    }

    const { isWindow, periodStart } = briefingWindow(nowMs, timezone)
    if (!options.force && !isWindow) {
      summary.skipped += 1
      continue
    }

    if ((await getPlan(supabase, userId)) !== 'pro') {
      summary.skipped += 1
      continue
    }

    // Claim this week's slot before sending — dedup + concurrency guard.
    const claimed = await claimWeek(supabase, userId, periodStart)
    if (!claimed) {
      summary.skipped += 1
      continue
    }

    try {
      const email = await loadEmail(supabase, userId)
      if (!email) {
        await releaseWeek(supabase, userId, periodStart)
        summary.skipped += 1
        continue
      }
      const data = await buildBriefing(supabase, userId)
      const { subject, html, text } = renderBriefingEmail(data, {
        userId,
        baseUrl,
        tokenSecret,
        nowSeconds,
      })
      await emailProvider.send({ to: email, subject, html, text })
      summary.sent += 1
    } catch {
      // Release the claim so a later tick retries this week.
      await releaseWeek(supabase, userId, periodStart)
      summary.failed += 1
    }
  }

  return summary
}

async function claimWeek(
  supabase: SupabaseClient,
  userId: string,
  periodStart: string,
): Promise<boolean> {
  const { data, error } = await supabase
    .from('weekly_briefing_log')
    .upsert({ user_id: userId, period_start: periodStart }, { onConflict: 'user_id,period_start', ignoreDuplicates: true })
    .select('user_id')
  if (error) throw new Error(`briefing: claim failed: ${error.message}`)
  return (data ?? []).length > 0
}

async function releaseWeek(
  supabase: SupabaseClient,
  userId: string,
  periodStart: string,
): Promise<void> {
  await supabase
    .from('weekly_briefing_log')
    .delete()
    .eq('user_id', userId)
    .eq('period_start', periodStart)
}

async function loadEmail(supabase: SupabaseClient, userId: string): Promise<string | null> {
  const { data, error } = await supabase.auth.admin.getUserById(userId)
  if (error || !data?.user?.email) return null
  return data.user.email
}
