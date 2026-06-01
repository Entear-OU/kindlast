'use client'

import Link from 'next/link'
import { useState, useTransition } from 'react'
import { toast } from 'sonner'

import { updateNotificationPreferences } from '@/app/(authed)/settings/actions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { Plan } from '@/lib/billing/plan'
import type { NotificationPreferences, SeverityLevel } from '@/lib/notifications/preferences'

/**
 * Notification settings form (ENT-76) — the founder controls which severities
 * email them, the weekly briefing (Pro), deadline alerts, quiet hours, delivery
 * address and timezone. Each control is independent; Save upserts via the server
 * action, which is the authority on validation and the Pro gate.
 */

const SEVERITIES: { value: SeverityLevel; label: string; hint: string }[] = [
  { value: 'low', label: 'Low and up', hint: 'Everything — most email' },
  { value: 'medium', label: 'Medium and up', hint: 'Recommended' },
  { value: 'high', label: 'High and up', hint: 'Only the urgent' },
  { value: 'critical', label: 'Critical only', hint: 'Bare minimum' },
]

// A short, sensible set; the user's current zone is added if it isn't here.
const TIMEZONES = [
  'Europe/Tallinn',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Helsinki',
  'America/New_York',
  'America/Los_Angeles',
  'Asia/Singapore',
  'UTC',
]

function Toggle({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
        checked ? 'bg-[#00C9A7]' : 'bg-zinc-700'
      }`}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
          checked ? 'translate-x-6' : 'translate-x-1'
        }`}
      />
    </button>
  )
}

function Row({
  title,
  description,
  children,
}: {
  title: string
  description: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-6 border-b border-white/5 py-4 last:border-0">
      <div className="min-w-0">
        <p className="text-sm font-medium text-zinc-100">{title}</p>
        <p className="mt-0.5 text-xs text-zinc-400">{description}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

export function NotificationSettings({
  prefs,
  plan,
}: {
  prefs: NotificationPreferences
  plan: Plan
}) {
  const isPro = plan === 'pro'
  const [email, setEmail] = useState(prefs.email ?? '')
  const [minSeverity, setMinSeverity] = useState<SeverityLevel>(prefs.minSeverityForEmail)
  const [weeklyBriefing, setWeeklyBriefing] = useState(prefs.weeklyBriefingEnabled && isPro)
  const [deadlineAlerts, setDeadlineAlerts] = useState(prefs.deadlineAlertsEnabled)
  const [quietStart, setQuietStart] = useState((prefs.quietHoursStart ?? '').slice(0, 5))
  const [quietEnd, setQuietEnd] = useState((prefs.quietHoursEnd ?? '').slice(0, 5))
  const [timezone, setTimezone] = useState(prefs.timezone)
  const [pending, startTransition] = useTransition()

  const zones = TIMEZONES.includes(timezone) ? TIMEZONES : [timezone, ...TIMEZONES]

  function save() {
    startTransition(async () => {
      const res = await updateNotificationPreferences({
        email,
        minSeverityForEmail: minSeverity,
        weeklyBriefingEnabled: weeklyBriefing,
        deadlineAlertsEnabled: deadlineAlerts,
        quietHoursStart: quietStart,
        quietHoursEnd: quietEnd,
        timezone,
      })
      if (res.ok) {
        toast.success('Notification preferences saved')
      } else {
        toast.error(res.error)
      }
    })
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-zinc-50">Notifications</h1>
        <p className="mt-1 text-sm text-zinc-400">
          Choose what reaches your inbox, and when. Critical findings always email you.
        </p>
      </div>

      <div className="rounded-2xl border border-white/5 bg-white/[0.02] px-5">
        <Row
          title="Delivery address"
          description="Where notifications are sent. Defaults to your account email."
        >
          <Input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-label="Delivery email address"
            className="w-64"
          />
        </Row>

        <Row
          title="Email me about findings"
          description="The lowest severity that triggers an immediate email."
        >
          <Select value={minSeverity} onValueChange={(v) => setMinSeverity(v as SeverityLevel)}>
            <SelectTrigger className="w-48" aria-label="Minimum severity for email">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SEVERITIES.map((s) => (
                <SelectItem key={s.value} value={s.value}>
                  {s.label} · {s.hint}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>

        <Row
          title="Weekly Monday briefing"
          description={
            isPro ? (
              'A Monday summary of your posture.'
            ) : (
              <>
                A Monday summary of your posture.{' '}
                <Link href="/billing" className="text-[#00C9A7] underline">
                  Upgrade to Pro
                </Link>{' '}
                to enable.
              </>
            )
          }
        >
          <Toggle
            checked={weeklyBriefing}
            onChange={setWeeklyBriefing}
            disabled={!isPro}
            label="Weekly Monday briefing"
          />
        </Row>

        <Row
          title="Deadline alerts"
          description="Email as obligations approach their deadline (30 / 14 / 7 / 1 days)."
        >
          <Toggle checked={deadlineAlerts} onChange={setDeadlineAlerts} label="Deadline alerts" />
        </Row>

        <Row
          title="Quiet hours"
          description="Hold non-critical emails during these hours (your timezone). Leave blank for none."
        >
          <div className="flex items-center gap-2">
            <Input
              type="time"
              value={quietStart}
              onChange={(e) => setQuietStart(e.target.value)}
              aria-label="Quiet hours start"
              className="w-28"
            />
            <span className="text-xs text-zinc-500">to</span>
            <Input
              type="time"
              value={quietEnd}
              onChange={(e) => setQuietEnd(e.target.value)}
              aria-label="Quiet hours end"
              className="w-28"
            />
          </div>
        </Row>

        <Row title="Timezone" description="Used for the briefing schedule and quiet hours.">
          <Select value={timezone} onValueChange={(v) => v && setTimezone(v)}>
            <SelectTrigger className="w-56" aria-label="Timezone">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {zones.map((z) => (
                <SelectItem key={z} value={z}>
                  {z}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
      </div>

      <div className="flex justify-end">
        <Button onClick={save} disabled={pending}>
          {pending ? 'Saving…' : 'Save changes'}
        </Button>
      </div>

      {/* Keep a hidden label association for the form region for a11y tools. */}
      <Label className="sr-only" htmlFor="notification-settings">
        Notification settings
      </Label>
    </div>
  )
}
