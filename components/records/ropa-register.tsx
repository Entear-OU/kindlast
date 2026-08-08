'use client'

import { useEffect, useState, useTransition } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

import { addActivity, editActivity } from '@/app/(authed)/records/ropa/actions'
import { trackUpgradeConverted, trackUpgradePromptShown } from '@/lib/analytics/track'
import type { Plan } from '@/lib/billing/plan'
import { upgradeHref } from '@/lib/billing/upgrade-link'
import {
  deriveRopaStatus,
  formatUpdatedAt,
  lockedManualActivityIds,
  manualActivityCount,
  ropaManualLimit,
  ROPA_STATUS_LABEL,
  type ProcessingActivity,
  type ProcessingActivityInput,
  type RopaStatus,
} from '@/lib/records/ropa'

/**
 * Form-local values. The two list fields are kept as raw comma-separated strings
 * while editing — splitting/re-joining on every keystroke would eat the
 * separators — and are parsed to arrays only at submit (`toInput`).
 */
interface RopaFormValues {
  name: string
  purpose: string
  legal_basis: string
  data_categories: string
  recipients: string
  retention_period: string
}

const EMPTY: RopaFormValues = {
  name: '',
  purpose: '',
  legal_basis: '',
  data_categories: '',
  recipients: '',
  retention_period: '',
}

const STATUS_STYLE: Record<RopaStatus, string> = {
  complete: 'bg-emerald-500/15 text-emerald-300',
  review_needed: 'bg-sky-500/15 text-sky-300',
  incomplete: 'bg-amber-500/15 text-amber-300',
}

const joinList = (xs: string[]) => xs.join(', ')
const splitList = (s: string) =>
  s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)

const toForm = (a: ProcessingActivity): RopaFormValues => ({
  name: a.name,
  purpose: a.purpose ?? '',
  legal_basis: a.legal_basis ?? '',
  data_categories: joinList(a.data_categories),
  recipients: joinList(a.recipients),
  retention_period: a.retention_period ?? '',
})

const toInput = (f: RopaFormValues): ProcessingActivityInput => ({
  name: f.name,
  purpose: f.purpose,
  legal_basis: f.legal_basis,
  data_categories: splitList(f.data_categories),
  recipients: splitList(f.recipients),
  retention_period: f.retention_period,
})

