'use client'

import Link from 'next/link'

import type { Plan } from '@/lib/billing/plan'
import {
  gateChunks,
  severityRationale,
  type FindingDetail as FindingDetailData,
  type SupportingChunk,
} from '@/lib/feed/finding-detail'
import { severityChip, statusLabel } from '@/lib/feed/findings'

/**
 * ENT-64 — the finding DETAIL view. The founder expands a finding to read the
 * Analyst's full reasoning and the source citations that justify a decision to
 * an auditor. Presentational + read-only: a sibling route fetches the data and
 * renders this on the finding's permalink page.
 *
 * Free-tier sees the first supporting source and an upgrade prompt for the rest
 * (the gate lives in `gateChunks`); Pro sees them all. The component export is
 * `FindingDetailView` to avoid clashing with the imported `FindingDetail` type
 * (imported here as `FindingDetailData`).
 */
export function FindingDetailView({
  finding,
  chunks,
  plan,
}: {
  finding: FindingDetailData
  chunks: SupportingChunk[]
  plan: Plan
}) {
  const sev = severityChip(finding.severity)
  const gated = gateChunks(chunks, plan)

  return (
    <article className="flex flex-col gap-6">
      <Link
        href="/feed"
        className="text-xs text-zinc-500 underline-offset-2 transition-colors hover:text-zinc-300 hover:underline"
      >
        ← Back to feed
      </Link>

      <header className="flex flex-col gap-3">
        <h1 className="text-xl font-semibold text-zinc-100">{finding.detected}</h1>
        <div className="flex flex-wrap items-center gap-2">
          <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${sev.className}`}>
            {sev.label}
          </span>
          <span className="rounded-full border border-white/10 px-2 py-0.5 text-xs text-zinc-300">
            {statusLabel(finding.status)}
          </span>
          <span className="text-xs text-zinc-500">Effort: {finding.effort_estimate}</span>
        </div>
      </header>

      <section className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
        <h2 className="text-xs font-medium uppercase tracking-wide text-zinc-500">
          Why this matters
        </h2>
        <p className="mt-2 text-sm leading-relaxed text-zinc-300">
          {severityRationale(finding.severity)}
        </p>
      </section>

      {finding.regulatory_obligation ? (
        <section className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
          <h2 className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            Mapped obligation
          </h2>
          <p className="mt-2 text-sm text-zinc-200">
            {finding.citation_url ? (
              <a
                href={finding.citation_url}
                target="_blank"
                rel="noreferrer"
                className="underline decoration-zinc-600 underline-offset-2 hover:text-zinc-100"
              >
                {finding.regulatory_obligation}
              </a>
            ) : (
              finding.regulatory_obligation
            )}
          </p>
          {finding.supporting_context ? (
            <p className="mt-2 text-sm leading-relaxed text-zinc-400 whitespace-pre-line">
              {finding.supporting_context}
            </p>
          ) : null}
        </section>
      ) : null}

      <section className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
        <h2 className="text-xs font-medium uppercase tracking-wide text-zinc-500">
          Proposed action
        </h2>
        <p className="mt-2 text-sm leading-relaxed text-zinc-200">{finding.proposed_action}</p>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-xs font-medium uppercase tracking-wide text-zinc-500">
          Supporting sources
        </h2>

        {chunks.length === 0 ? (
          <p className="text-sm text-zinc-500">No supporting sources available.</p>
        ) : (
          <>
            {gated.visible.map((c) => (
              <div
                key={c.ordinal}
                className="rounded-xl border border-white/10 bg-white/[0.03] p-4"
              >
                <h3 className="text-sm font-semibold text-zinc-100">{c.label}</h3>
                <blockquote className="mt-2 border-l-2 border-white/10 pl-3 text-sm leading-relaxed text-zinc-300 whitespace-pre-line">
                  {c.quoted_text}
                </blockquote>
                {c.source_url ? (
                  <a
                    href={c.source_url}
                    target="_blank"
                    rel="noreferrer"
                    className="mt-2 inline-block text-xs text-zinc-400 underline decoration-zinc-600 underline-offset-2 hover:text-zinc-200"
                  >
                    View source
                  </a>
                ) : null}
              </div>
            ))}

            {gated.lockedCount > 0 ? (
              <div className="rounded-xl border border-[#00C9A7]/30 bg-[#00C9A7]/[0.06] p-4">
                <p className="text-sm text-zinc-200">
                  {gated.lockedCount} more supporting source
                  {gated.lockedCount === 1 ? '' : 's'} available on Pro.
                </p>
                <a
                  href="/billing"
                  className="mt-3 inline-block rounded-md bg-[#00C9A7] px-3 py-1.5 text-sm font-medium text-zinc-950 transition-opacity hover:opacity-90"
                >
                  Upgrade to Pro
                </a>
              </div>
            ) : null}
          </>
        )}
      </section>

      {finding.status === 'rejected' && finding.rejection_reason ? (
        <section className="rounded-xl border border-white/10 bg-white/[0.02] p-4">
          <h2 className="text-xs font-medium uppercase tracking-wide text-zinc-500">
            Rejection reason
          </h2>
          <p className="mt-2 text-sm leading-relaxed text-zinc-500">{finding.rejection_reason}</p>
        </section>
      ) : null}
    </article>
  )
}
