'use server'

import { z } from 'zod'

import { getPlan } from '@/lib/billing/plan'
import { createClient } from '@/lib/supabase/server'

/**
 * Notification-preferences server action (ENT-76).
 *
 * The source of truth for a founder's notification settings. Validates the
 * payload, enforces the Pro gate server-side (a Free user can't enable the
 * Pro-only weekly briefing), and upserts the caller's own row — RLS
 * insert/update-own keeps a user scoped to their own preferences.
 */

export type SaveResult = { ok: true } | { ok: false; error: string; upgrade?: boolean }

const TIME = /^([01]\d|2[0-3]):[0-5]\d$/

// Empty string → null (cleared field); otherwise must be HH:MM.
const optionalTime = z
  .string()
  .trim()
  .transform((v) => (v === '' ? null : v))
  .refine((v) => v === null || TIME.test(v), { message: 'Time must be HH:MM' })

const schema = z.object({
  email: z.string().trim().email('Enter a valid email address'),
  minSeverityForEmail: z.enum(['low', 'medium', 'high', 'critical']),
  weeklyBriefingEnabled: z.boolean(),
  deadlineAlertsEnabled: z.boolean(),
  quietHoursStart: optionalTime,
  quietHoursEnd: optionalTime,
  timezone: z.string().trim().min(1, 'Choose a timezone'),
})

export type NotificationSettingsInput = z.input<typeof schema>

export async function updateNotificationPreferences(
  input: NotificationSettingsInput,
): Promise<SaveResult> {
  const parsed = schema.safeParse(input)
  if (!parsed.success) {
    return { ok: false, error: parsed.error.issues[0]?.message ?? 'Invalid settings' }
  }
  const prefs = parsed.data

  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  // Pro gate (AC: weekly briefing is true for Pro / false for Free). A Free user
  // can't switch it on — authoritative here, not just in the UI.
  if (prefs.weeklyBriefingEnabled && (await getPlan(supabase, user.id)) !== 'pro') {
    return { ok: false, error: 'The weekly briefing is a Pro feature.', upgrade: true }
  }

  const { error } = await supabase.from('notification_preferences').upsert(
    {
      user_id: user.id,
      email: prefs.email,
      min_severity_for_email: prefs.minSeverityForEmail,
      weekly_briefing_enabled: prefs.weeklyBriefingEnabled,
      deadline_alerts_enabled: prefs.deadlineAlertsEnabled,
      quiet_hours_start: prefs.quietHoursStart,
      quiet_hours_end: prefs.quietHoursEnd,
      timezone: prefs.timezone,
    },
    { onConflict: 'user_id' },
  )
  if (error) return { ok: false, error: error.message }

  return { ok: true }
}
