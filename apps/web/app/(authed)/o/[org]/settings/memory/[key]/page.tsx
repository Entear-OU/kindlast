import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import {
  FACT_LABELS,
  SOURCE_LABELS,
  getFactHistory,
  readValue,
  type ProfileFactKey,
} from '@/lib/memory/client'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Fact',
}

/**
 * What we used to think (ENT-228, §26.5).
 *
 * # THE PAGE THAT MAKES CORRECTION MEAN SOMETHING
 *
 * Without it, correcting a fact is indistinguishable from our having always
 * thought the new thing. The question somebody checking an older finding is
 * asking is precisely what we believed at the time, and this is the only place
 * that answers it.
 *
 * # KEYED BY THE FACT, NOT BY A ROW
 *
 * The URL names the fact rather than the currently open row's id, because that
 * id changes every time the fact is corrected, which is exactly when somebody
 * would want to look at its history. A key is stable and is a link somebody
 * can keep.
 *
 * An unknown key is a 404 rather than an empty history. A fact this product
 * does not understand is not a fact with nothing recorded against it, and
 * saying "no history" would send a reader looking for a correction that never
 * happened.
 */
export default async function FactHistoryPage({
  params,
}: {
  params: Promise<{ org: string; key: string }>
}) {
  const { org: slug, key } = await params

  if (!(key in FACT_LABELS)) notFound()
  const factKey = key as ProfileFactKey

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(
        orgPath(slug, `/settings/memory/${key}`),
      )}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="History" />

  const result = await getFactHistory(
    session.accessToken,
    resolved.membership.orgId,
    factKey,
  )
  const facts = result.ok ? (result.value.facts ?? []) : []

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <Link
        href={orgPath(slug, '/settings/memory')}
        className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
      >
        What Kindlast knows about you
      </Link>

      <h1 className="mt-3 text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {FACT_LABELS[factKey]}
      </h1>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        Every answer we have held, newest first. Nothing here is ever removed or
        rewritten, including by us.
      </p>

      {facts.length === 0 ? (
        <p className="mt-6 text-sm text-muted-foreground">
          Nothing recorded for this yet.
        </p>
      ) : (
        <ol className="mt-6 divide-y divide-border/60 rounded-xl border border-border/60 bg-background">
          {facts.map((fact) => (
            <li key={`${fact.validFrom}`} className="p-4">
              <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
                <p className="text-sm font-medium text-foreground">
                  {readValue(fact) ?? 'Not recorded'}
                </p>
                {/* The open value, labelled. Without it a reader has to work
                    out which row is current by comparing dates, and the whole
                    point of the page is that several answers coexist. */}
                {fact.validTo ? null : (
                  <span className="text-xs font-medium text-foreground">
                    Current
                  </span>
                )}
              </div>

              <p className="mt-1 text-xs text-muted-foreground">
                {fact.source
                  ? (SOURCE_LABELS[fact.source] ?? fact.source)
                  : null}
                {fact.validFrom ? ` · from ${formatDay(fact.validFrom)}` : null}
                {fact.validTo ? ` until ${formatDay(fact.validTo)}` : null}
              </p>

              {/* Why, when somebody said why. The correction form asks for it
                  and the column has always stored it, but nothing read it
                  back, so the one part of the record that explains a change
                  was invisible to everyone without a database client.

                  Quoted rather than paraphrased, and rendered as the person's
                  own words: this is testimony about a compliance record, and
                  the whole value of it is that it is what they actually
                  wrote. */}
              {fact.note ? (
                <p className="mt-2 border-l-2 border-border pl-3 text-xs text-muted-foreground italic">
                  “{fact.note}”
                </p>
              ) : null}
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

function formatDay(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}
