import {
  CompletenessBadge,
  DateValue,
  DocumentationBadge,
  DueLabel,
  RiskBadge,
  UrgencyBadge,
} from '@/components/records/badges'
import { NotRecorded, Value, ValueList } from '@/components/records/states'
import type { AiSystem, Dsar, ProcessingActivity } from '@/lib/records/client'

/**
 * The three registers as tables.
 *
 * Tables rather than cards, unlike the feed, and the difference is the job.
 * The feed asks for a decision on one finding at a time, so a card that carries
 * the whole argument is right. A register is a record somebody scans, compares
 * across and is eventually asked to produce for a regulator, and the question is
 * usually "which rows are missing something" rather than "what does this one
 * say". Columns answer that; a stack of cards does not.
 *
 * Each table scrolls inside its own container rather than letting the page
 * scroll sideways, because a compliance record has genuinely wide rows and the
 * console's other surfaces must not start moving horizontally to accommodate it.
 */

function Table({
  caption,
  head,
  children,
}: {
  caption: string
  head: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="overflow-x-auto rounded-xl border border-border/60">
      <table className="w-full min-w-[44rem] border-collapse text-left text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="border-b border-border/60 bg-muted/30">{head}</tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th
      scope="col"
      className="px-3 py-2 text-xs font-medium tracking-[0.04em] text-muted-foreground uppercase"
    >
      {children}
    </th>
  )
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-3 py-3 align-top">{children}</td>
}

export function RopaTable({ items }: { items: ProcessingActivity[] }) {
  return (
    <Table
      caption="Record of processing activities"
      head={
        <>
          <Th>Activity</Th>
          <Th>Legal basis</Th>
          <Th>Data</Th>
          <Th>Recipients</Th>
          <Th>Retention</Th>
          <Th>State</Th>
        </>
      }
    >
      {items.map((activity) => (
        <tr
          key={activity.processingActivityId}
          data-testid="ropa-row"
          className="border-b border-border/40 last:border-0"
        >
          <Td>
            <span className="font-medium text-foreground">{activity.name}</span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <Value>{activity.purpose}</Value>
            </span>
          </Td>
          <Td>
            <Value>{activity.legalBasis}</Value>
          </Td>
          <Td>
            <ValueList items={activity.dataCategories} />
          </Td>
          <Td>
            <ValueList items={activity.recipients} />
          </Td>
          <Td>
            <Value>{activity.retentionPeriod}</Value>
          </Td>
          <Td>
            <CompletenessBadge value={activity.completeness} />
          </Td>
        </tr>
      ))}
    </Table>
  )
}

export function AiSystemsTable({ items }: { items: AiSystem[] }) {
  return (
    <Table
      caption="AI system register"
      head={
        <>
          <Th>System</Th>
          <Th>Supplier</Th>
          <Th>Risk</Th>
          <Th>Documentation</Th>
          <Th>Last reviewed</Th>
        </>
      }
    >
      {items.map((system) => (
        <tr
          key={system.aiSystemId}
          data-testid="ai-system-row"
          className="border-b border-border/40 last:border-0"
        >
          <Td>
            <span className="font-medium text-foreground">{system.name}</span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <Value>{system.purpose}</Value>
            </span>
          </Td>
          <Td>
            {/* Empty means built in house, which is a fact rather than a gap:
                the AI Act's provider and deployer duties differ. */}
            {system.vendor && system.vendor.trim() !== '' ? (
              system.vendor
            ) : (
              <span className="text-muted-foreground/60">Built in house</span>
            )}
          </Td>
          <Td>
            <RiskBadge value={system.riskClassification} />
          </Td>
          <Td>
            <DocumentationBadge value={system.documentationStatus} />
          </Td>
          <Td>
            <DateValue value={system.lastReviewedAt} never="Never" />
          </Td>
        </tr>
      ))}
    </Table>
  )
}

export function DsarTable({ items }: { items: Dsar[] }) {
  return (
    <Table
      caption="Data-subject requests"
      head={
        <>
          <Th>Received</Th>
          <Th>Type</Th>
          <Th>Handler</Th>
          <Th>Deadline</Th>
          {/* "Response sent" rather than "Answered", which would repeat the
              word the urgency badge in the last column uses for a different
              thing: this column is a date, that one is a state. */}
          <Th>Response sent</Th>
          <Th>State</Th>
        </>
      }
    >
      {items.map((dsar) => (
        <tr
          key={dsar.dsarId}
          data-testid="dsar-row"
          className="border-b border-border/40 last:border-0"
        >
          <Td>
            <span className="font-medium text-foreground">
              <DateValue value={dsar.receivedAt} never="Not recorded" />
            </span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              {dsar.subjectName && dsar.subjectName.trim() !== '' ? (
                dsar.subjectName
              ) : (
                /* A request can arrive pseudonymously, through a form or an
                   inbox, before anyone has identified the person behind it. */
                <span className="text-muted-foreground/60">
                  Requester not identified
                </span>
              )}
            </span>
          </Td>
          <Td>
            <Value>{dsar.requestType}</Value>
          </Td>
          <Td>
            <Value>{dsar.handler}</Value>
          </Td>
          <Td>
            <span className="block">
              <DateValue value={dsar.responseDueAt} never="Not recorded" />
            </span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <DueLabel
                urgency={dsar.urgency}
                daysUntilDue={dsar.daysUntilDue}
              />
            </span>
          </Td>
          <Td>
            <DateValue value={dsar.respondedAt} never="Not yet" />
          </Td>
          <Td>
            <UrgencyBadge value={dsar.urgency} />
          </Td>
        </tr>
      ))}
    </Table>
  )
}

export { NotRecorded }
