import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { ActControls } from '@/components/feed/act-controls'
import { FindingNarrative } from '@/components/feed/finding-card'
import { SeverityBadge, StatusLabel } from '@/components/feed/severity'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getFinding } from '@/lib/findings/client'
import { effortSentence } from '@/lib/findings/effort'

import { approve, reject, snooze } from '../actions'

/**
 * One finding, with the regulation behind it (ENT-203).
 *
 * This page is where the product's central claim is either true or it is not.
 * A founder should be able to read what we say, read the passage we say it
 * from, and disagree with us. Everything below the heading exists for that.
 *
 * The quoted text comes from `finding_supporting_chunks`, which reads the
 * committed corpus. Nothing on this page is generated, and nothing is assembled
 * from parts: the citation label is the one stored when the finding was
 * created, and the passages are quoted verbatim.
 */
export default async function FindingPage({
  params,
}: {
  params: Promise<{ org: string; id: string }>
}) {
  const { org: slug, id } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, `/feed/${id}`))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Feed" />

  const result = await getFinding(
    session.accessToken,
    resolved.membership.orgId,
    id,
  )

  // A finding in another organisation and a finding that never existed answer
  // alike, all the way from the policy to here. Rendering 404 for both is what
  // stops this page being a way to ask whether a given finding exists in
  // someone else's organisation.
  if (!result.ok && result.error.kind === 'missing') notFound()

  if (!result.ok) {
    const denied = result.error.kind === 'denied'
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-8">
        <BackLink slug={slug} />
        <p
          data-testid={`finding-${result.error.kind}`}
          className="mt-6 rounded-xl border border-border/60 bg-muted/40 px-4 py-6 text-sm text-muted-foreground"
        >
          {denied
            ? 'Your session is not yet authorised to read findings. This is a known gap in sign-in (ENT-221) rather than a permission an owner can grant you.'
            : 'This finding could not be loaded just now. This is usually temporary; reloading is worth a try.'}
        </p>
      </div>
    )
  }

  const finding = result.value.finding
  if (!finding) notFound()

  const supporting = result.value.supporting ?? []

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <BackLink slug={slug} />

      <div className="mt-6 flex flex-wrap items-center gap-2">
        <SeverityBadge severity={finding.severity} />
        <StatusLabel status={finding.status} />
      </div>

      {/* The heading is `detected` here for the same reason it is on the card:
          it is a phrase the Watcher wrote, and the Analyst's prose goes in the
          section below rather than over it (ENT-164). */}
      <h1 className="mt-3 text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {finding.detected}
      </h1>

      {/* In full here, and clamped on the card. This is the page somebody
          opened to understand one finding, so the explanation belongs above
          the action it is the reason for. Renders nothing when no run has
          happened, which is most findings. */}
      <FindingNarrative finding={finding} />

      {finding.proposedAction ? (
        <section className="mt-6">
          <h2 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
            What to do
          </h2>
          <p className="mt-2 text-[15px] text-foreground">
            {finding.proposedAction}
          </p>
          {effortSentence(finding.effortEstimate) ? (
            <p className="mt-1 text-sm text-muted-foreground">
              {effortSentence(finding.effortEstimate)}
            </p>
          ) : null}
        </section>
      ) : null}

      <section className="mt-8">
        <h2 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
          The regulation
        </h2>

        {finding.citation?.label ? (
          <p className="mt-2 font-mono text-sm text-foreground">
            {finding.citation.url ? (
              <a
                href={finding.citation.url}
                target="_blank"
                rel="noreferrer"
                className="underline underline-offset-4 hover:opacity-80"
              >
                {finding.citation.label}
              </a>
            ) : (
              // No anchor for this provision in the corpus. The label shows
              // unlinked rather than pointing at a URL we guessed.
              finding.citation.label
            )}
          </p>
        ) : null}

        {finding.citation?.title ? (
          <p className="mt-1 text-sm text-muted-foreground">
            {finding.citation.title}
          </p>
        ) : null}

        {supporting.length > 0 ? (
          <ol className="mt-4 space-y-4">
            {supporting.map((chunk, index) => (
              <li
                key={chunk.ordinal ?? index}
                className="border-l-2 border-border/60 pl-4"
              >
                {chunk.label ? (
                  <p className="font-mono text-xs text-muted-foreground">
                    {chunk.label}
                  </p>
                ) : null}
                {chunk.quotedText ? (
                  <blockquote className="mt-1 text-sm whitespace-pre-line text-foreground">
                    {chunk.quotedText}
                  </blockquote>
                ) : null}
              </li>
            ))}
          </ol>
        ) : (
          /* Honest rather than empty. The corpus does not cover every
             instrument an obligation can cite, and "we cannot show you the
             text" is a different statement from "there is no citation". */
          <p className="mt-4 text-sm text-muted-foreground">
            We do not hold the text of this provision, so it is not quoted here.
            The citation above is what the finding was raised against.
          </p>
        )}
      </section>

      <div className="mt-8">
        <ActControls
          slug={slug}
          findingId={finding.findingId}
          status={finding.status}
          actions={{ approve, reject, snooze }}
        />
      </div>

      {finding.rejectionReason ? (
        <p className="mt-4 text-sm text-muted-foreground">
          Rejected: {finding.rejectionReason}
        </p>
      ) : null}
    </div>
  )
}

function BackLink({ slug }: { slug: string }) {
  return (
    <Link
      href={orgPath(slug, '/feed')}
      className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground"
    >
      Back to the feed
    </Link>
  )
}
