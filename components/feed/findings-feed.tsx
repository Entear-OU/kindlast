'use client'

import Link from 'next/link'
import { useEffect, useMemo, useState, useTransition } from 'react'
import { toast } from 'sonner'

import { approveFinding, rejectFinding, snoozeFinding } from '@/app/(authed)/feed/actions'
import { trackUpgradeConverted, trackUpgradePromptShown } from '@/lib/analytics/track'
import type { Plan } from '@/lib/billing/plan'
import {
  DEFAULT_SNOOZE_DAYS,
  FEED_SEVERITIES,
  FEED_STATUSES,
  FREE_FINDING_LIMIT,
  SNOOZE_OPTIONS,
  filterFindings,
  gateFindings,
  severityChip,
  statusLabel,
  upgradeWaitingMessage,
  type Finding,
  type FindingSeverity,
  type FindingStatus,
} from '@/lib/feed/findings'

/**
 * The Agent feed (ENT-62 list, ENT-63 actions) — every finding the agents
 * produced, newest first, with status + severity filters and a friendly empty
 * state. Pending cards carry one-tap Approve / Reject / Snooze.
 *
 * Actions are optimistic (AC): the card reflects the new status immediately, and
 * a failure rolls the row back and raises a toast. The server action is the
 * authority — the tier gate and ownership rules live there — so the optimistic
 * flip is only a prediction the server confirms or rejects.
 *
 * `plan` (ENT-63 seam, real since ENT-81) drives two Free-tier affordances: the
 * Pro prompt on Approve, and the 3-finding cap (ENT-82) — Free sees the 3
 * most-recent findings and the rest render as locked, blurred previews behind an
 * upgrade prompt that carries the trigger context.
 */

type StatusChoice = FindingStatus | 'all'
type SeverityChoice = FindingSeverity | 'all'

const chip = (active: boolean) =>
  `rounded-full px-3 py-1 text-xs font-medium transition-colors ${
    active
      ? 'bg-white text-zinc-900'
      : 'border border-white/10 text-zinc-400 hover:bg-white/5 hover:text-zinc-200'
  }`

function FilterGroup<T extends string>({
  label,
  options,
  value,
  onChange,
  renderLabel,
}: {
  label: string
  options: T[]
  value: T
  onChange: (v: T) => void
  renderLabel: (v: T) => string
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-xs uppercase tracking-wide text-zinc-500">{label}</span>
      {options.map((opt) => (
        <button
          key={opt}
          type="button"
          aria-pressed={value === opt}
          onClick={() => onChange(opt)}
          className={chip(value === opt)}
        >
          {renderLabel(opt)}
        </button>
      ))}
    </div>
  )
}

