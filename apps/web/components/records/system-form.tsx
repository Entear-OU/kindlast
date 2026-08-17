'use client'

import { useActionState, useState } from 'react'

import { Field, FormFooter } from '@/components/records/activity-form'
import { Label } from '@/components/ui/label'
import { idle, type RecordActionState } from '@/lib/records/action-state'
import type { AiSystem } from '@/lib/records/client'

const RISK = [
  { value: 'unclassified', label: 'Unclassified, nobody has assessed it' },
  { value: 'minimal', label: 'Minimal' },
  { value: 'limited', label: 'Limited' },
  { value: 'high', label: 'High risk' },
  { value: 'unacceptable', label: 'Unacceptable' },
] as const

const DOCUMENTATION = [
  { value: 'missing', label: 'Missing' },
  { value: 'in_progress', label: 'In progress' },
  { value: 'complete', label: 'Complete' },
] as const

/**
 * Add or edit an AI system.
 *
 * THE CONFIRMATION IS THE POINT OF THIS COMPONENT
 *
 * A classification change is refused by the database unless the request carries
 * `reviewed`, because an Article 6 classification is the determination the whole
 * high-risk obligation stack hangs off. Moving a system out of `high` in a form
 * somebody was tabbing through would quietly retire Articles 9 to 17 for it.
 *
 * So the confirmation appears the moment the select changes away from the
 * recorded value, computed here from the old value and the new one. The server
 * is still the enforcer and refuses without it; this is the half that means a
 * person sees the checkbox BEFORE submitting rather than reading a sentence
 * about one after being refused.
 *
 * Deliberately not shown when the classification is untouched. A gate that fires
 * on every save is a gate people learn to click through, which is the failure
 * mode a reviewed approval exists to prevent.
 */
export function SystemForm({
  slug,
  action,
  system,
  onDone,
}: {
  slug: string
  action: (
    slug: string,
    previous: RecordActionState,
    form: FormData,
  ) => Promise<RecordActionState>
  /** Absent when adding. */
  system?: AiSystem
  onDone?: () => void
}) {
  const recorded = system?.riskClassification ?? 'unclassified'
  const [classification, setClassification] = useState(recorded)

  // Adding: only `high` needs confirming, because that is the classification
  // that switches the obligation stack on. Editing: any change does, because
  // switching it off is the dangerous direction.
  const confirmNeeded = system
    ? classification !== recorded
    : classification === 'high'

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
      {system ? (
        <input type="hidden" name="aiSystemId" value={system.aiSystemId} />
      ) : null}

      <Field
        label="System"
        name="name"
        defaultValue={system?.name}
        placeholder="CV ranking model"
        required
      />
      <Field
        label="Supplier"
        name="vendor"
        defaultValue={system?.vendor}
        placeholder="Leave empty if you built it"
        hint="Provider and deployer duties differ, and an organisation that built the system is generally both."
      />
      <Field
        label="Purpose"
        name="purpose"
        defaultValue={system?.purpose}
        placeholder="Ranks applicants for shortlisting"
      />

      <div className="space-y-1.5">
        <Label htmlFor="riskClassification">Risk classification</Label>
        <select
          id="riskClassification"
          name="riskClassification"
          value={classification}
          onChange={(event) => setClassification(event.target.value)}
          className="h-9 w-full rounded-md border border-border bg-transparent px-3 text-sm shadow-xs focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          {RISK.map(({ value, label }) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground">
          Unclassified means nobody has assessed it yet, which is not the same
          as low risk.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="documentationStatus">Technical documentation</Label>
        <select
          id="documentationStatus"
          name="documentationStatus"
          defaultValue={system?.documentationStatus ?? 'missing'}
          className="h-9 w-full rounded-md border border-border bg-transparent px-3 text-sm shadow-xs focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          {DOCUMENTATION.map(({ value, label }) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground">Article 11.</p>
      </div>

      {confirmNeeded ? (
        <ReviewConfirmation
          data-testid="classification-confirmation"
          description={
            system
              ? `This changes the classification from ${labelFor(recorded)} to ${labelFor(classification)}, which changes which AI Act obligations apply.`
              : 'A high-risk classification brings the full Articles 9 to 17 obligation stack with it.'
          }
        />
      ) : null}

      <FormFooter
        state={state}
        pending={pending}
        submitLabel={system ? 'Save changes' : 'Register system'}
        onCancel={onDone}
      />
    </form>
  )
}

function labelFor(value: string): string {
  return RISK.find((entry) => entry.value === value)?.label ?? value
}

/**
 * The checkbox that turns a change into a reviewed approval.
 *
 * Required at the browser as well as at the database, so the refusal a person
 * meets is the one next to the checkbox rather than a sentence after a round
 * trip. The database is still the enforcer: this can be bypassed and the write
 * would then be refused, which is the correct order for a gate that matters.
 */
export function ReviewConfirmation({
  description,
  ...rest
}: {
  description: string
} & React.ComponentProps<'div'>) {
  return (
    <div
      className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3"
      {...rest}
    >
      <p className="text-sm text-foreground">{description}</p>
      <label className="mt-2 flex items-start gap-2 text-sm text-muted-foreground">
        <input
          type="checkbox"
          name="reviewed"
          required
          className="mt-0.5 size-4 rounded border-border"
        />
        <span>Record this as a reviewed approval.</span>
      </label>
    </div>
  )
}
