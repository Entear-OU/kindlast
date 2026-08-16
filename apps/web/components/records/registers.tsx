import {
  CompletenessBadge,
  DateValue,
  DocumentationBadge,
  DueLabel,
  RiskBadge,
  UrgencyBadge,
} from '@/components/records/badges'
import { NotRecorded, Value, ValueList } from '@/components/records/states'
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'
import type { AiSystem, Dsar, ProcessingActivity } from '@/lib/records/client'

/**
 * The three registers, on the shared table primitives.
 *
 * Tables rather than cards, unlike the feed, and the difference is the job.
 * The feed asks for a decision on one finding at a time, so a card that carries
 * the whole argument is right. A register is a record somebody scans, compares
 * across and is eventually asked to produce for a regulator, and the question is
 * usually "which rows are missing something" rather than "what does this one
 * say". Columns answer that; a stack of cards does not.
 *
 * TWO DELIBERATE OVERRIDES ON THE SHARED TABLE
 *
 * `Table` ships `whitespace-nowrap` on cells, which is right for the dense data
 * tables it was built for and wrong here: an Article 30 retention period is a
 * sentence in the customer's own words, and refusing to wrap it would push the
 * useful columns off screen. `align-top` for the same reason, because a wrapped
 * cell next to a one-word cell should line up at the first line.
 *
 * The caption is visually hidden rather than shown. `TableCaption` renders below
 * the table by default, and each register already has a visible heading above
 * it; keeping the caption for screen readers avoids announcing the table twice
 * to everyone else.
 */

/** Wraps rather than clipping. See the note above. */
const CELL = 'whitespace-normal align-top py-3'
const HEAD =
  'text-xs font-medium tracking-[0.04em] text-muted-foreground uppercase'

function Register({
  caption,
  head,
  children,
}: {
  caption: string
  head: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="rounded-xl border border-border/60">
      <Table className="min-w-[44rem]">
        <TableCaption className="sr-only">{caption}</TableCaption>
        <TableHeader>
          <TableRow className="bg-muted/30 hover:bg-muted/30">{head}</TableRow>
        </TableHeader>
        <TableBody>{children}</TableBody>
      </Table>
    </div>
  )
}

export function RopaTable({ items }: { items: ProcessingActivity[] }) {
  return (
    <Register
      caption="Record of processing activities"
      head={
        <>
          <TableHead className={HEAD}>Activity</TableHead>
          <TableHead className={HEAD}>Legal basis</TableHead>
          <TableHead className={HEAD}>Data</TableHead>
          <TableHead className={HEAD}>Recipients</TableHead>
          <TableHead className={HEAD}>Retention</TableHead>
          <TableHead className={HEAD}>State</TableHead>
        </>
      }
    >
      {items.map((activity) => (
        <TableRow key={activity.processingActivityId} data-testid="ropa-row">
          <TableCell className={cn(CELL, 'min-w-56')}>
            <span className="font-medium text-foreground">{activity.name}</span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <Value>{activity.purpose}</Value>
            </span>
          </TableCell>
          <TableCell className={CELL}>
            <Value>{activity.legalBasis}</Value>
          </TableCell>
          <TableCell className={CELL}>
            <ValueList items={activity.dataCategories} />
          </TableCell>
          <TableCell className={CELL}>
            <ValueList items={activity.recipients} />
          </TableCell>
          <TableCell className={CELL}>
            <Value>{activity.retentionPeriod}</Value>
          </TableCell>
          <TableCell className={CELL}>
            <CompletenessBadge value={activity.completeness} />
          </TableCell>
        </TableRow>
      ))}
    </Register>
  )
}

export function AiSystemsTable({ items }: { items: AiSystem[] }) {
  return (
    <Register
      caption="AI system register"
      head={
        <>
          <TableHead className={HEAD}>System</TableHead>
          <TableHead className={HEAD}>Supplier</TableHead>
          <TableHead className={HEAD}>Risk</TableHead>
          <TableHead className={HEAD}>Documentation</TableHead>
          <TableHead className={HEAD}>Last reviewed</TableHead>
        </>
      }
    >
      {items.map((system) => (
        <TableRow key={system.aiSystemId} data-testid="ai-system-row">
          <TableCell className={cn(CELL, 'min-w-56')}>
            <span className="font-medium text-foreground">{system.name}</span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <Value>{system.purpose}</Value>
            </span>
          </TableCell>
          <TableCell className={CELL}>
            {/* Empty means built in house, which is a fact rather than a gap:
                the AI Act's provider and deployer duties differ. */}
            {system.vendor && system.vendor.trim() !== '' ? (
              system.vendor
            ) : (
              <span className="text-muted-foreground/60">Built in house</span>
            )}
          </TableCell>
          <TableCell className={CELL}>
            <RiskBadge value={system.riskClassification} />
          </TableCell>
          <TableCell className={CELL}>
            <DocumentationBadge value={system.documentationStatus} />
          </TableCell>
          <TableCell className={CELL}>
            <DateValue value={system.lastReviewedAt} never="Never" />
          </TableCell>
        </TableRow>
      ))}
    </Register>
  )
}

export function DsarTable({ items }: { items: Dsar[] }) {
  return (
    <Register
      caption="Data-subject requests"
      head={
        <>
          <TableHead className={HEAD}>Received</TableHead>
          <TableHead className={HEAD}>Type</TableHead>
          <TableHead className={HEAD}>Handler</TableHead>
          <TableHead className={HEAD}>Deadline</TableHead>
          {/* "Response sent" rather than "Answered", which would repeat the
              word the urgency badge in the last column uses for a different
              thing: this column is a date, that one is a state. */}
          <TableHead className={HEAD}>Response sent</TableHead>
          <TableHead className={HEAD}>State</TableHead>
        </>
      }
    >
      {items.map((dsar) => (
        <TableRow key={dsar.dsarId} data-testid="dsar-row">
          <TableCell className={CELL}>
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
          </TableCell>
          <TableCell className={CELL}>
            <Value>{dsar.requestType}</Value>
          </TableCell>
          <TableCell className={CELL}>
            <Value>{dsar.handler}</Value>
          </TableCell>
          <TableCell className={CELL}>
            <span className="block">
              <DateValue value={dsar.responseDueAt} never="Not recorded" />
            </span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              <DueLabel
                urgency={dsar.urgency}
                daysUntilDue={dsar.daysUntilDue}
              />
            </span>
          </TableCell>
          <TableCell className={CELL}>
            <DateValue value={dsar.respondedAt} never="Not yet" />
          </TableCell>
          <TableCell className={CELL}>
            <UrgencyBadge value={dsar.urgency} />
          </TableCell>
        </TableRow>
      ))}
    </Register>
  )
}

export { NotRecorded }
