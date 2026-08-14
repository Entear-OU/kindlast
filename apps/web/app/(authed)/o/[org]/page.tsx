import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { currentSession } from '@/lib/auth/session'
import { orgPath, resolveOrg } from '@/lib/auth/org'

/**
 * An organisation's home (ENT-197, re-homed under `/o/[org]/` by ENT-198).
 *
 * Still deliberately small. Its job is to prove the arrangement works end to
 * end: a session read from Redis, an access token core-api verifies for
 * itself, and an organisation identified by the URL rather than by a cookie.
 * The dashboard, feed, records and settings return here as each surface is
 * rebuilt on core-api (ENT-200), and they will be siblings of this page rather
 * than replacements for it.
 *
 * The resolution is repeated from the layout rather than passed down, because
 * a layout cannot hand props to a page. It costs nothing: `loadCurrentUser` is
 * request-cached, so both get the same answer from one round trip.
 */
export default async function OrgHomePage({ params }: { params: Promise<{ org: string }> }) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session) redirect(`/sign-in?returnTo=${encodeURIComponent(orgPath(slug))}`)

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  // The layout has already rendered the unavailable state around this.
  if (resolved.status === 'unavailable') return null

  const { me, membership } = resolved
  const others = me.memberships.filter((m) => m.orgSlug !== slug)

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {membership.orgName || slug}
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Signed in{me.user?.email ? ` as ${me.user.email}` : ''}. Your session is held on the
        server; this browser holds an identifier and nothing else.
      </p>

      <section className="mt-8">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Active organisation
        </h2>

        <div
          data-testid="active-org"
          className="mt-3 rounded-lg border border-border/60 bg-background p-4"
        >
          <p className="text-[15px] font-medium text-foreground">
            {membership.orgName || membership.orgId}
          </p>
          <p className="mt-1 font-mono text-xs text-muted-foreground">/o/{slug}</p>
          {membership.role ? (
            <p className="mt-1 text-sm text-muted-foreground">Your role: {membership.role}</p>
          ) : null}
        </div>
      </section>

      {others.length > 0 ? (
        <section className="mt-8">
          <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Other organisations
          </h2>
          {/* Links rather than labels: switching organisation is a navigation,
              which is the point of routing on the slug. */}
          <ul className="mt-3 space-y-2">
            {others.map((m) => (
              <li key={m.orgId} className="text-sm">
                {m.orgSlug ? (
                  <Link
                    href={orgPath(m.orgSlug)}
                    className="text-foreground underline underline-offset-4 hover:opacity-80"
                  >
                    {m.orgName || m.orgSlug}
                  </Link>
                ) : (
                  <span className="text-muted-foreground">{m.orgName || m.orgId}</span>
                )}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </main>
  )
}
