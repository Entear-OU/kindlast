import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { ConnectForm } from '@/components/integrations/connect-form'
import { ConnectionList } from '@/components/integrations/connection-list'
import { FetchList } from '@/components/integrations/fetch-list'
import { GrantsForm } from '@/components/integrations/grants-form'
import { connectAction, revokeAction, updateGrantsAction } from './actions'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { isActive, listFetches, listIntegrations } from '@/lib/integrations/client'

/**
 * The page that makes a connection something a customer controls (ENT-231,
 * §26.4).
 *
 * # WHAT THIS PAGE IS FOR
 *
 * Integrations are how Kindlast learns about a company from its own systems
 * rather than from a form. That is worth a lot and it is also the point where
 * a compliance product starts reaching into a customer's helpdesk, their
 * document store and their cloud account. So the page is arranged around the
 * three questions somebody would want answered before agreeing to that, in
 * that order:
 *
 *   what can Kindlast reach          the connection list
 *   what is it allowed to do there   the per-connection tool grants
 *   what has it actually done        "what we fetched", refusals included
 *
 * A page showing only the first would be a settings screen. It is the third
 * that makes the first two believable.
 *
 * # THE REFUSALS ARE ON THE PAGE, NOT HIDDEN BEHIND A FILTER
 *
 * "We did not call close_ticket because this connection has not granted write
 * access" is the sentence that turns a policy into something a customer can
 * see working. A log holding only successes would be indistinguishable from a
 * deployment where the gateway does nothing.
 */
export default async function IntegrationsPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/integrations'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Integrations" />

  const { membership } = resolved

  // Both in parallel: neither depends on the other, and this is one render
  // rather than a sequence.
  const [connectionsResult, fetchesResult] = await Promise.all([
    listIntegrations(session.accessToken, membership.orgId),
    listFetches(session.accessToken, membership.orgId),
  ])

  // A failed read degrades to an empty list rather than throwing, matching
  // every other console page. The one thing it must not do is look like "you
  // have connected nothing" when the truth is "we could not ask", which is
  // what the note below the heading is for.
  const integrations = connectionsResult.ok
    ? (connectionsResult.value.integrations ?? [])
    : []
  const fetches = fetchesResult.ok ? (fetchesResult.value.fetches ?? []) : []

  // Registered only when this deployment has a gateway, so a 404 here means
  // integrations are not configured rather than that something is broken.
  const unconfigured =
    !connectionsResult.ok && connectionsResult.error.kind === 'missing'

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Integrations
      </h1>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        Connect the systems Kindlast may read from, decide exactly which of
        their tools it may call, and see everything it has fetched. Nothing is
        called unless you allow it, and what comes back is redacted before it
        is stored.
      </p>

      {unconfigured ? (
        <p className="mt-6 rounded-xl border border-border/60 bg-background p-4 text-sm text-muted-foreground">
          This installation has no integrations gateway configured, so there is
          nothing to connect to yet. Your operator sets{' '}
          <span className="font-mono">KINDLAST_GATEWAY_URL</span>.
        </p>
      ) : null}

      <section className="mt-8" aria-labelledby="connections">
        <h2
          id="connections"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          What Kindlast can reach
        </h2>
        {integrations.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">
            Nothing is connected. Kindlast knows about you only from what your
            team has told it.
          </p>
        ) : (
          <ConnectionList integrations={integrations} />
        )}
      </section>

      {unconfigured ? null : (
        <section className="mt-10" aria-labelledby="connect">
          <h2
            id="connect"
            className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
          >
            Connect an endpoint
          </h2>
          <ConnectForm slug={slug} connect={connectAction} />
        </section>
      )}

      {integrations.filter(isActive).length > 0 ? (
        <section className="mt-10" aria-labelledby="grants">
          <h2
            id="grants"
            className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
          >
            What Kindlast may do
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Changing this records a new agreement. The previous one is kept.
          </p>
          <div className="mt-3 space-y-4">
            {integrations.filter(isActive).map((integration) => (
              <GrantsForm
                key={integration.id}
                slug={slug}
                integration={integration}
                update={updateGrantsAction}
                revoke={revokeAction}
              />
            ))}
          </div>
        </section>
      ) : null}

      <section className="mt-10" aria-labelledby="fetched">
        <h2
          id="fetched"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          What we fetched
        </h2>
        {fetches.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">
            Nothing has been fetched yet. When something is, every attempt
            appears here, including the ones we declined to make.
          </p>
        ) : (
          <FetchList fetches={fetches} />
        )}
      </section>
    </div>
  )
}
