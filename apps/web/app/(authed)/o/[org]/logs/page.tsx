import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { AuditTable } from '@/components/audit/audit-table'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { listAuditEntries } from '@/lib/audit/client'
import {
  isFiltered,
  toExportQuery,
  toFilter,
  type AuditSearchParams,
} from '@/lib/audit/filter'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * Logs: what was decided, by whom, and when (ENT-223).
 *
 * # THIS PAGE READS THE AUDIT LOG AND NOTHING ELSE
 *
 * Not traces, not model calls, not anything an observability tool holds. The
 * page says so in its own words rather than leaving it to be inferred, because
 * that firewall is what an auditor is buying: a record assembled partly from a
 * vendor's telemetry has completeness that depends on that vendor's retention
 * settings, and this one does not.
 *
 * # EVERY ROW IS A THING THAT HAPPENED
 *
 * There is no status filter and no failure column, which is a real difference
 * from the infrastructure log views this is modelled on. A refused act writes
 * nothing, because the gates are constraints and policies and a refusal aborts
 * the transaction the row would have been written in.
 *
 * # NO ROLE GATE
 *
 * Every member sees this, viewers included, unlike billing. "Who approved this
 * finding" is the shared record of the work these people did together, and
 * making somebody ask permission to check it is the opposite of what a
 * compliance record is for.
 *
 * # THE FILTER LIVES IN THE URL
 *
 * So a filtered view can be sent to a colleague, bookmarked for next quarter,
 * and survive the back button. "Here is the link to the decisions I am asking
 * about" is the interaction this page exists for. It also means filtering needs
 * no client-side JavaScript: the form GETs and the server re-renders.
 */
export default async function LogsPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>
  searchParams: Promise<AuditSearchParams>
}) {
  const { org: slug } = await params
  const query = await searchParams

  const session = await currentSession()
  if (!session)
    redirect(`/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/logs'))}`)

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Logs" />

  const { membership } = resolved

  const result = await listAuditEntries(session.accessToken, membership.orgId, {
    filter: toFilter(query),
    pageToken: query.page || undefined,
  })

  const entries = result.ok ? (result.value.entries ?? []) : []
  const actionTypes = result.ok ? (result.value.availableActionTypes ?? []) : []
  const nextPage = result.ok ? result.value.nextPageToken : undefined
  const filtered = isFiltered(query)
  const exportQuery = toExportQuery(query)

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Logs
      </h1>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        Every decision recorded for this organisation, newest first. Each row is
        written in the same transaction as the act it describes, and the log
        cannot be edited or deleted from anywhere in this product.
      </p>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        This is the audit log and only the audit log. It is not assembled from
        traces or monitoring data, so what it contains does not depend on any
        third party&rsquo;s retention settings.
      </p>

      <form
        method="GET"
        className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-4"
      >
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium text-foreground">From</span>
          <input
            type="date"
            name="since"
            defaultValue={query.since ?? ''}
            className="rounded-md border border-border/60 bg-background px-3 py-2"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium text-foreground">To</span>
          <input
            type="date"
            name="until"
            defaultValue={query.until ?? ''}
            className="rounded-md border border-border/60 bg-background px-3 py-2"
          />
          <span className="text-xs text-muted-foreground">
            Whole days, in UTC, so a shared link shows the same rows to
            everyone.
          </span>
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium text-foreground">Action</span>
          {/* Only values that exist in this organisation's log. A list of every
              action type the schema could hold sends a person hunting for rows
              that were never written. */}
          <select
            name="action"
            defaultValue={
              Array.isArray(query.action)
                ? query.action[0]
                : (query.action ?? '')
            }
            className="rounded-md border border-border/60 bg-background px-3 py-2"
          >
            <option value="">Any action</option>
            {actionTypes.map((actionType) => (
              <option key={actionType} value={actionType}>
                {actionType}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium text-foreground">Search</span>
          <input
            type="search"
            name="q"
            defaultValue={query.q ?? ''}
            placeholder="Action, register or person"
            className="rounded-md border border-border/60 bg-background px-3 py-2"
          />
          {/* Stated rather than discovered. Somebody searching for a data
              subject's name and getting nothing should know that is the design
              and not a broken filter. */}
          <span className="text-xs text-muted-foreground">
            Searches the action, the register and the person. Not the recorded
            values, which hold personal data.
          </span>
        </label>

        <div className="flex items-end gap-3 sm:col-span-2 lg:col-span-4">
          <button
            type="submit"
            className="rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background"
          >
            Apply filters
          </button>
          {filtered ? (
            <Link
              href={orgPath(slug, '/logs')}
              className="text-sm text-muted-foreground underline underline-offset-4"
            >
              Clear
            </Link>
          ) : null}
          <a
            href={`${orgPath(slug, '/logs/export')}${exportQuery ? `?${exportQuery}` : ''}`}
            className="ml-auto rounded-md border border-border/60 px-4 py-2 text-sm font-medium text-foreground"
          >
            Export CSV
          </a>
        </div>
      </form>

      {/* The cap is stated where the button is, not discovered in the file.
          A truncated CSV is a valid CSV that simply stops, so an auditor who
          is not told about the cap can attach an incomplete record to a report
          without knowing. The download names itself `-partial` when it happens,
          which is the version that survives being emailed to somebody else. */}
      <p className="mt-3 text-xs text-muted-foreground">
        An export covers the filtered set, up to 50,000 rows. If it reaches that
        limit the file is named <code>-partial</code>; narrow the dates and
        export again.
      </p>

      <section className="mt-8">
        {!result.ok ? (
          // Not an empty table. Rendering "no decisions yet" here would tell an
          // organisation their compliance record is empty because a request
          // failed, which is the most alarming possible way to report an
          // outage.
          <p className="text-sm text-muted-foreground">
            Could not load the log. Reload to try again.
          </p>
        ) : entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {filtered
              ? 'No decisions match these filters.'
              : 'Nothing has been decided yet. Approving, rejecting or snoozing a finding records a row here.'}
          </p>
        ) : (
          <>
            <AuditTable entries={entries} />
            {nextPage ? (
              <div className="mt-4">
                <Link
                  href={`${orgPath(slug, '/logs')}?${new URLSearchParams({
                    ...(exportQuery
                      ? Object.fromEntries(new URLSearchParams(exportQuery))
                      : {}),
                    page: nextPage,
                  }).toString()}`}
                  className="text-sm text-foreground underline underline-offset-4"
                >
                  Older decisions
                </Link>
              </div>
            ) : null}
          </>
        )}
      </section>
    </div>
  )
}
