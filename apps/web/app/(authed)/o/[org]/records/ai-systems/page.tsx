import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { RegisterNav } from '@/components/records/register-nav'
import { AiSystemsTable } from '@/components/records/registers'
import { EmptyRegister, RegisterUnavailable } from '@/components/records/states'
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
  if (resolved.status === 'unavailable') return null

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
          <AiSystemsTable items={systems} />
        )}
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