export function FindingsFeed({
  findings,
  plan = 'pro',
}: {
  findings: Finding[]
  /** ENT-63 seam: render the Pro upgrade affordance on Approve for Free users. */
  plan?: Plan
}) {
  const [items, setItems] = useState<Finding[]>(findings)
  const [status, setStatus] = useState<StatusChoice>('all')
  const [severity, setSeverity] = useState<SeverityChoice>('all')
  const [pending, startTransition] = useTransition()
  const [busyId, setBusyId] = useState<string | null>(null)

  // Free-tier gate first (ENT-82): the 3 most-recent are actionable, the rest
  // locked. Filters then narrow only the actionable set; the locked previews are
  // a separate teaser below.
  const gated = useMemo(() => gateFindings(items, plan), [items, plan])
  const visible = useMemo(
    () => filterFindings(gated.visible, { status, severity }),
    [gated.visible, status, severity],
  )

  /**
   * Optimistically patch a finding, run its server action, and roll the whole
   * list back with a toast if the action fails. `patch` is the predicted row;
   * `successMsg` is the confirmation toast.
   */
  function runAction(
    id: string,
    patch: Partial<Finding>,
    action: () => Promise<{ ok: true } | { ok: false; error: string; upgrade?: boolean }>,
    successMsg: string,
  ) {
    const snapshot = items
    setBusyId(id)
    setItems((prev) => prev.map((f) => (f.id === id ? { ...f, ...patch } : f)))
    startTransition(async () => {
      const res = await action()
      if (res.ok) {
        toast.success(successMsg)
      } else {
        setItems(snapshot)
        if (res.upgrade) {
          toast.error('Approving is a Pro feature', {
            description: 'Upgrade to let your agents act on this finding.',
          })
        } else {
          toast.error('Something went wrong', { description: res.error })
        }
      }
      setBusyId(null)
    })
  }

  function onApprove(id: string) {
    // Free users never reach the Executor — surface the upgrade prompt instead
    // of an optimistic flip the server would only reject.
    if (plan !== 'pro') {
      toast.error('Approving is a Pro feature', {
        description: 'Upgrade to let your agents act on this finding.',
      })
      return
    }
    runAction(id, { status: 'approved' }, () => approveFinding(id), 'Finding approved')
  }

  function onReject(id: string, reason: string) {
    runAction(
      id,
      { status: 'rejected', rejection_reason: reason.trim() || null },
      () => rejectFinding(id, reason),
      'Finding rejected',
    )
  }

  function onSnooze(id: string, days: number) {
    // Only the status pill is shown optimistically; the concrete snoozed_until
    // comes back from the server on the next load (and avoids an impure Date.now
    // in render).
    runAction(id, { status: 'snoozed' }, () => snoozeFinding(id, days), `Snoozed for ${days} days`)
  }

  // Nothing has ever been detected — the friendly all-clear (AC).
  if (items.length === 0) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center gap-2 text-center">
        <p className="text-base font-medium text-zinc-200">All clear</p>
        <p className="max-w-sm text-sm text-zinc-500">
          The Watcher will let you know when something changes. New findings show up here, newest
          first.
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-3">
        <FilterGroup<StatusChoice>
          label="Status"
          options={['all', ...FEED_STATUSES]}
          value={status}
          onChange={setStatus}
          renderLabel={(v) => (v === 'all' ? 'All' : statusLabel(v))}
        />
        <FilterGroup<SeverityChoice>
          label="Severity"
          options={['all', ...FEED_SEVERITIES]}
          value={severity}
          onChange={setSeverity}
          renderLabel={(v) => (v === 'all' ? 'All' : severityChip(v).label)}
        />
      </div>

      {visible.length === 0 ? (
        <div className="rounded-xl border border-white/10 bg-white/[0.02] px-4 py-10 text-center text-sm text-zinc-500">
          No findings match these filters.
        </div>
      ) : (
        <ul className="flex flex-col gap-3">
          {visible.map((f) => (
            <FindingCard
              key={f.id}
              finding={f}
              busy={pending && busyId === f.id}
              onApprove={() => onApprove(f.id)}
              onReject={(reason) => onReject(f.id, reason)}
              onSnooze={(days) => onSnooze(f.id, days)}
            />
          ))}
        </ul>
      )}

      {gated.lockedCount > 0 ? (
        <LockedFindings
          locked={gated.locked}
          lockedCount={gated.lockedCount}
          totalCount={gated.totalCount}
        />
      ) : null}
    </div>
  )
}

/**
 * The Free-tier lock (ENT-82): blurred, non-interactive previews of every
 * finding beyond the cap, plus one upgrade prompt carrying the trigger context.
 * Fires `upgrade_prompt_shown` on mount and `upgrade_prompt_converted` when the
 * founder taps the CTA.
 */
function LockedFindings({
  locked,
  lockedCount,
  totalCount,
}: {
  locked: Finding[]
  lockedCount: number
  totalCount: number
}) {
  useEffect(() => {
    trackUpgradePromptShown({ source: 'finding_cap', lockedCount, totalCount })
  }, [lockedCount, totalCount])

  return (
    <div className="flex flex-col gap-3">
      <ul aria-hidden="true" className="flex flex-col gap-3">
        {locked.map((f) => (
          <li
            key={f.id}
            className="pointer-events-none select-none rounded-xl border border-white/10 bg-white/[0.03] p-4 blur-sm"
          >
            <h3 className="text-sm font-semibold text-zinc-100">{f.detected}</h3>
            <p className="mt-3 text-sm text-zinc-300">{f.proposed_action}</p>
          </li>
        ))}
      </ul>

      <div className="rounded-xl border border-[#00C9A7]/30 bg-[#00C9A7]/[0.06] p-4 text-center">
        <p className="text-sm font-medium text-zinc-100">{upgradeWaitingMessage(totalCount)}</p>
        <p className="mt-1 text-xs text-zinc-400">
          Free shows your {FREE_FINDING_LIMIT} most-recent findings. Upgrade to Pro to see and act
          on all of them.
        </p>
        <Link
          href="/billing"
          onClick={() =>
            trackUpgradeConverted({ source: 'finding_cap', lockedCount, totalCount })
          }
          className="mt-3 inline-block rounded-md bg-[#00C9A7] px-4 py-1.5 text-sm font-medium text-zinc-950 transition-opacity hover:opacity-90"
        >
          Upgrade to Pro
        </Link>
      </div>
    </div>
  )
}

