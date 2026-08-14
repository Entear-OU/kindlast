import { redirect } from 'next/navigation'

import { currentSession } from '@/lib/auth/session'
import { getCurrentUser } from '@/lib/auth/client'

/**
 * The authenticated entry point on the self-managed stack (ENT-197).
 *
 * Deliberately small. Its job is to be the first page that proves the whole
 * arrangement works end to end: a session read from Redis, an access token
 * that core-api verifies for itself, and an organisation that exists because
 * signing in created it. Nothing here talks to Supabase, which is the point.
 *
 * It is not the console. The dashboard, feed, records and settings still gate
 * on a Supabase session and still read Supabase for their data, so they are
 * unreachable until they are ported: that is a migration of its own rather
 * than something to smuggle into the auth work. ENT-198's `/o/[org]/` routing
 * is what this page grows into.
 */
export default async function WorkspacePage() {
  const session = await currentSession()

  // The proxy checks only that a cookie is present, which is not
  // authorization. This is the check that means anything.
  if (!session) redirect('/sign-in?returnTo=%2Fworkspace')

  const me = await getCurrentUser(session.accessToken)
  const memberships = me?.memberships ?? []
  const active =
    memberships.find((m) => m.orgId === session.orgId) ?? memberships[0] ?? null

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">Workspace</h1>

      {me ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Signed in{me.user?.email ? ` as ${me.user.email}` : ''}. Your session is held on the
          server; this browser holds an identifier and nothing else.
        </p>
      ) : (
        <p className="mt-2 text-sm text-muted-foreground">
          Signed in. The workspace service could not be reached, so your organisations are not
          listed below. Your session is unaffected.
        </p>
      )}

      <section className="mt-8">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Active organisation
        </h2>

        {active ? (
          <div
            data-testid="active-org"
            className="mt-3 rounded-lg border border-border/60 bg-background p-4"
          >
            <p className="text-[15px] font-medium text-foreground">
              {active.orgName || active.orgId}
            </p>
            {active.role ? (
              <p className="mt-1 text-sm text-muted-foreground">Your role: {active.role}</p>
            ) : null}
          </div>
        ) : (
          <p data-testid="no-org" className="mt-3 text-sm text-muted-foreground">
            You do not belong to an organisation yet.
          </p>
        )}
      </section>

      {memberships.length > 1 ? (
        <section className="mt-8">
          <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Other organisations
          </h2>
          <ul className="mt-3 space-y-2">
            {memberships
              .filter((m) => m.orgId !== active?.orgId)
              .map((m) => (
                <li key={m.orgId} className="text-sm text-foreground">
                  {m.orgName || m.orgId}
                </li>
              ))}
          </ul>
        </section>
      ) : null}
    </main>
  )
}
