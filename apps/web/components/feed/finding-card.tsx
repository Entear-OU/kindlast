import Link from 'next/link'

import { SeverityBadge, StatusLabel } from '@/components/feed/severity'
import { orgPath } from '@/lib/auth/org'
import type { Citation, Finding } from '@/lib/findings/client'

/**
 * One finding in the feed (ENT-203).
 *
 * The heading is `detected`: what the Watcher observed, in the customer's
 * terms. ENT-164 records that the old card put the narrative paragraph here
 * instead, which meant a heading three lines long. The narrative layer does not
 * exist yet and belongs to the Python service (ENT-218), so when it returns it
 * goes in the body, not the heading.
 */
export function FindingCard({
  finding,
  orgSlug,
}: {
  finding: Finding
  orgSlug: string
}) {
  return (
    <li>
      <Link
        href={orgPath(orgSlug, `/feed/${finding.findingId}`)}
        className="block rounded-xl border border-border/60 bg-background p-4 transition-colors hover:border-border hover:bg-muted/40"
      >
        <div className="flex flex-wrap items-center gap-2">
          <SeverityBadge severity={finding.severity} />
          <StatusLabel status={finding.status} />
        </div>

        <p className="mt-2 text-[15px] font-medium text-foreground">
          {finding.detected}
        </p>

        {finding.proposedAction ? (
          <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
            {finding.proposedAction}
          </p>
        ) : null}

        <CitationLine citation={finding.citation} />
      </Link>
    </li>
  )
}

/**
 * The citation, as stored.
 *
 * Renders `label` and nothing else. It is not assembled here from `celex` and
 * `article`, and it must not be: a second assembler is a second thing that can
 * disagree with what the Analyst recorded, and the product's whole claim is
 * that a human can check this against the law.
 *
 * A finding with no stored label shows nothing rather than a guess. Silence is
 * recoverable; an invented citation is the one failure this product cannot
 * afford.
 */
export function CitationLine({ citation }: { citation?: Citation }) {
  if (!citation?.label) return null

  return (
    <p className="mt-3 font-mono text-xs text-muted-foreground">
      {citation.label}
    </p>
  )
}
