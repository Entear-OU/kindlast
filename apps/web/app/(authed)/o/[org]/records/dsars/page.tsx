import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { RegisterNav } from '@/components/records/register-nav'
import { DsarTable } from '@/components/records/registers'
import { EmptyRegister, RegisterUnavailable } from '@/components/records/states'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { listDsars } from '@/lib/records/client'

const FILTERS = [
  { label: 'All', value: '' },
  { label: 'Open', value: 'open' },
  { label: 'In progress', value: 'in_progress' },
  { label: 'Responded', value: 'responded' },
  { label: 'Closed', value: 'closed' },
] as const

/**
 * The data-subject request log (ENT-200).
 *
 * Soonest deadline first, because the only question anyone asks of this list is
 * which request runs out next. That ordering comes from the server, not from a
 * sort here.
 *
 * NOTHING IN THIS REGISTER ARRIVES FROM AN APPROVAL
 *
 * Unlike the other two, and deliberately. 00009 classified three obligations and
 * mapped none of them to `create_dsar`, because a data-subject request arrives
 * from a person: an obligation that manufactured one would be inventing the
 * requester. So every row here was logged by a human, and the empty state says
 * that rather than promising entries that will never appear on their own.
 */
export default async function DsarsPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>
  searchParams: Promise<{ status?: string; page?: string }>
}) {
  const { org: slug } = await params
  const { status, page } = await searchParams

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/records/dsars'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable') return null

  const register = await listDsars(
    session.accessToken,
    resolved.membership.orgId,
    { status: status || undefined, pageToken: page || undefined },
  )

  const dsars = register.ok ? (register.value.dsars ?? []) : []

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

      <RegisterNav slug={slug} active="dsars" />

      <h2 className="mt-6 text-sm font-medium text-foreground">
        Data-subject requests
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Soonest deadline first. The clock runs from the day a request was
        received, not the day it was logged, because that is what Article 12(3)
        counts.
      </p>

      <nav aria-label="Filter requests" className="mt-4 flex flex-wrap gap-2">
        {FILTERS.map(({ label, value }) => {
          const current = (status ?? '') === value
          const href = value
            ? `${orgPath(slug, '/records/dsars')}?status=${value}`
            : orgPath(slug, '/records/dsars')

          return (
            <Link
              key={label}
              href={href}
              aria-current={current ? 'page' : undefined}
              className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                current
                  ? 'border-primary/40 bg-primary/10 text-primary'
                  : 'border-border/60 text-muted-foreground hover:border-border hover:text-foreground'
              }`}
            >
              {label}
            </Link>
          )
        })}
      </nav>

      <div className="mt-4">
        {!register.ok ? (
          <RegisterUnavailable
            what="data-subject request log"
            error={register.error}
            testId={`dsars-${register.error.kind}`}
          />
        ) : dsars.length === 0 ? (
          <EmptyRegister
            title={status ? 'None with that status' : 'Nothing logged yet'}
            testId="dsars-empty"
          >
            {status
              ? 'No requests are in that state right now.'
              : 'These are added by hand: a request comes from a person, so nothing creates one for you.'}
          </EmptyRegister>
        ) : (
          <DsarTable items={dsars} />
        )}
      </div>

      {register.ok && register.value.nextPageToken ? (
        <div className="mt-6">
          <Link
            href={`${orgPath(slug, '/records/dsars')}?${new URLSearchParams({
              ...(status ? { status } : {}),
              page: register.value.nextPageToken,
            })}`}
            className="text-sm text-foreground underline underline-offset-4 hover:opacity-80"
          >
            Later deadlines
          </Link>
        </div>
      ) : null}
    </div>
  )
}
