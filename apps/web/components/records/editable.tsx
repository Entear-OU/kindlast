'use client'

import { useState } from 'react'

import { ActivityForm } from '@/components/records/activity-form'
import { DsarTable, RopaTable } from '@/components/records/registers'
import { AiSystemsTable } from '@/components/records/registers'
import { SystemForm } from '@/components/records/system-form'
import { Button } from '@/components/ui/button'
import type { RecordActionState } from '@/lib/records/action-state'
import type { AiSystem, Dsar, ProcessingActivity } from '@/lib/records/client'

/**
 * The registers, with a way to edit a row.
 *
 * ONE FORM AT A TIME, IN PLACE OF THE TABLE, NOT INSIDE IT
 *
 * An Article 30 entry has six fields and an AI system has five, and a row that
 * expands into all of them inside a table either scrolls sideways or collapses
 * the columns of every other row. Replacing the table with the form for as long
 * as somebody is editing keeps both readable, and it makes the state obvious:
 * you are editing this one thing, and here is the way back.
 *
 * The alternative considered was a dialog. Rejected because a compliance record
 * is something people cross-reference while they fill it in, and a modal is
 * exactly the control that stops you looking at the row above.
 *
 * WHY THESE ARE CLIENT COMPONENTS AND THE PAGES ARE NOT
 *
 * Only the "which row is open" state lives here. The pages stay server
 * components that read through the session, so no token or org id crosses into
 * the browser: the form posts a record id and the server action re-resolves the
 * organisation from the slug in the URL.
 */

type Action = (
  slug: string,
  previous: RecordActionState,
  form: FormData,
) => Promise<RecordActionState>

function EditingFrame({
  title,
  onBack,
  children,
}: {
  title: string
  onBack: () => void
  children: React.ReactNode
}) {
  return (
    <section className="rounded-xl border border-border/60 p-4">
      <div className="flex items-center justify-between gap-4">
        <h3 className="text-sm font-medium text-foreground">{title}</h3>
        <Button type="button" variant="ghost" onClick={onBack}>
          Back to the register
        </Button>
      </div>
      <div className="mt-4">{children}</div>
    </section>
  )
}

export function EditableRopa({
  slug,
  items,
  action,
}: {
  slug: string
  items: ProcessingActivity[]
  action: Action
}) {
  const [editing, setEditing] = useState<string | null>(null)
  const activity = items.find((item) => item.processingActivityId === editing)

  if (activity) {
    return (
      <EditingFrame title={activity.name} onBack={() => setEditing(null)}>
        <ActivityForm
          slug={slug}
          action={action}
          activity={activity}
          onDone={() => setEditing(null)}
        />
      </EditingFrame>
    )
  }

  return <RopaTable items={items} onEdit={setEditing} />
}

export function EditableAiSystems({
  slug,
  items,
  action,
}: {
  slug: string
  items: AiSystem[]
  action: Action
}) {
  const [editing, setEditing] = useState<string | null>(null)
  const system = items.find((item) => item.aiSystemId === editing)

  if (system) {
    return (
      <EditingFrame title={system.name} onBack={() => setEditing(null)}>
        <SystemForm
          slug={slug}
          action={action}
          system={system}
          onDone={() => setEditing(null)}
        />
      </EditingFrame>
    )
  }

  return <AiSystemsTable items={items} onEdit={setEditing} />
}

/**
 * The DSAR log, which has no edit form.
 *
 * A logged request is a record of something that happened, so the fields are
 * not somebody's draft to revise: what changes about it is whether it has been
 * answered, and that is one gated transition rather than an edit. So this
 * carries a respond control per row and nothing else.
 */
export function RespondableDsars({
  slug,
  items,
  action,
}: {
  slug: string
  items: Dsar[]
  action: Action
}) {
  return <DsarTable items={items} slug={slug} respondAction={action} />
}
