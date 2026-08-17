'use client'

import { useActionState, useState } from 'react'

import { Field, FormFooter } from '@/components/records/activity-form'
import { ReviewConfirmation } from '@/components/records/system-form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { idle, type RecordActionState } from '@/lib/records/action-state'

/**
 * Logging a request that arrived.
 *
 * THE DATE FIELD IS THE POINT OF THIS FORM, NOT A CONVENIENCE
 *
 * `log_dsar` computes the deadline from `received_at`, so what somebody types
 * here decides when an Article 12(3) clock runs out. Before ENT-224's second
 * half the function stamped `now()` and there was no field to offer: a request
 * that came by post on the 1st and was logged on the 8th was recorded as due a
 * month from the 8th, which quietly granted the organisation the days it took
 * them to notice.
 *
 * Defaulted to today, which is the common case, and capped at today by `max`,
 * because a request cannot have arrived tomorrow. The database refuses a future
 * date too; the attribute is what stops somebody meeting that refusal after
 * filling the rest of the form in.
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

  // Today in the browser's own timezone, which is the date the person means
  // when they say "today". `toISOString` would give UTC and can be yesterday or
  // tomorrow for them.
  const today = new Date().toLocaleDateString('en-CA')

  return (
    <form action={submit} className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor="receivedAt">Received on</Label>
        <Input
          id="receivedAt"
          name="receivedAt"
          type="date"
          defaultValue={today}
          max={today}
        />
        <p className="text-xs text-muted-foreground">
          The day it arrived, not the day you are logging it. The deadline is
          counted from here, so backdating a request that sat in an inbox
          shortens the time left rather than extending it.
        </p>
      </div>

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
