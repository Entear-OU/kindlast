'use client'

import { useMemo, useState } from 'react'

import {
  FEED_SEVERITIES,
  FEED_STATUSES,
  filterFindings,
  severityChip,
  statusLabel,
  type Finding,
  type FindingSeverity,
  type FindingStatus,
} from '@/lib/feed/findings'

/**
 * The Agent feed (ENT-62) — every finding the agents produced, newest first,
 * with status + severity filters and a friendly empty state. Read-only:
 * Approve / Reject / Snooze land in ENT-63.
 *
 * `pendingLimit` is the ENT-82 seam (Free-tier 3-pending cap). Unused here —
 * enforcement needs the subscriptions table (ENT-81).
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
}: {
  findings: Finding[]
  /** ENT-82 seam: cap visible pending findings on the Free tier. Not enforced yet. */
  pendingLimit?: number
}) {
  const [status, setStatus] = useState<StatusChoice>('all')
  const [severity, setSeverity] = useState<SeverityChoice>('all')

  const visible = useMemo(
    () => filterFindings(findings, { status, severity }),
    [findings, status, severity],
  )

  // Nothing has ever been detected — the friendly all-clear (AC).
  if (findings.length === 0) {
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
            <FindingCard key={f.id} finding={f} />
          ))}
        </ul>
      )}
    </div>
  )
}

function FindingCard({ finding }: { finding: Finding }) {
  const sev = severityChip(finding.severity)
  return (
    <li className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-semibold text-zinc-100">{finding.detected}</h3>
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
    </li>
  )
}
