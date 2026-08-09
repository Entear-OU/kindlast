'use client'

import { useState, useTransition } from 'react'
import { useRouter } from 'next/navigation'

import { addSystem, editSystem, type AiSystemInput } from '@/app/(authed)/records/ai-systems/actions'
import { isBlank } from '@/lib/records/required-fields'
import {
  DOC_LABEL,
  DOC_OPTIONS,
  DOC_TONE,
  formatReviewed,
  RISK_LABEL,
  RISK_OPTIONS,
  RISK_TONE,
  type AiSystem,
  type PillTone,
} from '@/lib/records/ai-system'

const TONE_STYLE: Record<PillTone, string> = {
  done: 'bg-emerald-500/15 text-emerald-300',
  danger: 'bg-rose-500/15 text-rose-300',
  warn: 'bg-amber-500/15 text-amber-300',
  info: 'bg-sky-500/15 text-sky-300',
  muted: 'bg-white/10 text-zinc-400',
}

const EMPTY: AiSystemInput = {
  name: '',
  vendor: '',
  purpose: '',
  risk_classification: 'unclassified',
  documentation_status: 'missing',
}

const fieldClass =
  'w-full rounded-md border border-white/10 bg-white/5 px-2 py-1 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-[#00C9A7] focus:outline-none'

const toForm = (a: AiSystem): AiSystemInput => ({
  name: a.name,
  vendor: a.vendor ?? '',
  purpose: a.purpose ?? '',
  risk_classification: a.risk_classification,
  documentation_status: a.documentation_status,
})

