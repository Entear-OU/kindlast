import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { RegisterNav } from '@/components/records/register-nav'
import { RopaTable } from '@/components/records/registers'
import { EmptyRegister, RegisterUnavailable } from '@/components/records/states'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { listProcessingActivities } from '@/lib/records/client'

/**
 * The Article 30 record, and the default view of the compliance record
 * (ENT-200).
 *
 * WHY THIS PAGE EXISTS BEFORE ANY WAY TO EDIT IT
 *
 * Approving a finding already creates a record. `approve_finding` dispatches on
 * `findings.action_type` and writes the row, so before this page shipped the act
 * path wrote into a space the customer could not look at. Reading comes first
 * because that is the half that was missing and the half an auditor needs.
 *
 * Editing arrives next. The write scopes and the database functions behind them
 * already exist, so the gap is the surface rather than the mechanism.
 */
export default async function RecordsPage({
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
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/records'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable') return null

  const register = await listProcessingActivities(
    session.accessToken,
    resolved.membership.orgId,
    { pageToken: page || undefined },
  )

  const activities = register.ok
    ? (register.value.processingActivities ?? [])
    : []
  const quota = register.ok ? register.value.manualQuota : undefined

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

      <RegisterNav slug={slug} active="ropa" />

      <h2 className="mt-6 text-sm font-medium text-foreground">
        Record of processing activities
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Article 30 requires a written record of what you do with personal data,
        why, and for how long.
      </p>

      <div className="mt-4">
        {!register.ok ? (
          <RegisterUnavailable
            what="record of processing activities"
            error={register.error}
            testId={`records-${register.error.kind}`}
          />
        ) : activities.length === 0 ? (
          <EmptyRegister testId="ropa-empty">
            Nothing on file yet. Approving a finding about an activity adds an
            entry here for you to complete.
          </EmptyRegister>
        ) : (
          <RopaTable items={activities} />
        )}
      </div>

      {quota && quota.limit ? (
        <p
          data-testid="ropa-quota"
          className="mt-3 text-xs text-muted-foreground"
        >
          {/* Only entries somebody added by hand count. A record created from an
              approved finding is part of the compliance record and is never
              withheld behind a plan. */}
          Free plan: {quota.used ?? 0} of {quota.limit} manually added
          activities used. Entries created from approved findings do not count.
        </p>
      ) : null}

      <NextPage
        slug={slug}
        token={register.ok ? register.value.nextPageToken : undefined}
      />
    </div>
  )
}

/**
 * Forward-only, because the cursor is opaque and encodes one position. A
 * "previous" link would need a stack of tokens held somewhere, and the browser's
 * back button already does that job correctly.
 */
function NextPage({ slug, token }: { slug: string; token?: string }) {
  if (!token) return null
  return (
    <div className="mt-6">
      <Link
        href={`${orgPath(slug, '/records')}?page=${encodeURIComponent(token)}`}
        className="text-sm text-foreground underline underline-offset-4 hover:opacity-80"
      >
        More activities
      </Link>
    </div>
  )
}
