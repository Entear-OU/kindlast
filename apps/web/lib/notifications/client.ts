/**
 * Notification preferences, from web's side (ENT-209).
 *
 * Preferences are personal within an organisation: the policies on
 * `notification_preferences` pin `user_id` to `app.current_user_id`, so these
 * calls carry no user id and could not act on somebody else's row if they did.
 * The organisation comes from the header, as everywhere.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

export interface NotificationPreferences {
  /** Empty means "use the address I sign in with". */
  email?: string
  /** One of low, medium, high, critical. */
  minSeverityForEmail?: string
  weeklyBriefingEnabled?: boolean
  deadlineAlertsEnabled?: boolean
  /** IANA name, e.g. Europe/Tallinn. */
  timezone?: string
  /** HH:MM, both empty meaning no quiet window. */
  quietHoursStart?: string
  quietHoursEnd?: string
}

export interface NotificationChannel {
  id: string
  displayName: string
  available: boolean
  unavailableReason?: string
}

export function getPreferences(accessToken: string, orgId: string) {
  return call<{ preferences?: NotificationPreferences }>(
    'kindlast.core.v1.NotificationService/GetNotificationPreferences',
    { accessToken, orgId },
  )
}

export function updatePreferences(
  accessToken: string,
  orgId: string,
  preferences: NotificationPreferences,
) {
  return call<{ preferences?: NotificationPreferences }>(
    'kindlast.core.v1.NotificationService/UpdateNotificationPreferences',
    { accessToken, orgId, body: { preferences } },
  )
}

/**
 * Which channels this deployment can actually deliver on (§18.3).
 *
 * Asked rather than assumed, so the settings page never renders a switch for
 * something that would queue forever. A console that offers Telegram on a
 * deployment with no Telegram is indistinguishable from a broken integration,
 * and generates support rather than value.
 */
export function getCapabilities(accessToken: string, orgId: string) {
  return call<{ channels?: NotificationChannel[] }>(
    'kindlast.core.v1.NotificationService/GetNotificationCapabilities',
    { accessToken, orgId },
  )
}
