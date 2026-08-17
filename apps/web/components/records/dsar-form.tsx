'use client'

import { useActionState, useState } from 'react'

import { Field, FormFooter } from '@/components/records/activity-form'
import { ReviewConfirmation } from '@/components/records/system-form'
import { Button } from '@/components/ui/button'
import { idle, type RecordActionState } from '@/lib/records/action-state'

/**
 * Logging a request that arrived.
 *
 * No date field, and that is worth stopping on. `log_dsar` stamps `received_at`
 * as now and computes the deadline from it, so this form can only log a request
 * on the day it arrives. A request that came by post last week is currently
 * logged with this week's clock, which grants the organisation the days it took
 * them to notice: exactly the error ENT-224 fixed on the executor path.
 *
 * Not fixed here because the fix is in the database function's signature rather
 * than in this form, and adding a date input that the API then ignores would be
 * worse than the gap. Tracked as the next piece of ENT-224.
 */
export function DsarForm({
  slug,
  action,
  onDone,
}: {
  slug: string
  action: (
    slug: string,
    previous: RecordActionState,
    form: FormData,
  ) => Promise<RecordActionState>
  onDone?: () => void
}) {
  const [state, submit, pending] = useActionState(
    async (previous: RecordActionState, form: FormData) => {
      const next = await action(slug, previous, form)
      if (next.status === 'ok') onDone?.()
      return next
    },
    idle,
  )

  return (
    <form action={submit} className="space-y-4">
      <Field
        label="Requester"
        name="subjectName"
        placeholder="Leave empty if you do not know yet"
        hint="A request can arrive through a form or an inbox before anyone has identified the person behind it."
      />
      <Field
        label="What was asked for"
        name="requestType"
        placeholder="access, erasure, rectification, portability…"
        hint="In the words they used. Articles 15 to 22."
        required
      />
      <Field
        label="Handler"
        name="handler"
        placeholder="Privacy team, or an external DPO"
      />

      <FormFooter
        state={state}
        pending={pending}
        submitLabel="Log request"
        onCancel={onDone}
      />
    </form>
  )
}

/**
 * Recording that a response went out, which stops the statutory clock.
 *
 * Gated behind a confirmation for the same class of reason a reclassification
 * is: this is the assertion that the organisation met an Article 12(3)
 * deadline, and a regulator reading the log later is reading this field.
 *
 * Two steps rather than a checkbox beside a button, because this one sits in a
 * table row next to other rows' buttons and a mis-click would otherwise be one
 * click from a claim nobody made.
 */
export function RespondButton({
  slug,
  dsarId,
  action,
}: {
  slug: string
  dsarId: string
  action: (
    slug: string,
    previous: RecordActionState,
    form: FormData,
  ) => Promise<RecordActionState>
}) {
  const [confirming, setConfirming] = useState(false)
  const [state, submit, pending] = useActionState(
    async (previous: RecordActionState, form: FormData) => {
      const next = await action(slug, previous, form)
      if (next.status === 'ok') setConfirming(false)
      return next
    },
    idle,
  )

  if (!confirming) {
    return (
      <Button
        type="button"
        variant="outline"
        onClick={() => setConfirming(true)}
      >
        Mark responded
      </Button>
    )
  }

  return (
    <form action={submit} className="space-y-2">
      <input type="hidden" name="dsarId" value={dsarId} />
      <ReviewConfirmation description="This records that a response was sent, which is what a regulator reads as evidence the deadline was met." />
      <FormFooter
        state={state}
        pending={pending}
        submitLabel="Confirm response sent"
        onCancel={() => setConfirming(false)}
      />
    </form>
  )
}
