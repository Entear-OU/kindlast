import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { RegisterNav } from '@/components/records/register-nav'
import { AddSystem, EditableAiSystems } from '@/components/records/editable'
import { EmptyRegister, RegisterUnavailable } from '@/components/records/states'
import { addAiSystem, editAiSystem } from '../actions'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { listAiSystems } from '@/lib/records/client'

/**
 * The AI Act system register (ENT-200).
 *
 * Entries arrive from approving an Annex III or Article 26 finding, which 00009
 * classified. A system here defaulting to `unclassified` rather than `minimal`
 * is the point: Article 6 classification is a judgement with consequences, and a
 * register that defaulted to the lowest tier would tell a customer they are fine
 * about a system nobody has assessed.
 */
export default async function AiSystemsPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>
  searchParams: Promise<{ page?: string }>
}) {
  const { org: slug } = await params
  const { page } = await searchParams

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/records/ai-systems'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable') return <WorkspaceUnavailable />

  const register = await listAiSystems(
    session.accessToken,
    resolved.membership.orgId,
    { pageToken: page || undefined },
  )

  const systems = register.ok ? (register.value.aiSystems ?? []) : []

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Compliance record
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        What is on file for this organisation. Entries appear here when you
        approve a finding, and the gaps in them are part of the record rather
        than a rendering fault.
      </p>

      <RegisterNav slug={slug} active="ai-systems" />

      <h2 className="mt-6 text-sm font-medium text-foreground">
        AI system register
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        The systems you build or deploy, and how each one is classified under
        the AI Act. Unclassified means nobody has assessed it yet, which is not
        the same as low risk.
      </p>

      <div className="mt-4">
        {!register.ok ? (
          <RegisterUnavailable
            what="AI system register"
            error={register.error}
            testId={`ai-systems-${register.error.kind}`}
          />
        ) : systems.length === 0 ? (
          <EmptyRegister title="Nothing on file yet" testId="ai-systems-empty">
            Approving a finding about an AI system adds an entry here for you to
            classify.
          </EmptyRegister>
        ) : (
          <EditableAiSystems
            slug={slug}
            items={systems}
            action={editAiSystem}
          />
        )}
      </div>

      {/* Useful mostly for shadow AI: the system somebody adopted without
          telling anyone is exactly the one no finding will ever raise. */}
      <div className="mt-4">
        <AddSystem slug={slug} action={addAiSystem} />
      </div>

      {register.ok && register.value.nextPageToken ? (
        <div className="mt-6">
          <Link
            href={`${orgPath(slug, '/records/ai-systems')}?page=${encodeURIComponent(register.value.nextPageToken)}`}
            className="text-sm text-foreground underline underline-offset-4 hover:opacity-80"
          >
            More systems
          </Link>
        </div>
      ) : null}
    </div>
  )
}

/**
 * When the caller's own memberships could not be read.
 *
 * Previously `return null`, which rendered the console shell around an empty
 * page: no register, no explanation, nothing to do. Met in a browser after a
 * session's token expired, which is the commonest way to reach this branch.
 *
 * Not a redirect to sign-in, deliberately. `unavailable` is also what a
 * core-api outage produces, and redirecting on that would bounce somebody
 * between the console and the sign-in page while nothing was wrong with their
 * session. Naming both possibilities is more honest than guessing which.
 */
function WorkspaceUnavailable() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Compliance record
      </h1>
      <p
        data-testid="records-workspace-unavailable"
        className="mt-4 rounded-xl border border-dashed border-border/60 px-4 py-10 text-center text-sm text-muted-foreground"
      >
        We could not load your workspace just now. Your session may have
        expired, in which case signing in again will fix it; otherwise this is
        usually temporary.
      </p>
    </div>
  )
}
