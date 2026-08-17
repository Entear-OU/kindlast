import { NextResponse, type NextRequest } from 'next/server'

import { exportAuditEntries } from '@/lib/audit/client'
import { toFilter, type AuditSearchParams } from '@/lib/audit/filter'
import { resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * The audit export, as a file the browser downloads (ENT-223).
 *
 * # WHY A ROUTE HANDLER RATHER THAN A SERVER ACTION
 *
 * Because the result is a file, and a download has to survive being a plain
 * link. A server action returning bytes needs client-side JavaScript to turn
 * them into a saved file, which means the export stops working for anyone whose
 * browser did not run it, and means the artefact an auditor's whole workflow
 * ends in depends on a hydration step. A route handler with a
 * `Content-Disposition` is the boring version that always works.
 *
 * # IT READS THE SAME QUERY STRING THE PAGE DOES
 *
 * `toFilter` is shared with the page, so "export what I am looking at" is true
 * by construction rather than because two implementations agree. An export that
 * quietly differs from the table above it is a bug an auditor discovers after
 * they have filed the report.
 *
 * # THE FILENAME CARRIES THE TRUNCATION
 *
 * When core-api reports the row cap was hit, the file is named `-partial`. That
 * is deliberate and it is the only signal that survives the trip: a truncated
 * CSV is a valid CSV that simply stops, and the file is what gets attached to a
 * report and emailed onwards, long after any banner in the console is gone. A
 * name is the one piece of context that travels with it.
 */
export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ org: string }> },
) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session) {
    // 401 rather than a redirect. This is a file endpoint, and redirecting it
    // into a sign-in page would save an HTML document called `audit.csv`.
    return new NextResponse('Not signed in', { status: 401 })
  }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') {
    // 404, matching the rest of the console: a slug the caller does not belong
    // to is not found rather than forbidden, so this endpoint cannot be used to
    // discover which organisations exist.
    return new NextResponse('Not found', { status: 404 })
  }
  if (resolved.status === 'unavailable') {
    return new NextResponse('The workspace is unavailable', { status: 503 })
  }

  const search = request.nextUrl.searchParams
  const query: AuditSearchParams = {
    since: search.get('since') ?? undefined,
    until: search.get('until') ?? undefined,
    action: search.getAll('action'),
    actor: search.getAll('actor'),
    q: search.get('q') ?? undefined,
  }

  const result = await exportAuditEntries(
    session.accessToken,
    resolved.membership.orgId,
    toFilter(query),
  )

  if (!result.ok) {
    const status = result.error.kind === 'refused' ? 400 : 502
    return new NextResponse(result.error.message || 'The export failed', {
      status,
    })
  }

  const content = result.value.content
  if (!content?.data) {
    // A response with no content is a fault, not an empty export: an export of
    // nothing still carries a header row. Refusing here rather than serving a
    // zero-byte file, because a zero-byte file looks like an answer.
    return new NextResponse('The export produced no file', { status: 502 })
  }

  // Connect encodes proto `bytes` as base64 over JSON.
  const bytes = Buffer.from(content.data, 'base64')

  const name = result.value.truncated
    ? (content.filename ?? 'kindlast-audit.csv').replace(
        /\.csv$/,
        '-partial.csv',
      )
    : (content.filename ?? 'kindlast-audit.csv')

  return new NextResponse(bytes, {
    status: 200,
    headers: {
      'Content-Type': content.contentType || 'text/csv; charset=utf-8',
      // `attachment` rather than `inline`, so a browser saves it instead of
      // rendering it as text in a tab the person then has to work out how to
      // save.
      'Content-Disposition': `attachment; filename="${name}"`,
      // Never cached. This is a per-organisation record behind a session, and a
      // shared proxy holding one tenant's audit log is the worst cache bug
      // available in this product.
      'Cache-Control': 'no-store, private',
      // Reported as a header too, so a script consuming this endpoint can see
      // the truncation without parsing the filename.
      'X-Kindlast-Row-Count': String(result.value.rowCount ?? 0),
      'X-Kindlast-Truncated': result.value.truncated ? 'true' : 'false',
    },
  })
}
