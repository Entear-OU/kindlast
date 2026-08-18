import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { AgentProfile } from '@/components/agents/agent-profile'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { PipelineNote } from '@/components/feed/posture-band'
import { agentBySlug } from '@/lib/agents/catalog'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getDashboard } from '@/lib/findings/client'

/**
 * One agent, addressable by name (ENT-232, §26.5).
 *
 * The rail names four agents and, until this page, a customer had no way to
 * find out what any of them was allowed to do. That is a strange gap for a
 * product whose claim is that a human can check what it did: the four are
 * described in marketing copy and nowhere a signed-in person could reach.
 *
 * # WHAT IS REAL DATA HERE, AND WHAT IS NOT
 *
 * Almost all of this page is a description rather than a reading, and that is
 * a limitation worth naming rather than hiding. `agent_runs` has a write path
 * (`IngestService.RecordAgentRun`) and no read path, so there is no RPC that
 * would let this page show the runs an agent has actually made. Adding one is
 * proposed in the ENT-232 PR body and is deliberately not smuggled in here.
 *
 * The Watcher is the exception, and it is the exception because the data
 * already exists: `GetDashboard` carries when the last sweep ran and whether
 * there is a profile to sweep. That is read live and shown, because "the
 * Watcher last ran on Tuesday" is worth far more than any sentence describing
 * what a Watcher is.
 *
 * # AN UNKNOWN AGENT IS A 404
 *
 * The four are the same for every organisation, so an unknown name is simply
 * unknown and 404 leaks nothing. Different from the tenant surfaces, where 404
 * is doing the work of hiding whose data it is. Both end in `notFound()` and
 * only one of them is a security boundary.
 */
export default async function AgentPage({
  params,
}: {
  params: Promise<{ org: string; agent: string }>
}) {
  const { org: orgSlug, agent: agentSlug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(
        orgPath(orgSlug, `/agents/${agentSlug}`),
      )}`,
    )

  const resolved = await resolveOrg(session.accessToken, orgSlug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Agents" />

  const agent = agentBySlug(agentSlug)
  if (!agent) notFound()

  // Only for the Watcher, and only because it is the one agent whose last run
  // the console can already ask about. Fetching it for the other three would be
  // a round trip whose answer is about somebody else.
  const dashboard =
    agent.slug === 'watcher'
      ? await getDashboard(session.accessToken, resolved.membership.orgId)
      : null

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <Link
        href={orgPath(orgSlug, '/agents')}
        className="text-xs text-muted-foreground underline-offset-4 hover:underline"
      >
        All agents
      </Link>

      <div className="mt-6">
        <AgentProfile agent={agent} />
      </div>

      {dashboard ? (
        <section className="mt-8 border-t border-border/60 pt-6">
          <h2 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
            When it last ran
          </h2>
          <div className="mt-3">
            {dashboard.ok ? (
              <PipelineNote dashboard={dashboard.value} />
            ) : (
              // Never rendered as "it has not run". An unavailable read and a
              // sweep that never happened are different facts, and showing the
              // second for the first is the ENT-161 mistake in miniature.
              <p className="text-xs text-muted-foreground">
                Could not read when the Watcher last ran. Reload to try again.
              </p>
            )}
          </div>
        </section>
      ) : null}
    </div>
  )
}
