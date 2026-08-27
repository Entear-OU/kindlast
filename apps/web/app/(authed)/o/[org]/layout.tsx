import type { Metadata } from 'next'
import { notFound, redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/shell'
import { currentSession } from '@/lib/auth/session'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { listFindings } from '@/lib/findings/client'
import type { ActivityItem } from '@/components/console/agent-rail'

/**
 * The title every console page inherits when we do not know whose console it
 * is: nobody signed in, a slug that is not the caller's, or core-api down.
 *
 * A template rather than a bare string, so the section a page names still
 * shows. It carries no organisation, which is the whole point of it.
 */
const ANONYMOUS_TITLE: Metadata = {
  title: { template: '%s, Kindlast', default: 'Kindlast' },
}

/**
 * What a browser tab says it is showing (ENT-269).
 *
 * Until this existed, every page under `/o/{slug}/` inherited the root
 * layout's marketing title, so a consultant with three client organisations
 * open had three tabs reading "Kindlast: AI-Powered GDPR & AI Act Compliance"
 * and eleven bookmarks that all read the same. The tab strip was the last
 * place in the console where which organisation you were looking at was
 * ambiguous, and §20.1 puts the slug in the URL precisely so that it never is.
 *
 * # THE ORGANISATION GOES FIRST
 *
 * A tab truncates from the end, and the organisation is the half that differs
 * between tabs. "Ada Furniture Group, Feed" still identifies the client after
 * the strip has cut it to fifteen characters; "Feed, Ada Furniture Group"
 * would give three tabs reading "Feed..." and answer nothing.
 *
 * # THE 404 CASE
 *
 * A slug the caller does not belong to must not be distinguishable, through
 * the title or anything else, from one that does not exist. It is not, and the
 * reason is structural rather than careful: the only name this can learn comes
 * from the caller's own memberships, so for a foreign slug there is no name
 * here to leak. Both cases fall to `ANONYMOUS_TITLE`, byte for byte.
 *
 * # WHAT IT COSTS
 *
 * Nothing worth counting. `resolveOrg` reads through `loadCurrentUser`, which
 * is wrapped in React's `cache`, so this shares the one GetCurrentUser the
 * layout below already makes rather than adding a second round trip.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ org: string }>
}): Promise<Metadata> {
  const { org: slug } = await params

  const session = await currentSession()
  // The layout redirects this request to sign-in. Asking core-api who the
  // caller is, with no token to ask it with, would be a round trip for a page
  // nobody is going to see.
  if (!session) return ANONYMOUS_TITLE

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') return ANONYMOUS_TITLE

  // The slug when core-api sends no name, which is what the dashboard heading
  // already does. A title reading ", Kindlast" would be worse than a slug.
  const name = resolved.membership.orgName || slug

  return {
    title: { template: `${name}, %s, Kindlast`, default: `${name}, Kindlast` },
  }
}

/**
 * The organisation a URL names (ENT-198, §20.1, §22.4).
 *
 * Every console route lives under this layout, so this is the single place
 * that decides which organisation a request acts in. It comes from the path
 * and nowhere else: not a cookie, not the session, not the last one used.
 *
 * That is a correctness decision rather than an aesthetic one. A consultant
 * serving three client companies has three tabs open; with the active
 * organisation held in a cookie, switching in one tab silently changes what
 * the other two are showing, and an approval clicked in a stale tab is
 * recorded against the wrong company. In a compliance product that is the
 * failure the whole design exists to prevent.
 *
 * # 404, not 403
 *
 * A slug the caller does not belong to is `notFound()`. Not 403, because a 403
 * confirms that the organisation exists to someone with no business knowing,
 * and not a redirect into an organisation they DO belong to, because that
 * changes what a URL means underneath somebody who bookmarked it. The
 * resolution never consults anything but the caller's own memberships, so it
 * cannot tell "no such organisation" from "not yours" even in principle.
 */
export default async function OrgLayout({
  children,
  params,
}: {
  children: React.ReactNode
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  // The proxy checks only that a cookie is present, which is not
  // authorization. This is the check that means anything.
  if (!session) {
    redirect(`/sign-in?returnTo=${encodeURIComponent(orgPath(slug))}`)
  }

  const resolved = await resolveOrg(session.accessToken, slug)

  if (resolved.status === 'not-a-member') {
    notFound()
  }

  if (resolved.status === 'unavailable') {
    // Deliberately not a 404. core-api being unreachable says nothing about
    // whether this person belongs here, and answering "not found" would tell
    // them their organisation had been deleted during an outage.
    return (
      <ConsoleShell orgSlug={slug}>
        <main className="mx-auto w-full max-w-3xl px-4 py-12">
          <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
            Workspace unavailable
          </h1>
          <p
            data-testid="org-unavailable"
            className="mt-2 text-sm text-muted-foreground"
          >
            The workspace service could not be reached, so this organisation
            could not be loaded. Your session is unaffected: reload in a moment.
          </p>
        </main>
      </ConsoleShell>
    )
  }

  // The rail's Activity list: the newest findings, whatever their state,
  // because "the Watcher raised this yesterday" is still activity after it
  // was approved. Fetched here rather than in the rail so the rail stays a
  // plain synchronous component a test can render, and passed as absent on a
  // failed read so the rail shows nothing-listed rather than claiming
  // nothing happened.
  const recent = await listFindings(
    session.accessToken,
    resolved.membership.orgId,
    { pageSize: 3 },
  )
  const activity: ActivityItem[] | undefined = recent.ok
    ? (recent.value.findings ?? []).map((f) => ({
        id: f.findingId,
        title: f.detected,
        severity: f.severity,
        at: f.createdAt,
      }))
    : undefined

  return (
    <ConsoleShell
      orgSlug={slug}
      orgName={resolved.membership.orgName}
      activity={activity}
    >
      {children}
    </ConsoleShell>
  )
}
