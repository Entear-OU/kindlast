'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { updateNotificationsAction } from '@/app/(authed)/o/[org]/settings/actions'
import { idle, type ActionState } from '@/lib/org/action-state'
import type {
  NotificationChannel,
  NotificationPreferences,
} from '@/lib/notifications/client'

const SEVERITIES = ['low', 'medium', 'high', 'critical'] as const

/**
 * Notification preferences (ENT-209).
 *
 * Personal within the organisation, not organisation-wide, so there is no owner
 * gate here and no "manage" variant: every member edits their own row and the
 * policy on `notification_preferences` is what makes that true rather than this
 * component. Somebody in two organisations holds two sets and this page shows
 * the one for the organisation in the URL.
 *
 * The severity control is a floor rather than a switch. "Tell me everything"
 * and "tell me nothing" are both wrong for a compliance product: the first
 * trains people to ignore the mail, the second is how a critical finding goes
 * unread. A floor is the shape that lets somebody stay subscribed to the things
 * that matter.
 */
export function NotificationsForm({
  slug,
  preferences,
  channels,
}: {
  slug: string
  preferences: NotificationPreferences
  channels: NotificationChannel[]
}) {
  const [state, save] = useActionState<ActionState, FormData>(
    updateNotificationsAction.bind(null, slug),
    idle,
  )

  const email = channels.find((c) => c.id === 'email')

  return (
    <form action={save} className="space-y-6">
      {/* Said once, at the top, rather than disabling every control. A person
          whose deployment cannot send mail can still record what they want, and
          it takes effect the moment an operator configures SMTP. Greying the
          form out would lose that and explain nothing. */}
      {email && !email.available ? (
        <p
          role="status"
          className="rounded-md border border-border/60 bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
        >
          {email.unavailableReason}
        </p>
      ) : null}

      <div className="max-w-md">
        <Label htmlFor="notify-email">Send to</Label>
        <Input
          id="notify-email"
          name="email"
          type="email"
          autoComplete="off"
          defaultValue={preferences.email ?? ''}
          placeholder="the address you sign in with"
        />
        <p className="mt-1 text-xs text-muted-foreground">
          Leave this empty to use the address you sign in with. A compliance
          contact is often a shared mailbox nobody signs in as, which is why it
          is a separate field.
        </p>
      </div>

      <div className="max-w-md">
        <Label htmlFor="notify-severity">Email me about findings from</Label>
        <select
          id="notify-severity"
          name="minSeverityForEmail"
          defaultValue={preferences.minSeverityForEmail ?? 'medium'}
          className="mt-1 h-9 w-full rounded-md border border-border/60 bg-background px-2 text-sm"
        >
          {SEVERITIES.map((severity) => (
            <option key={severity} value={severity}>
              {severity} and above
            </option>
          ))}
        </select>
      </div>

      <fieldset className="space-y-2">
        <legend className="text-sm text-foreground">Also send me</legend>

        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            name="weeklyBriefingEnabled"
            defaultChecked={preferences.weeklyBriefingEnabled ?? true}
            className="size-4 rounded border-border/60"
          />
          A weekly briefing
        </label>

        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            name="deadlineAlertsEnabled"
            defaultChecked={preferences.deadlineAlertsEnabled ?? true}
            className="size-4 rounded border-border/60"
          />
          Alerts as a regulatory deadline approaches
        </label>
      </fieldset>

      <div className="max-w-md">
        <Label htmlFor="notify-timezone">Timezone</Label>
        <Input
          id="notify-timezone"
          name="timezone"
          autoComplete="off"
          defaultValue={preferences.timezone ?? 'Europe/Tallinn'}
          placeholder="Europe/Tallinn"
        />
        <p className="mt-1 text-xs text-muted-foreground">
          Decides when a briefing and any quiet hours fall for you. Yours rather
          than the organisation&rsquo;s: a consultant and their client are not
          always in the same country.
        </p>
      </div>

      <div className="flex max-w-md gap-3">
        <div className="grow">
          <Label htmlFor="notify-quiet-start">Quiet from</Label>
          <Input
            id="notify-quiet-start"
            name="quietHoursStart"
            type="time"
            defaultValue={preferences.quietHoursStart ?? ''}
          />
        </div>
        <div className="grow">
          <Label htmlFor="notify-quiet-end">Quiet until</Label>
          <Input
            id="notify-quiet-end"
            name="quietHoursEnd"
            type="time"
            defaultValue={preferences.quietHoursEnd ?? ''}
          />
        </div>
      </div>
      <p className="-mt-3 max-w-md text-xs text-muted-foreground">
        Leave both empty for no quiet hours. Overnight windows are fine, so
        22:00 to 07:00 means what it looks like.
      </p>

      <div className="flex items-center gap-3">
        <Button type="submit" variant="outline" size="sm">
          Save notification settings
        </Button>
        {state.status !== 'idle' ? (
          <p
            role={state.status === 'error' ? 'alert' : 'status'}
            className={
              state.status === 'error'
                ? 'text-xs text-destructive'
                : 'text-xs text-muted-foreground'
            }
          >
            {state.message}
          </p>
        ) : null}
      </div>
    </form>
  )
}