const actionBtn =
  'rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50'

function FindingCard({
  finding,
  busy,
  onApprove,
  onReject,
  onSnooze,
}: {
  finding: Finding
  busy: boolean
  onApprove: () => void
  onReject: (reason: string) => void
  onSnooze: (days: number) => void
}) {
  const sev = severityChip(finding.severity)
  const [rejecting, setRejecting] = useState(false)
  const [reason, setReason] = useState('')
  const [snoozeDays, setSnoozeDays] = useState(DEFAULT_SNOOZE_DAYS)

  return (
    <li className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-semibold text-zinc-100">
          <Link
            href={`/feed/${finding.id}`}
            className="hover:text-white hover:underline decoration-zinc-600 underline-offset-2"
          >
            {finding.detected}
          </Link>
        </h3>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${sev.className}`}>
          {sev.label}
        </span>
      </div>

      {finding.regulatory_obligation ? (
        <p className="mt-1 text-xs text-zinc-400">
          {finding.citation_url ? (
            <a
              href={finding.citation_url}
              target="_blank"
              rel="noreferrer"
              className="underline decoration-zinc-600 underline-offset-2 hover:text-zinc-200"
            >
              {finding.regulatory_obligation}
            </a>
          ) : (
            finding.regulatory_obligation
          )}
        </p>
      ) : null}

      <p className="mt-3 text-sm text-zinc-300">{finding.proposed_action}</p>

      <div className="mt-3 flex items-center gap-2 text-xs text-zinc-500">
        <span className="rounded-full border border-white/10 px-2 py-0.5 text-zinc-300">
          {statusLabel(finding.status)}
        </span>
        <span aria-hidden="true">·</span>
        <span>Effort: {finding.effort_estimate}</span>
      </div>

      {finding.status === 'pending' && (
        <div className="mt-3 border-t border-white/5 pt-3">
          {rejecting ? (
            <div className="flex flex-col gap-2">
              <textarea
                aria-label="Rejection reason (optional)"
                placeholder="Why are you rejecting this? (optional)"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                rows={2}
                className="w-full rounded-md border border-white/10 bg-white/5 px-2 py-1 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-[#00C9A7] focus:outline-none"
              />
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => onReject(reason)}
                  disabled={busy}
                  className={`${actionBtn} bg-rose-500/15 text-rose-300 hover:bg-rose-500/25`}
                >
                  Confirm reject
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setRejecting(false)
                    setReason('')
                  }}
                  disabled={busy}
                  className={`${actionBtn} text-zinc-400 hover:text-zinc-200`}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={onApprove}
                disabled={busy}
                className={`${actionBtn} bg-[#00C9A7] text-zinc-950 hover:opacity-90`}
              >
                Approve
              </button>
              <button
                type="button"
                onClick={() => setRejecting(true)}
                disabled={busy}
                className={`${actionBtn} border border-white/10 text-zinc-300 hover:bg-white/5`}
              >
                Reject
              </button>
              <span className="ml-auto flex items-center gap-2">
                <label className="sr-only" htmlFor={`snooze-${finding.id}`}>
                  Snooze duration
                </label>
                <select
                  id={`snooze-${finding.id}`}
                  value={snoozeDays}
                  onChange={(e) => setSnoozeDays(Number(e.target.value))}
                  disabled={busy}
                  className="rounded-md border border-white/10 bg-white/5 px-2 py-1 text-xs text-zinc-300 focus:border-[#00C9A7] focus:outline-none"
                >
                  {SNOOZE_OPTIONS.map((opt) => (
                    <option key={opt.days} value={opt.days} className="bg-zinc-900">
                      {opt.label}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={() => onSnooze(snoozeDays)}
                  disabled={busy}
                  className={`${actionBtn} border border-white/10 text-zinc-300 hover:bg-white/5`}
                >
                  Snooze
                </button>
              </span>
            </div>
          )}
        </div>
      )}
    </li>
  )
}
