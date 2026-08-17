import type { AuditEntry } from '@/lib/audit/client'

/**
 * The decisions, as a table (ENT-223).
 *
 * # WHAT AN ACTION TYPE IS CALLED
 *
 * `approve_finding` is what the database stores and what the export carries.
 * The table shows a sentence instead, because the reader is an auditor rather
 * than a schema author. The raw value stays in the export, so the file and the
 * page can always be reconciled.
 *
 * An action type this map does not know renders as itself rather than as
 * "Unknown". The vocabulary grows as obligations are added, and an audit log is
 * the one register where a value the client has not been taught must still show
 * what it says.
 */
const ACTION_LABELS: Record<string, string> = {
  approve_finding: 'Approved a finding',
  reject_finding: 'Rejected a finding',
  snooze_finding: 'Snoozed a finding',
  create_ropa: 'Created an Article 30 entry',
  create_ai_system: 'Created an AI system entry',
  create_dsar: 'Created a data subject request',
  log_dsar: 'Logged a data subject request',
  mark_dsar_responded: 'Recorded a response to a request',
  update_processing_activity: 'Edited an Article 30 entry',
  update_ai_system: 'Edited an AI system entry',
  create_processing_activity: 'Added an Article 30 entry',
  create_ai_system_manual: 'Added an AI system entry',
}

export function actionLabel(actionType: string): string {
  return ACTION_LABELS[actionType] ?? actionType
}

/**
 * Who acted, in the words the reader needs.
 *
 * The role is the one recorded AT THE TIME, never resolved now. A page that
 * looked it up would say "approved by Ada, an owner" about an act Ada performed
 * as a member, and would change what it said about a past act every time
 * somebody's role changed.
 */
function actorLabel(entry: AuditEntry): string {
  const actor = entry.actor
  if (!actor) return 'Unknown'
  if (actor.kind === 'ACTOR_KIND_SERVICE')
    return actor.displayName || 'Automated'
  // An actor who has left has no identity record this caller can read. The row
  // is still here, which is the point: a log that dropped entries when somebody
  // was offboarded would be defeatable by offboarding somebody.
  return actor.displayName || actor.email || 'A former member'
}

export function AuditTable({ entries }: { entries: AuditEntry[] }) {
  return (
    // Scrolls inside its own container rather than pushing the page sideways.
    <div className="overflow-x-auto rounded-xl border border-border/60">
      <table className="w-full min-w-[48rem] border-collapse text-sm">
        <caption className="sr-only">
          Decisions recorded for this organisation, newest first
        </caption>
        <thead>
          <tr className="border-b border-border/60 bg-muted/30 text-left">
            <th scope="col" className="px-4 py-3 font-medium">
              When
            </th>
            <th scope="col" className="px-4 py-3 font-medium">
              What happened
            </th>
            <th scope="col" className="px-4 py-3 font-medium">
              Who
            </th>
            <th scope="col" className="px-4 py-3 font-medium">
              Role at the time
            </th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr
              key={entry.id}
              className="border-b border-border/40 last:border-0"
            >
              <td className="px-4 py-3 align-top whitespace-nowrap text-muted-foreground">
                {entry.occurredAt ? (
                  // The machine-readable instant stays in the markup even
                  // though the text is friendlier, so the page and the export
                  // can be reconciled without guessing a timezone.
                  <time dateTime={entry.occurredAt}>
                    {new Date(entry.occurredAt)
                      .toISOString()
                      .replace('T', ' ')
                      .slice(0, 19)}
                    {' UTC'}
                  </time>
                ) : null}
              </td>
              <td className="px-4 py-3 align-top text-foreground">
                {actionLabel(entry.actionType ?? '')}
                {entry.targetTable ? (
                  <span className="block text-xs text-muted-foreground">
                    {entry.targetTable}
                  </span>
                ) : null}
              </td>
              <td className="px-4 py-3 align-top text-foreground">
                {actorLabel(entry)}
              </td>
              <td className="px-4 py-3 align-top text-muted-foreground">
                {/* `actor_role` is nullable, and rows written before 00002
                    added the column have none. "Not recorded" rather than a
                    blank cell or a dash: an empty cell in an audit table reads
                    as a rendering fault, and this one is a fact about the row. */}
                {entry.actor?.actorRole || (
                  <span className="italic">Not recorded</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
