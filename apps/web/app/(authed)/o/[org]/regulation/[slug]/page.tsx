import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { CitationLink } from '@/components/corpus/obligation-list'
import { getObligation } from '@/lib/corpus/client'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * One obligation, and the regulatory text behind it (ENT-207).
 *
 * # THIS IS WHERE "SAYS WHO" LANDS
 *
 * A finding claims an organisation has a duty. This page is the answer to the
 * next question, and it has three layers on purpose: what the duty is in plain
 * language, what the provision it comes from says, and a link to the official
 * text so the reader can stop trusting us entirely.
 *
 * The third layer is the important one. The corpus stores no verbatim Official
 * Journal wording, only curated summaries, so the link is not a convenience: it
 * is where the authoritative text actually is, and the summaries are explicitly
 * ours rather than the law's.
 *
 * # AN EMPTY CITED SUMMARY IS A REAL CONDITION, NOT A BLANK
 *
 * Ingest refuses to store an obligation whose citation resolves to nothing, so
 * reaching this page with no text means either the corpus was written some
 * other way or a regulation was loaded without the one this cites. Either is
 * worth saying out loud, because the alternative is a page that looks like it
 * failed to render.
 */
export default async function ObligationPage({
  params,
}: {
  params: Promise<{ org: string; slug: string }>
}) {
  const { org: orgSlug, slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(orgSlug, `/regulation/${slug}`))}`,
    )

  const resolved = await resolveOrg(session.accessToken, orgSlug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Regulation" />

  const result = await getObligation(
    session.accessToken,
    resolved.membership.orgId,
    slug,
  )

  // The corpus is the same for everybody, so an unknown slug is simply unknown
  // and 404 leaks nothing. That is a real difference from the tenant surfaces,
  // where 404 is doing the work of hiding whose data it is.
  if (!result.ok && result.error.kind === 'missing') notFound()
  if (!result.ok)
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-8">
        <p className="text-sm text-muted-foreground">
          Could not load this obligation. Reload to try again.
        </p>
      </div>
    )

  const { obligation, citedSummary, citedHeading, citedParagraphSummary } =
    result.value
  if (!obligation) notFound()

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <Link
        href={orgPath(orgSlug, '/regulation')}
        className="text-sm text-muted-foreground underline underline-offset-4"
      >
        Regulation
      </Link>

      <h1 className="mt-4 text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {obligation.title}
      </h1>

      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
        <CitationLink citation={obligation.citation} />
        {obligation.severity ? (
          <span>{obligation.severity} severity</span>
        ) : null}
        {obligation.recurrence ? <span>{obligation.recurrence}</span> : null}
        {obligation.effectiveDate ? (
          <span>Applies from {obligation.effectiveDate}</span>
        ) : null}
      </div>

      <section className="mt-8">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          What this requires
        </h2>
        <p className="mt-2 text-sm leading-relaxed text-foreground">
          {obligation.summary}
        </p>
      </section>

      <section className="mt-8">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          The provision
        </h2>

        {citedSummary ? (
          <div className="mt-2 rounded-xl border border-border/60 bg-background p-5">
            {citedHeading ? (
              <p className="text-sm font-semibold text-foreground">
                {citedHeading}
              </p>
            ) : null}
            <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
              {citedSummary}
            </p>
            {citedParagraphSummary ? (
              <p className="mt-3 border-l-2 border-border pl-3 text-sm leading-relaxed text-muted-foreground">
                {citedParagraphSummary}
              </p>
            ) : null}
            {/* Stated rather than implied. These are our summaries, and a
                reader deciding whether to act on a finding should know they are
                reading a paraphrase and where the real text is. */}
            <p className="mt-4 text-xs text-muted-foreground">
              A summary, not the official wording.{' '}
              {obligation.citation?.url ? (
                <a
                  href={obligation.citation.url}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="underline underline-offset-4"
                >
                  Read the provision on EUR-Lex
                </a>
              ) : null}
            </p>
          </div>
        ) : (
          <p className="mt-2 text-sm text-muted-foreground">
            The text behind this citation is not in this deployment&rsquo;s
            corpus, so it cannot be shown here. The citation still names the
            provision, and the official text is the authority either way.
          </p>
        )}
      </section>

      {obligation.actionType && obligation.actionType !== 'review' ? (
        <section className="mt-8">
          <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            What approving a finding does
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            {DESCRIBES_ACTION[obligation.actionType] ??
              `Runs ${obligation.actionType}.`}
          </p>
        </section>
      ) : null}
    </div>
  )
}

/**
 * What the Executor does when a finding for this obligation is approved.
 *
 * Spelled out because approving a finding writes to the compliance record, and
 * a person should know what a button is about to create before they press it
 * rather than after. `review` is omitted by the caller: it means approving
 * records the decision and creates nothing, which needs no explanation.
 */
const DESCRIBES_ACTION: Record<string, string> = {
  create_ropa:
    'Creates an entry in the Article 30 record of processing activities, pre-filled and marked as needing review.',
  create_ai_system:
    'Creates an entry in the AI system register, pre-filled and marked as needing review.',
  create_dsar: 'Creates an entry in the data subject request log.',
}
