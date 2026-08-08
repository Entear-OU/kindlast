'use client'

import { useState, useTransition } from 'react'
import { useRouter } from 'next/navigation'

import { logDsar, markResponded } from '@/app/(authed)/records/dsar/actions'
import {
  deriveDsarStatus,
  formatDate,
  formatDueLabel,
  isOpenDsar,
  type Dsar,
  type DsarTone,
} from '@/lib/records/dsar'

const TONE_STYLE: Record<DsarTone, string> = {
  done: 'bg-emerald-500/15 text-emerald-300',
  danger: 'bg-rose-500/15 text-rose-300',
  warn: 'bg-amber-500/15 text-amber-300',
  info: 'bg-sky-500/15 text-sky-300',
}

const inputClass =
  'w-full rounded-md border border-white/10 bg-white/5 px-2 py-1 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-[#00C9A7] focus:outline-none'

const EMPTY = { subject_name: '', request_type: '', handler: '' }

export function DsarLog({
  dsars,
  canComplete = true,
}: {
  dsars: Dsar[]
  /** Pro capability — "Mark as responded" is an Executor write (ENT-71 AC). */
  canComplete?: boolean
}) {
  const router = useRouter()
  const [pending, startTransition] = useTransition()
  const [logging, setLogging] = useState(false)
  const [draft, setDraft] = useState(EMPTY)
  const [confirmingId, setConfirmingId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  function reset() {
    setLogging(false)
    setConfirmingId(null)
    setError(null)
  }

  function submitLog() {
    startTransition(async () => {
      const res = await logDsar(draft)
      if (res.ok) {
        setDraft(EMPTY)
        reset()
        router.refresh()
      } else {
        setError(res.error)
      }
    })
  }

  function confirmResponded(id: string) {
    startTransition(async () => {
      const res = await markResponded(id)
      if (res.ok) {
        reset()
        router.refresh()
      } else {
        setError(res.error)
      }
    })
  }

  const logButton = (
    <button
      type="button"
      onClick={() => {
        setDraft(EMPTY)
        setError(null)
        setLogging(true)
      }}
      disabled={logging}
      className="rounded-lg bg-white px-3 py-1.5 text-sm font-medium text-zinc-900 transition-opacity hover:opacity-90 disabled:opacity-40"
    >
      Log a DSAR
    </button>
  )

  const logForm = (
    <div className="grid gap-2 rounded-lg border border-white/10 bg-white/[0.03] p-3 sm:grid-cols-3">
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Requester
        <input
          aria-label="Requester"
          className={inputClass}
          value={draft.subject_name}
          onChange={(e) => setDraft({ ...draft, subject_name: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Request type
        <input
          aria-label="Request type"
          className={inputClass}
          value={draft.request_type}
          onChange={(e) => setDraft({ ...draft, request_type: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-zinc-400">
        Handler
        <input
          aria-label="Handler"
          className={inputClass}
          value={draft.handler}
          onChange={(e) => setDraft({ ...draft, handler: e.target.value })}
        />
      </label>
      <div className="flex items-end gap-2">
        <button
          type="button"
          onClick={submitLog}
          disabled={pending}
          className="rounded-md bg-[#00C9A7] px-3 py-1.5 text-sm font-medium text-zinc-950 transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {pending ? 'Saving…' : 'Log DSAR'}
        </button>
        <button
          type="button"
          onClick={reset}
          disabled={pending}
          className="rounded-md px-3 py-1.5 text-sm text-zinc-400 hover:text-zinc-200"
        >
          Cancel
        </button>
      </div>
    </div>
  )

  if (dsars.length === 0 && !logging) {
    return (
      <div className="flex flex-col items-start gap-4">
        <div className="max-w-md rounded-xl border border-white/10 bg-white/[0.03] p-6">
          <h2 className="text-sm font-semibold text-zinc-100">No data-subject requests yet</h2>
          <p className="mt-2 text-sm leading-relaxed text-zinc-400">
            DSARs land here when you <strong>approve a request finding</strong>. Each gets a 30-day
            Article 12(3) countdown. You can also log one you received offline.
          </p>
        </div>
        {logButton}
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
              <th className="py-2 pr-4 font-medium">Received</th>
              <th className="py-2 pr-4 font-medium">Type</th>
              <th className="py-2 pr-4 font-medium">Handler</th>
              <th className="py-2 pr-4 font-medium">Deadline</th>
              <th className="py-2 pr-4 font-medium">Response sent</th>
              <th className="py-2 pr-4 font-medium">Status</th>
              <th className="py-2 font-medium sr-only">Actions</th>
            </tr>
          </thead>
          <tbody>
            {dsars.map((d) => {
              const badge = deriveDsarStatus(d)
              return (
                <tr key={d.id} className="border-t border-white/5 align-top">
                  <td className="py-3 pr-4 text-zinc-300">
                    <div>{formatDate(d.received_at)}</div>
                    {d.subject_name && (
                      <div className="text-xs text-zinc-500">{d.subject_name}</div>
                    )}
                  </td>
                  <td className="py-3 pr-4 text-zinc-300">{d.request_type || '–'}</td>
                  <td className="py-3 pr-4 text-zinc-300">{d.handler || '–'}</td>
                  <td className="py-3 pr-4 text-zinc-400">{formatDueLabel(d)}</td>
                  <td className="py-3 pr-4 text-zinc-400">{formatDate(d.responded_at)}</td>
                  <td className="py-3 pr-4">
                    <span
                      className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ${TONE_STYLE[badge.tone]}`}
                    >
                      {badge.label}
                    </span>
                  </td>
                  <td className="py-3 text-right">
                    {isOpenDsar(d) &&
                      (!canComplete ? (
                        <span
                          title="Marking a DSAR responded is a Pro feature"
                          className="rounded-md px-2 py-1 text-xs text-zinc-600"
                        >
                          Pro
                        </span>
                      ) : confirmingId === d.id ? (
                        <span className="inline-flex items-center gap-2">
                          <span className="text-xs text-zinc-400">Confirm reviewed?</span>
                          <button
                            type="button"
                            onClick={() => confirmResponded(d.id)}
                            disabled={pending}
                            className="rounded-md bg-[#00C9A7] px-2 py-1 text-xs font-medium text-zinc-950 disabled:opacity-50"
                          >
                            Confirm
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmingId(null)}
                            disabled={pending}
                            className="rounded-md px-2 py-1 text-xs text-zinc-400 hover:text-zinc-200"
                          >
                            Cancel
                          </button>
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() => {
                            setError(null)
                            setConfirmingId(d.id)
                          }}
                          className="rounded-md px-2 py-1 text-xs text-zinc-400 hover:bg-white/5 hover:text-zinc-100"
                        >
                          Mark as responded
                        </button>
                      ))}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {logging ? logForm : logButton}
    </div>
  )
}