function StatusPill({ status }: { status: RopaStatus }) {
  return (
    <span
      className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ${STATUS_STYLE[status]}`}
    >
      {ROPA_STATUS_LABEL[status]}
    </span>
  )
}

const inputClass =
  'w-full rounded-md border border-white/10 bg-white/5 px-2 py-1 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-[#00C9A7] focus:outline-none'

/** The editable cluster of fields, shared by the add row and the edit row. */
function ActivityForm({
  draft,
  setDraft,
  onSave,
  onCancel,
  pending,
  saveLabel,
}: {
  draft: RopaFormValues
  setDraft: (d: RopaFormValues) => void
  onSave: () => void
  onCancel: () => void
  pending: boolean
  saveLabel: string
}) {
  return (
    <div className="grid gap-2 rounded-lg border border-white/10 bg-white/[0.03] p-3 sm:grid-cols-2">
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Activity name
        <input
          aria-label="Activity name"
          className={inputClass}
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Legal basis
        <input
          aria-label="Legal basis"
          className={inputClass}
          value={draft.legal_basis}
          onChange={(e) => setDraft({ ...draft, legal_basis: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400 sm:col-span-2">
        Purpose
        <input
          aria-label="Purpose"
          className={inputClass}
          value={draft.purpose}
          onChange={(e) => setDraft({ ...draft, purpose: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Data categories (comma-separated)
        <input
          aria-label="Data categories"
          className={inputClass}
          value={draft.data_categories}
          onChange={(e) => setDraft({ ...draft, data_categories: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Recipients (comma-separated)
        <input
          aria-label="Recipients"
          className={inputClass}
          value={draft.recipients}
          onChange={(e) => setDraft({ ...draft, recipients: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Retention period
        <input
          aria-label="Retention period"
          className={inputClass}
          value={draft.retention_period}
          onChange={(e) => setDraft({ ...draft, retention_period: e.target.value })}
        />
      </label>
      <div className="flex items-end gap-2">
        <button
          type="button"
          onClick={onSave}
          disabled={pending}
          className="rounded-md bg-[#00C9A7] px-3 py-1.5 text-sm font-medium text-zinc-950 transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {pending ? 'Saving…' : saveLabel}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={pending}
          className="rounded-md px-3 py-1.5 text-sm text-zinc-400 hover:text-zinc-200"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}

export function RopaRegister({
  activities,
  plan = 'pro',
}: {
  activities: ProcessingActivity[]
  /** Drives the Free-tier manual-activity cap (ENT-84). */
  plan?: Plan
}) {
  const router = useRouter()
  const [pending, startTransition] = useTransition()
  const [editingId, setEditingId] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState<RopaFormValues>(EMPTY)
  const [error, setError] = useState<string | null>(null)

  // null = uncapped (Pro). Free is capped at ROPA_MANUAL_LIMIT manual rows.
  const manualLimit = ropaManualLimit(plan)
  const manualUsed = manualActivityCount(activities)
  const atManualCap = manualLimit !== null && manualUsed >= manualLimit
  // Edge case: a downgrade can leave more manual rows than the cap — those go
  // read-only with an upgrade hint instead of staying freely editable.
  const lockedIds = lockedManualActivityIds(activities, manualLimit)

  // Tracking (AC mirrors ENT-82/83): the cap prompt is "shown" once the Free
  // user hits the limit.
  useEffect(() => {
    if (atManualCap) {
      trackUpgradePromptShown({ source: 'ropa_cap', lockedCount: lockedIds.size, totalCount: manualUsed })
    }
  }, [atManualCap, lockedIds.size, manualUsed])

  function beginAdd() {
    setEditingId(null)
    setDraft(EMPTY)
    setError(null)
    setAdding(true)
  }

  function beginEdit(a: ProcessingActivity) {
    setAdding(false)
    setError(null)
    setDraft(toForm(a))
    setEditingId(a.id)
  }

  function reset() {
    setAdding(false)
    setEditingId(null)
    setError(null)
  }

  function submitAdd() {
    startTransition(async () => {
      const res = await addActivity(toInput(draft))
      if (res.ok) {
        reset()
        router.refresh()
      } else {
        setError(res.error)
      }
    })
  }

  function submitEdit(id: string) {
    startTransition(async () => {
      const res = await editActivity(id, toInput(draft))
      if (res.ok) {
        reset()
        router.refresh()
      } else {
        setError(res.error)
      }
    })
  }

  const addButton = (
    <div className="flex flex-col items-start gap-1">
      <button
        type="button"
        onClick={beginAdd}
        disabled={adding || atManualCap}
        className="rounded-lg bg-white px-3 py-1.5 text-sm font-medium text-zinc-900 transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
      >
        Add activity
      </button>
      {atManualCap && (
        <p className="text-xs text-amber-300/80">
          Free plan: {manualUsed} of {manualLimit} manual activities used.{' '}
          <Link
            href={upgradeHref('/records/ropa')}
            onClick={() =>
              trackUpgradeConverted({
                source: 'ropa_cap',
                lockedCount: lockedIds.size,
                totalCount: manualUsed,
              })
            }
            className="font-medium text-[#00C9A7] underline-offset-2 hover:underline"
          >
            Upgrade to Pro
          </Link>{' '}
          to add more.
        </p>
      )}
    </div>
  )

  if (activities.length === 0 && !adding) {
    return (
      <div className="flex flex-col items-start gap-4">
        <div className="max-w-md rounded-xl border border-white/10 bg-white/[0.03] p-6">
          <h2 className="text-sm font-semibold text-zinc-100">No processing activities yet</h2>
          <p className="mt-2 text-sm leading-relaxed text-zinc-400">
            Your ROPA fills up as you <strong>approve findings</strong>. The agent pre-fills a
            ratified entry for each one. You can also add an activity the agent hasn&apos;t seen.
          </p>
        </div>
        {addButton}
        {error && <p className="text-sm text-rose-400">{error}</p>}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {error && <p className="text-sm text-rose-400">{error}</p>}

      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left text-sm">
          <thead>
            <tr className="text-[11px] uppercase tracking-wider text-zinc-500">
              <th className="py-2 pr-4 font-medium">Activity</th>
              <th className="py-2 pr-4 font-medium">Legal basis</th>
              <th className="py-2 pr-4 font-medium">Data categories</th>
              <th className="py-2 pr-4 font-medium">Recipients</th>
              <th className="py-2 pr-4 font-medium">Retention</th>
              <th className="py-2 pr-4 font-medium">Status</th>
              <th className="py-2 pr-4 font-medium">Updated</th>
              <th className="py-2 font-medium sr-only">Actions</th>
            </tr>
          </thead>
          <tbody>
            {activities.map((a) =>
              editingId === a.id ? (
                <tr key={a.id}>
                  <td colSpan={8} className="py-2">
                    <ActivityForm
                      draft={draft}
                      setDraft={setDraft}
                      onSave={() => submitEdit(a.id)}
                      onCancel={reset}
                      pending={pending}
                      saveLabel="Save"
                    />
                  </td>
                </tr>
              ) : (
                <tr key={a.id} className="border-t border-white/5 align-top">
                  <td className="py-3 pr-4">
                    <div className="font-medium text-zinc-100">{a.name}</div>
                    {a.purpose && <div className="text-xs text-zinc-500">{a.purpose}</div>}
                  </td>
                  <td className="py-3 pr-4 text-zinc-300">{a.legal_basis || '–'}</td>
                  <td className="py-3 pr-4 text-zinc-300">{joinList(a.data_categories) || '–'}</td>
                  <td className="py-3 pr-4 text-zinc-300">{joinList(a.recipients) || '–'}</td>
                  <td className="py-3 pr-4 text-zinc-300">{a.retention_period || '–'}</td>
                  <td className="py-3 pr-4">
                    <StatusPill status={deriveRopaStatus(a)} />
                  </td>
                  <td className="py-3 pr-4 text-zinc-400">{formatUpdatedAt(a.updated_at)}</td>
                  <td className="py-3 text-right">
                    {lockedIds.has(a.id) ? (
                      <Link
                        href={upgradeHref('/records/ropa')}
                        onClick={() =>
                          trackUpgradeConverted({
                            source: 'ropa_cap',
                            lockedCount: lockedIds.size,
                            totalCount: manualUsed,
                          })
                        }
                        title="Over the Free-tier limit. Upgrade to edit this activity."
                        className="rounded-md px-2 py-1 text-xs text-amber-300/80 hover:bg-white/5"
                      >
                        Upgrade to edit
                      </Link>
                    ) : (
                      <button
                        type="button"
                        onClick={() => beginEdit(a)}
                        className="rounded-md px-2 py-1 text-xs text-zinc-400 hover:bg-white/5 hover:text-zinc-100"
                      >
                        Edit
                      </button>
                    )}
                  </td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>

      {adding && (
        <ActivityForm
          draft={draft}
          setDraft={setDraft}
          onSave={submitAdd}
          onCancel={reset}
          pending={pending}
          saveLabel="Add activity"
        />
      )}

      {!adding && addButton}
    </div>
  )
}
