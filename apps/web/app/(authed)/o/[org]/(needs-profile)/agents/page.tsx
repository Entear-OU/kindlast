import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { AgentStatusDot } from '@/components/agents/agent-status'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { AGENTS, STATUS_LABEL } from '@/lib/agents/catalog'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Agents',
}

/**
 * The four agents (ENT-232).
 *
 * The rail is the index on a wide screen, so this page exists for the two
 * places the rail is not: a phone, where the rail sits below the fold, and the
 * back link from an agent's own page.
 *
 * It repeats what the rail says rather than adding to it, on purpose. The
 * catalogue is the one declaration; a summary page that said something the rail
 * did not would be a second description of the same four things, which is the
 * shape of drift this issue was about.
 */
export default async function AgentsPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: orgSlug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(orgSlug, '/agents'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, orgSlug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Agents" />

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Your agents
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Four of them, working in order. Each one says when it runs and what it
        can change, so you can tell what has looked at your compliance and what
        has not.
      </p>

      <ul className="mt-6 space-y-3">
        {AGENTS.map((agent) => (
          <li key={agent.slug}>
            <Link
              href={orgPath(orgSlug, `/agents/${agent.slug}`)}
              className="block rounded-xl border border-border/60 bg-background p-5 transition-colors hover:border-border"
            >
              <p className="flex items-center gap-2 text-xs text-muted-foreground">
                <AgentStatusDot status={agent.status} />
                {STATUS_LABEL[agent.status]}
              </p>
              <p className="mt-1.5 text-[15px] font-medium text-foreground">
                {agent.name}
              </p>
              <p className="mt-1 text-sm text-muted-foreground">{agent.does}</p>
            </Link>
          </li>
        ))}
      </ul>
    </main>
  )
}
