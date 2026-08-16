import { FileQuestion, TriangleAlert } from 'lucide-react'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import type { Failure } from '@/lib/records/client'

/**
 * The shared not-there states for the three registers, on the shared `Empty`.
 *
 * Empty and unavailable are different claims and are kept apart deliberately,
 * which is why they are two components over one primitive rather than one
 * component with a flag. An empty register says we looked and there is nothing
 * on file, which is a statement about the customer's compliance record. A failed
 * read says we could not look. Rendering the first when the second happened
 * tells a customer their record is empty when it may not be, which in this
 * product is the worst available failure: not a wrong answer, a confident one.
 *
 * They carry different icons for the same reason. A person who glances at the
 * page and does not read the sentence should still not confuse the two.
 */

export function EmptyRegister({
  title,
  children,
  testId,
}: {
  title: string
  children: React.ReactNode
  testId: string
}) {
  return (
    <Empty data-testid={testId} className="border border-dashed">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FileQuestion aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{children}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

/**
 * A failed read, in words that say what it means and what to do.
 *
 * `denied` is spelled out rather than shown as a generic error because it is
 * the one a person can act on: reading the compliance record needs
 * `records:read`, and an owner genuinely can grant that. That is a different
 * situation from the one the feed was in before ENT-221, where no human token
 * carried any scope at all and telling somebody to ask an owner sent them to a
 * person who could not help.
 */
export function RegisterUnavailable({
  what,
  error,
  testId,
}: {
  what: string
  error: Failure
  testId: string
}) {
  const denied = error.kind === 'denied'

  return (
    <Empty data-testid={testId} className="border border-dashed">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <TriangleAlert aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>
          {denied ? 'Not permitted' : 'Could not be loaded'}
        </EmptyTitle>
        <EmptyDescription>
          {denied
            ? `Your session is not permitted to read the ${what}. Reading the compliance record needs the records:read scope; an owner can grant it.`
            : `The ${what} could not be loaded just now. This is usually temporary; reloading is worth a try.`}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

/**
 * A value the record does not carry.
 *
 * A visible placeholder rather than an empty cell: a blank reads as a rendering
 * bug, where "not recorded" reads as the fact it is. Article 30 is a record of
 * fact and the gaps in it are part of what a customer is looking at.
 */
export function NotRecorded() {
  return <span className="text-muted-foreground/60">Not recorded</span>
}

/** Renders a value, or NotRecorded when it is absent or blank. */
export function Value({ children }: { children?: string }) {
  if (!children || children.trim() === '') return <NotRecorded />
  return <>{children}</>
}

/** Renders a list, or NotRecorded when it is empty. */
export function ValueList({ items }: { items?: string[] }) {
  if (!items || items.length === 0) return <NotRecorded />
  return <>{items.join(', ')}</>
}
