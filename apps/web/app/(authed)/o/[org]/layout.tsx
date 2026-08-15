import { notFound, redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/shell'
import { currentSession } from '@/lib/auth/session'
import { orgPath, resolveOrg } from '@/lib/auth/org'

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

  return (
    <ConsoleShell orgSlug={slug} orgName={resolved.membership.orgName}>
      {children}
    </ConsoleShell>
  )
}
