/**
 * The audit log, from web's side (ENT-223).
 *
 * Read only. There is no call here that changes anything, because there is no
 * such RPC: the table is append-only by trigger and `kindlast_app` holds no
 * update or delete grant on it. A record a customer can edit is not an audit
 * log.
 *
 * # AND IT READS ONE SOURCE
 *
 * Everything on this surface comes from `audit_log`. Not traces, not model
 * calls, not anything an observability tool holds (§7.2). The page says so out
 * loud, because "traces are not the audit log" is the property an auditor is
 * buying and not something they should have to infer.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/** Who acted. Not assumed to be a person: §26 has agent runs producing acts. */
export interface Actor {
  userId?: string
  /** Both empty for an actor who has left, and for a non-human actor. The row
   *  is still returned: a log that dropped entries when somebody was
   *  offboarded would be defeatable by offboarding somebody. */
  displayName?: string
  email?: string
  /** The role held AT THE TIME, snapshotted into the row. Never re-resolved:
   *  the log has to stay true about the past. */
  actorRole?: string
  kind?: 'ACTOR_KIND_UNSPECIFIED' | 'ACTOR_KIND_HUMAN' | 'ACTOR_KIND_SERVICE'
}

export interface AuditEntry {
  id?: string
  occurredAt?: string
  actionType?: string
  actor?: Actor
  findingId?: string
  targetTable?: string
  targetId?: string
  beforeJson?: string
  afterJson?: string
  /** The agent run behind this act (§26). Always empty today. */
  agentRunId?: string
}

export interface AuditFilter {
  since?: string
  /** Exclusive, so consecutive ranges tile without returning a boundary row
   *  twice. */
  until?: string
  actionTypes?: string[]
  actorUserIds?: string[]
  query?: string
}

export interface AuditPage {
  entries?: AuditEntry[]
  nextPageToken?: string
  /** Every action type present in this organisation's log, whatever the
   *  filter. Populates the filter control with values that would actually
   *  return something. */
  availableActionTypes?: string[]
}

export interface AuditExport {
  content?: { data?: string; filename?: string; contentType?: string }
  download?: { url?: string; expiresAt?: string }
  rowCount?: number
  /** The export hit its row cap and the file is not the whole matching set.
   *  Must be shown: a truncated CSV is a valid CSV that simply stops, and an
   *  auditor who attaches it to a report has attached an incomplete record. */
  truncated?: boolean
}

export function listAuditEntries(
  accessToken: string,
  orgId: string,
  body: { filter?: AuditFilter; pageToken?: string; pageSize?: number },
) {
  return call<AuditPage>('kindlast.core.v1.AuditService/ListAuditEntries', {
    accessToken,
    orgId,
    body,
  })
}

export function exportAuditEntries(
  accessToken: string,
  orgId: string,
  filter: AuditFilter,
) {
  return call<AuditExport>('kindlast.core.v1.AuditService/ExportAuditEntries', {
    accessToken,
    orgId,
    body: { filter, format: 'EXPORT_FORMAT_CSV' },
  })
}
