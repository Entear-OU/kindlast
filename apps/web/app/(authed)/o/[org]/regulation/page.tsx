import type { Metadata } from 'next'
import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { ObligationList } from '@/components/corpus/obligation-list'
import { listDocuments, listObligations } from '@/lib/corpus/client'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Regulation',
}

/**
 * Regulation: what this deployment checks against (ENT-207).
 *
 * # THE PAGE THE PRODUCT'S CLAIM RESTS ON
 *
 * A finding says an organisation has an obligation and cites the law it comes
 * from. The reason anybody should believe that is that they can go and look,
 * and this is the looking. AGENTS.md opens by saying anything fabricating a
 * citation is worse than nothing, because the value is that a human can check
 * the claim against the law; until ENT-207 the corpus tables were empty and
 * there was nothing to check against.
 *
 * # IT SAYS WHICH VERSION OF THE LAW IT HOLDS
 *
 * The regulation cards carry a CELEX number, a version date and the counts.
 * That is not decoration. A compliance product that cannot say which text it is
 * working from is asking for trust it has not earned, and "the GDPR, 99
 * articles, as at 4 May 2016" is a claim a customer can check in one click.
 *
 * # SEVERITY HERE IS THE LAW'S, NOT THIS ORGANISATION'S
 *
 * Nothing on this page is about whether this customer is compliant. It is a
 * reference: the same rows for every organisation, from tables with no
 * `org_id`. The findings feed is where an obligation becomes a claim about
 * somebody, and keeping the two apart is what stops a reference page reading as
 * an alarm.
 */
export default async function RegulationPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/regulation'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Regulation" />

  const { membership } = resolved

  // Both in parallel: neither depends on the other, and the page is a single
  // render rather than a sequence.
  const [obligationsResult, documentsResult] = await Promise.all([
    listObligations(session.accessToken, membership.orgId),
    listDocuments(session.accessToken, membership.orgId),
  ])

  const obligations = obligationsResult.ok
    ? (obligationsResult.value.obligations ?? [])
    : []
  const documents = documentsResult.ok
    ? (documentsResult.value.documents ?? [])
    : []

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Regulation
      </h1>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        The law this workspace checks against, and the duties derived from it.
        Every finding cites one of these, and every citation links to the
        official text rather than to a copy held here.
      </p>

      <section className="mt-8" aria-labelledby="sources">
        <h2
          id="sources"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          Sources
        </h2>

        {documents.length === 0 ? (
          // The state this deployment was in before ENT-207, and it has to say
          // so rather than render an empty box: a corpus that has not been
          // loaded is a real condition an operator can fix, and it makes every
          // citation on every finding fall back to a raw CELEX number.
          <p className="mt-3 text-sm text-muted-foreground">
            No regulation has been loaded into this deployment yet, so findings
            cannot show the text behind their citations. An operator loads it
            with the corpus ingest.
          </p>
        ) : (
          <ul className="mt-3 grid gap-3 sm:grid-cols-2">
            {documents.map((document) => (
              <li
                key={document.celexNumber}
                className="rounded-xl border border-border/60 bg-background p-4"
              >
                <p className="text-sm font-semibold text-foreground">
                  {document.shortTitle}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {document.articleCount} articles, {document.recitalCount}{' '}
                  recitals
                  {document.annexCount
                    ? `, ${document.annexCount} annexes`
                    : ''}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Text as at {document.versionDate}. CELEX{' '}
                  {document.celexNumber}.
                </p>
                {document.officialUrl ? (
                  <a
                    href={document.officialUrl}
                    target="_blank"
                    rel="noreferrer noopener"
                    className="mt-2 inline-block text-xs underline underline-offset-4"
                  >
                    Read the official text
                  </a>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="mt-10" aria-labelledby="obligations">
        <h2
          id="obligations"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          Obligations
        </h2>

        {!obligationsResult.ok ? (
          // Not an empty list. Rendering "no obligations" because a request
          // failed would tell a customer this product checks nothing.
          <p className="mt-3 text-sm text-muted-foreground">
            Could not load the obligations. Reload to try again.
          </p>
        ) : obligations.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">
            No obligations have been loaded, so nothing is being checked yet.
          </p>
        ) : (
          <div className="mt-3">
            <ObligationList
              obligations={obligations}
              hrefFor={(obligationSlug) =>
                orgPath(slug, `/regulation/${obligationSlug}`)
              }
            />
          </div>
        )}
      </section>
    </div>
  )
}
