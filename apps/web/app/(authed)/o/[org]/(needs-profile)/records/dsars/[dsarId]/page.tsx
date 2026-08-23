import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { UrgencyBadge, DateValue, DueLabel } from '@/components/records/badges'
import { RegisterUnavailable } from '@/components/records/states'
import { AddTrailEntry, Trail } from '@/components/records/trail'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { addTrailEntry } from '../../actions'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getDsar, listDsarTrail } from '@/lib/records/client'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 *
 * Deliberately not the subject's name, which is what the heading shows.
 * A title lands in the tab strip, the history and any bookmark, and the
 * identity of a person who asked about their data belongs in none of them.
 */
export const metadata: Metadata = {
  title: 'Data-subject request',
}

/**
 * One data-subject request, and the trail a response to it was built from
 * (ENT-226).
 *
 * WHY THIS PAGE EXISTS, WHEN THE REGISTER ALREADY LISTS THE REQUEST
 *
 * The register carries the clock: who asked, what for, when it arrived, when it
 * runs out, and whether a response went out. That last field is the one a
 * regulator reads as evidence an Article 12(3) deadline was met, and until this
 * page it was an assertion with nothing behind it. Nothing in the product could
 * show what was searched, what was found, or what was returned.
 *
 * So the trail is here rather than in a column: it is free text about a named
 * person, it is as long as the work was, and a register of every request has no
 * business carrying all of it. The list carries the COUNT, which is what makes
 * the assertion checkable at a glance, and this page carries the substance.
 *
 * NOTHING HERE EDITS OR DELETES AN ENTRY
 *
 * Deliberate, and enforced well below this page: the database refuses an UPDATE
 * with a trigger that binds even the migrator, and the application role holds no
 * DELETE grant. A correction is another entry. See `components/records/trail`.
 *
 * NOT FOUND RATHER THAN FORBIDDEN, TWICE OVER
 *
 * A slug the caller does not belong to is 404, and so is a request id in
 * somebody else's organisation: core-api answers `not_found` for both because
 * RLS makes them the same query result, and this page renders that as a 404
 * rather than translating it into a message that would confirm the request
 * exists somewhere.
 */
export default async function DsarPage({
  params,
}: {
  params: Promise<{ org: string; dsarId: string }>
}) {
  const { org: slug, dsarId } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(
        orgPath(slug, `/records/dsars/${dsarId}`),
      )}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Data-subject request" />

  const request = await getDsar(
    session.accessToken,
    resolved.membership.orgId,
    dsarId,
  )
  if (!request.ok && request.error.kind === 'missing') notFound()
  if (!request.ok) {
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-8">
        <RegisterUnavailable
          what="data-subject request"
          error={request.error}
          testId={`dsar-${request.error.kind}`}
        />
      </div>
    )
  }

  const dsar = request.value.dsar
  if (!dsar) notFound()

  // Read after the request, so a trail that fails to load leaves the request
  // itself readable. They are separate calls because they are separate answers:
  // "here is the request and its clock" is useful on its own, and a page that
  // showed neither because one of them failed would be worse than one that
  // shows the half that worked.
  const trail = await listDsarTrail(
    session.accessToken,
    resolved.membership.orgId,
    dsarId,
  )

  const subject =
    dsar.subjectName && dsar.subjectName.trim() !== ''
      ? dsar.subjectName
      : 'Requester not identified'

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <Link
        href={orgPath(slug, '/records/dsars')}
        className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground"
      >
        Back to the request log
      </Link>

      <h1 className="mt-4 text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {subject}
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        {dsar.requestType && dsar.requestType.trim() !== ''
          ? dsar.requestType
          : 'Request type not recorded'}
        {dsar.handler && dsar.handler.trim() !== ''
          ? `, handled by ${dsar.handler}`
          : null}
      </p>

      <dl className="mt-6 grid gap-4 sm:grid-cols-2">
        <div>
          <dt className="text-xs font-medium text-muted-foreground">
            Received
          </dt>
          <dd className="mt-1 text-sm text-foreground">
            <DateValue value={dsar.receivedAt} never="Not recorded" />
          </dd>
        </div>
        <div>
          <dt className="text-xs font-medium text-muted-foreground">
            Response due
          </dt>
          <dd className="mt-1 text-sm text-foreground">
            <DateValue value={dsar.responseDueAt} never="Not recorded" />
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <DueLabel
                urgency={dsar.urgency}
                daysUntilDue={dsar.daysUntilDue}
              />
            </span>
          </dd>
        </div>
        <div>
          <dt className="text-xs font-medium text-muted-foreground">
            Response sent
          </dt>
          <dd className="mt-1 text-sm text-foreground">
            <DateValue value={dsar.respondedAt} never="Not yet" />
          </dd>
        </div>
        <div>
          <dt className="text-xs font-medium text-muted-foreground">State</dt>
          <dd className="mt-1">
            <UrgencyBadge value={dsar.urgency} />
          </dd>
        </div>
      </dl>

      <h2 className="mt-8 text-sm font-medium text-foreground">
        How the response was assembled
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Each store you searched, when you searched it, and what came back. This
        is what stands behind the response date: without it, the date is the
        organisation&rsquo;s word for it. Entries cannot be edited or removed
        once recorded, so a correction is another entry.
      </p>

      <div className="mt-4">
        {!trail.ok ? (
          <RegisterUnavailable
            what="trail"
            error={trail.error}
            testId={`trail-${trail.error.kind}`}
          />
        ) : (
          <Trail entries={trail.value.entries ?? []} />
        )}
      </div>

      <div className="mt-4">
        <AddTrailEntry slug={slug} dsarId={dsarId} action={addTrailEntry} />
      </div>

      {trail.ok && trail.value.nextPageToken ? (
        // Deliberately not a link. Paging this list means dropping the earlier
        // entries off the top of a chronological record, which reads as
        // evidence going missing. Long trails are ENT-226's follow-up, and
        // showing the first page with a note is more honest than a control that
        // hides the beginning.
        <p className="mt-6 text-xs text-muted-foreground">
          This trail is longer than one page. The rest is recorded and readable
          through the API; the console does not page it yet.
        </p>
      ) : null}
    </div>
  )
}
