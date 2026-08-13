import { redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/console-shell'
import { NotificationSettings } from '@/components/settings/notification-settings'
import { getPlan } from '@/lib/billing/plan'
import {
  resolvePreferences,
  type NotificationPreferencesRow,
} from '@/lib/notifications/preferences'
import { createClient } from '@/lib/supabase/server'

/**
 * Notification settings (ENT-76) — closes the Comms epic. Loads the founder's
 * preferences row (RLS select-own) and plan, resolves defaults (email ← auth
 * email; weekly briefing default ← plan), and hands a view-model to the client
 * form. The form's Save goes through the server action.
 */
export default async function SettingsPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const [{ data: row }, plan] = await Promise.all([
    supabase
      .from('notification_preferences')
      .select(
        'email,min_severity_for_email,weekly_briefing_enabled,deadline_alerts_enabled,quiet_hours_start,quiet_hours_end,timezone',
      )
      .eq('user_id', user.id)
      .maybeSingle<NotificationPreferencesRow>(),
    getPlan(supabase, user.id),
  ])

  const prefs = resolvePreferences(row, { authEmail: user.email ?? null, plan })

  return (
    <ConsoleShell activeRail="settings" title="Settings">
      <NotificationSettings prefs={prefs} plan={plan} />
    </ConsoleShell>
  )
}
