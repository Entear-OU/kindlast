/**
 * Loads a user's resolved notification preferences for the Comms dispatchers
 * (ENT-76). The single IO seam over `notification_preferences` + the auth email,
 * so the three dispatchers share one fallback path. Pure resolution lives in
 * `resolvePreferences`; this just fetches and delegates.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import type { Plan } from '@/lib/billing/plan'
import {
  resolvePreferences,
  type NotificationPreferences,
  type NotificationPreferencesRow,
} from '@/lib/notifications/preferences'

const PREFERENCE_COLUMNS =
  'email,min_severity_for_email,weekly_briefing_enabled,deadline_alerts_enabled,quiet_hours_start,quiet_hours_end,timezone'

export async function loadAuthEmail(
  supabase: SupabaseClient,
  userId: string,
): Promise<string | null> {
  const { data, error } = await supabase.auth.admin.getUserById(userId)
  if (error || !data?.user?.email) return null
  return data.user.email
}

/**
 * The prefs row + auth-email fallback + plan-aware defaults, fully resolved.
 * `plan` only affects the weekly-briefing default for a user with no row, so
 * callers that don't read `weeklyBriefingEnabled` may leave it at 'free'.
 */
export async function loadResolvedPreferences(
  supabase: SupabaseClient,
  userId: string,
  plan: Plan = 'free',
): Promise<NotificationPreferences> {
  const { data: row } = await supabase
    .from('notification_preferences')
    .select(PREFERENCE_COLUMNS)
    .eq('user_id', userId)
    .maybeSingle<NotificationPreferencesRow>()
  const authEmail = await loadAuthEmail(supabase, userId)
  return resolvePreferences(row, { authEmail, plan })
}