function Pill({ tone, children }: { tone: PillTone; children: React.ReactNode }) {
  return (
    <span className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ${TONE_STYLE[tone]}`}>
      {children}
    </span>
  )
}

function SystemForm({
  draft,
  setDraft,
  onSave,
  onCancel,
  pending,
  saveLabel,
  reviewNeeded,
  confirming,
}: {
  draft: AiSystemInput
  setDraft: (d: AiSystemInput) => void
  onSave: () => void
  onCancel: () => void
  pending: boolean
  saveLabel: string
  reviewNeeded: boolean
  confirming: boolean
}) {
  return (
    <div className="grid gap-2 rounded-lg border border-white/10 bg-white/[0.03] p-3 sm:grid-cols-2">
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        System name
        <input
          aria-label="System name"
          className={fieldClass}
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Vendor
        <input
          aria-label="Vendor"
          className={fieldClass}
          value={draft.vendor}
          onChange={(e) => setDraft({ ...draft, vendor: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400 sm:col-span-2">
        Purpose
        <input
          aria-label="Purpose"
          className={fieldClass}
          value={draft.purpose}
          onChange={(e) => setDraft({ ...draft, purpose: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Risk classification
        <select
          aria-label="Risk classification"
          className={fieldClass}
          value={draft.risk_classification}
          onChange={(e) =>
            setDraft({ ...draft, risk_classification: e.target.value as AiSystemInput['risk_classification'] })
          }
        >
          {RISK_OPTIONS.map((r) => (
            <option key={r} value={r}>
              {RISK_LABEL[r]}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Documentation status
        <select
          aria-label="Documentation status"
          className={fieldClass}
          value={draft.documentation_status}
          onChange={(e) =>
            setDraft({ ...draft, documentation_status: e.target.value as AiSystemInput['documentation_status'] })
          }
        >
          {DOC_OPTIONS.map((d) => (
            <option key={d} value={d}>
              {DOC_LABEL[d]}
            </option>
          ))}
        </select>
      </label>

      {reviewNeeded && (
        <p className="text-xs text-amber-300/90 sm:col-span-2">
          {confirming
            ? 'Confirm: this classification is recorded as a reviewed approval.'
            : 'Changing the risk classification requires a reviewed approval.'}
        </p>
      )}

      <div className="flex items-end gap-2 sm:col-span-2">
        <button
          type="button"
          onClick={onSave}
          // ENT-168: a system with no name is a blank row in the register.
          disabled={pending || isBlank(draft.name)}
          className="rounded-md bg-[#00C9A7] px-3 py-1.5 text-sm font-medium text-zinc-950 transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {pending ? 'Saving…' : confirming ? 'Confirm reviewed approval' : saveLabel}
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

export function AiSystemsRegister({ systems }: { systems: AiSystem[] }) {
  const router = useRouter()
  const [pending, startTransition] = useTransition()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<AiSystem | null>(null)
  const [draft, setDraft] = useState<AiSystemInput>(EMPTY)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // A reviewed approval is required to classify a new system High risk, or to
  // change any existing system's classification.
  const reviewNeeded = adding
    ? draft.risk_classification === 'high'
    : editing
      ? draft.risk_classification !== editing.risk_classification
      : false

  function reset() {
    setAdding(false)
    setEditing(null)
    setConfirming(false)
    setError(null)
  }

  function beginAdd() {
    setEditing(null)
    setDraft(EMPTY)
    setConfirming(false)
    setError(null)
    setAdding(true)
  }

  function beginEdit(a: AiSystem) {
    setAdding(false)
    setConfirming(false)
    setError(null)
    setDraft(toForm(a))
    setEditing(a)
  }

  function attemptSave() {
    // A reviewed approval is a deliberate two-step confirmation.
    if (reviewNeeded && !confirming) {
      setConfirming(true)
      return
    }
    const reviewed = reviewNeeded
    startTransition(async () => {
      const res = editing
        ? await editSystem(editing.id, draft, reviewed)
        : await addSystem(draft, reviewed)
      if (res.ok) {
        reset()
        router.refresh()
      } else {
        setError(res.error)
        setConfirming(false)
      }
    })
  }

  const addButton = (
    <button
      type="button"
      onClick={beginAdd}
      disabled={adding}
      className="rounded-lg bg-white px-3 py-1.5 text-sm font-medium text-zinc-900 transition-opacity hover:opacity-90 disabled:opacity-40"
    >
      Add system
    </button>
  )

  const form = (
    <SystemForm
      draft={draft}
      setDraft={setDraft}
      onSave={attemptSave}
      onCancel={reset}
      pending={pending}
      saveLabel={editing ? 'Save' : 'Add system'}
      reviewNeeded={reviewNeeded}
      confirming={confirming}
    />
  )

  if (systems.length === 0 && !adding) {
    return (
      <div className="flex flex-col items-start gap-4">
        <div className="max-w-md rounded-xl border border-white/10 bg-white/[0.03] p-6">
          <h2 className="text-sm font-semibold text-zinc-100">No AI systems registered yet</h2>
          <p className="mt-2 text-sm leading-relaxed text-zinc-400">
            Systems land here when you <strong>approve an AI-system finding</strong>. You can also
            add one the agent hasn&apos;t seen, which is useful for catching shadow AI.
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
              <th className="py-2 pr-4 font-medium">System</th>
              <th className="py-2 pr-4 font-medium">Vendor</th>
              <th className="py-2 pr-4 font-medium">Risk classification</th>
              <th className="py-2 pr-4 font-medium">Documentation</th>
              <th className="py-2 pr-4 font-medium">Last reviewed</th>
              <th className="py-2 font-medium sr-only">Actions</th>
            </tr>
          </thead>
          <tbody>
            {systems.map((a) =>
              editing?.id === a.id ? (
                <tr key={a.id}>
                  <td colSpan={6} className="py-2">
                    {form}
                  </td>
                </tr>
              ) : (
                <tr key={a.id} className="border-t border-white/5 align-top">
                  <td className="py-3 pr-4">
                    <div className="font-medium text-zinc-100">{a.name}</div>
                    {a.purpose && <div className="text-xs text-zinc-500">{a.purpose}</div>}
                  </td>
                  <td className="py-3 pr-4 text-zinc-300">{a.vendor || '–'}</td>
                  <td className="py-3 pr-4">
                    <Pill tone={RISK_TONE[a.risk_classification]}>
                      {RISK_LABEL[a.risk_classification]}
                    </Pill>
                  </td>
                  <td className="py-3 pr-4">
                    <Pill tone={DOC_TONE[a.documentation_status]}>
                      {DOC_LABEL[a.documentation_status]}
                    </Pill>
                  </td>
                  <td className="py-3 pr-4 text-zinc-400">{formatReviewed(a.last_reviewed_at)}</td>
                  <td className="py-3 text-right">
                    <button
                      type="button"
                      onClick={() => beginEdit(a)}
                      className="rounded-md px-2 py-1 text-xs text-zinc-400 hover:bg-white/5 hover:text-zinc-100"
                    >
                      Edit
                    </button>
                  </td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>

      {adding && form}
      {!adding && !editing && addButton}
    </div>
  )
}
