import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { FindingCard } from '@/components/feed/finding-card'
import { PipelineNote, PostureBand } from '@/components/feed/posture-band'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getDashboard, listFindings, type Failure } from '@/lib/findings/client'

/**
 * The feed (ENT-203), the first of ENT-200's six surfaces to return.
 *
 * Three reads, and each degrades on its own. The posture band and the list come
 * from different RPCs, so one being unavailable must not blank the other: a
 * customer whose dashboard call failed can still work through their findings,
 * and telling them the whole page is broken would be a worse answer than the
 * one we have.
 *
 * WHAT A REAL PERSON SEES
 *
 * The feed. This comment previously said `permission_denied`, because no human
 * token carried `findings:read` at all: the seed created the project roles and
 * granted them to nobody, so a browser session's token had an empty role set.
 * ENT-221 fixed that twice over, first by granting the roles and then by
 * deriving a human's scope set from the token's client rather than from grants,
 * and a signed-in person now reaches the feed. Verified in a browser.
 *
 * What survives from that note is the rule it was written to protect: an empty
 * feed is a claim. It says we looked and found nothing. So a failed read is
 * always reported as itself and never rendered as an empty list.
 */
export default async function FeedPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>
  searchParams: Promise<{ status?: string; page?: string }>
}) {
  const { org: slug } = await params
  const { status, page } = await searchParams

  const session = await currentSession()
  if (!session)
    redirect(`/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/feed'))}`)

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable') return null

  const orgId = resolved.membership.orgId

  // Concurrent rather than sequential: they are independent reads and the feed
  // should not wait on the dashboard to render.
  const [dashboard, feed] = await Promise.all([
    getDashboard(session.accessToken, orgId),
    listFindings(session.accessToken, orgId, {
      status: status || undefined,
      pageToken: page || undefined,
    }),
  ])

  const findings = feed.ok ? (feed.value.findings ?? []) : []
  const nextPageToken = feed.ok ? feed.value.nextPageToken : undefined

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Feed
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        What the Watcher has found, newest first. Every finding cites the
        regulation it comes from, so you can check it rather than take our word.
      </p>

      <div className="mt-6">
        {dashboard.ok ? (
          <>
            <PostureBand dashboard={dashboard.value} />
            <div className="mt-2">
              <PipelineNote dashboard={dashboard.value} />
            </div>
          </>
        ) : (
          <Unavailable what="posture" error={dashboard.error} />
        )}
      </div>

      <StatusFilter slug={slug} active={status} />

      <div className="mt-4">
        {!feed.ok ? (
          <Unavailable what="feed" error={feed.error} />
        ) : findings.length === 0 ? (
          <EmptyFeed filtered={Boolean(status)} />
        ) : (
          <ul className="space-y-3">
            {findings.map((finding) => (
              <FindingCard
                key={finding.findingId}
                finding={finding}
                orgSlug={slug}
              />
            ))}
          </ul>
        )}
      </div>

      {/* Forward-only, because the cursor is opaque and encodes one position.
          A "previous" link would need a stack of tokens held somewhere, and the
          browser's back button already does that job correctly. */}
      {nextPageToken ? (
        <div className="mt-6">
          <Link
            href={`${orgPath(slug, '/feed')}?${new URLSearchParams({
              ...(status ? { status } : {}),
              page: nextPageToken,
            })}`}
            className="text-sm text-foreground underline underline-offset-4 hover:opacity-80"
          >
            Older findings
          </Link>
        </div>
      ) : null}
    </div>
  )
}

const FILTERS = [
  { label: 'All', value: '' },
  { label: 'Needs a decision', value: 'pending' },
  { label: 'Approved', value: 'approved' },
  { label: 'Deferred', value: 'snoozed' },
  { label: 'Rejected', value: 'rejected' },
] as const

/**
 * Links rather than a form, so a filtered feed is a URL somebody can share and
 * the back button behaves.
 *
 * Changing the filter drops the page cursor, which it must: a cursor names a
 * position in one ordering, and carrying it across a filter change would resume
 * partway through a list the person has not seen the start of.
 */
function StatusFilter({ slug, active }: { slug: string; active?: string }) {
  return (
    <nav aria-label="Filter findings" className="mt-8 flex flex-wrap gap-2">
      {FILTERS.map(({ label, value }) => {
        const current = (active ?? '') === value
        const href = value
          ? `${orgPath(slug, '/feed')}?status=${value}`
          : orgPath(slug, '/feed')

        return (
          <Link
            key={label}
            href={href}
            aria-current={current ? 'page' : undefined}
            className={`rounded-full border px-3 py-1 text-xs transition-colors ${
              current
                ? 'border-primary/40 bg-primary/10 text-primary'
                : 'border-border/60 text-muted-foreground hover:border-border hover:text-foreground'
            }`}
          >
            {label}
          </Link>
        )
      })}
    </nav>
  )
}

/**
 * An empty feed says we looked and found nothing, so it is only shown when that
 * is true. Every other reason to have no rows is reported as itself.
 */
function EmptyFeed({ filtered }: { filtered: boolean }) {
  return (
    <p
      data-testid="feed-empty"
      className="rounded-xl border border-dashed border-border/60 px-4 py-10 text-center text-sm text-muted-foreground"
    >
      {filtered
        ? 'No findings with that status.'
        : 'No findings yet. When the Watcher runs, what it finds appears here.'}
    </p>
  )
}

/**
 * A failed read, in words that say what to do about it.
 *
 * `denied` is spelled out rather than shown as a generic error, but what it
 * says changed with ENT-221. It used to send people to a known gap in sign-in
 * and tell them an owner could not help, which was true then and is not now: a
 * signed-in person holds `findings:read`, so a denial today means something a
 * person can actually act on.
 */
function Unavailable({ what, error }: { what: string; error: Failure }) {
  const message =
    error.kind === 'denied'
      ? 'Your session is not permitted to read findings. Reading the feed needs the findings:read scope; an owner can grant it.'
      : `The ${what} could not be loaded just now. This is usually temporary; reloading is worth a try.`

  return (
    <p
      data-testid={`feed-${error.kind}`}
      className="rounded-xl border border-border/60 bg-muted/40 px-4 py-6 text-sm text-muted-foreground"
    >
      {message}
    </p>
  )
}
