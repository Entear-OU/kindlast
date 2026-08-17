import type { AuditFilter } from '@/lib/audit/client'

/**
 * The filter, as it travels in a URL (ENT-223).
 *
 * # WHY THE FILTER IS IN THE QUERY STRING AND NOT IN COMPONENT STATE
 *
 * Because an auditor's actual workflow ends in somebody else looking at the
 * same thing. A filtered view that lives only in a React state hook cannot be
 * sent to a colleague, cannot be bookmarked for the next quarter's review, and
 * is lost by the back button. "Here is the link to the decisions I am asking
 * about" is the whole interaction.
 *
 * It also means the page needs no client-side JavaScript to filter: the form
 * GETs, the server reads the params, and the table re-renders. The export route
 * reads the same params, which is what makes "export what I am looking at" true
 * by construction rather than by two implementations agreeing.
 */
export interface AuditSearchParams {
  since?: string
  until?: string
  action?: string | string[]
  actor?: string | string[]
  q?: string
  page?: string
}

function list(value: string | string[] | undefined): string[] {
  if (!value) return []
  return (Array.isArray(value) ? value : [value]).filter(Boolean)
}

/**
 * A date input gives `2026-08-17`. The RPC wants an instant.
 *
 * Both ends are interpreted in UTC rather than in the viewer's timezone, and
 * that is a deliberate, stated choice rather than an oversight. The alternative
 * is that two colleagues in different offices, sending each other the same
 * link, see different sets of rows, and neither can tell why. An audit range
 * that means something different depending on who opens it is not a range.
 *
 * `until` is turned into the START of the day after, because a person choosing
 * "17 August" means the whole of the 17th. The RPC's upper bound is exclusive,
 * so this lands exactly on the last moment of the chosen day without including
 * anything from the 18th.
 */
function startOfDayUTC(value: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined
  const at = new Date(`${value}T00:00:00.000Z`)
  return Number.isNaN(at.getTime()) ? undefined : at.toISOString()
}

function endOfDayExclusiveUTC(value: string): string | undefined {
  const start = startOfDayUTC(value)
  if (!start) return undefined
  return new Date(new Date(start).getTime() + 24 * 60 * 60 * 1000).toISOString()
}

export function toFilter(params: AuditSearchParams): AuditFilter {
  const filter: AuditFilter = {}

  if (params.since) {
    const since = startOfDayUTC(params.since)
    if (since) filter.since = since
  }
  if (params.until) {
    const until = endOfDayExclusiveUTC(params.until)
    if (until) filter.until = until
  }

  const actionTypes = list(params.action)
  if (actionTypes.length > 0) filter.actionTypes = actionTypes

  const actorUserIds = list(params.actor)
  if (actorUserIds.length > 0) filter.actorUserIds = actorUserIds

  const query = params.q?.trim()
  if (query) filter.query = query

  return filter
}

/**
 * Rebuilds the query string for the export link, dropping `page`.
 *
 * Dropping it is the point: an export is the whole matching set, and carrying a
 * page cursor into it would be a way to ask for "everything, starting from page
 * three", which is not a question anybody means to ask.
 */
export function toExportQuery(params: AuditSearchParams): string {
  const query = new URLSearchParams()
  if (params.since) query.set('since', params.since)
  if (params.until) query.set('until', params.until)
  for (const action of list(params.action)) query.append('action', action)
  for (const actor of list(params.actor)) query.append('actor', actor)
  if (params.q?.trim()) query.set('q', params.q.trim())
  return query.toString()
}

/** Whether anything is filtered, so the page can say "no rows match" rather
 *  than "nothing has happened yet". Those are different sentences and only one
 *  of them is alarming. */
export function isFiltered(params: AuditSearchParams): boolean {
  return Boolean(
    params.since ||
    params.until ||
    list(params.action).length > 0 ||
    list(params.actor).length > 0 ||
    params.q?.trim(),
  )
}
