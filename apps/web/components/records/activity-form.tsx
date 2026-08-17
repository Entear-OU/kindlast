'use client'

import { useActionState, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { idle, type RecordActionState } from '@/lib/records/action-state'
import type { ProcessingActivity } from '@/lib/records/client'

/**
 * Add or edit an Article 30 entry.
 *
 * One component for both, because a create form and an edit form that drift
 * apart accept different things about the same record, and the one nobody looks
 * at is the one that stops asking for a retention period.
 *
 * THE LIST FIELDS ARE RAW TEXT UNTIL SUBMIT
 *
 * `dataCategories` and `recipients` are comma-separated in a single input,
 * held as typed and split by the action. Splitting on every keystroke turns a
 * half-typed "name, ba" into two entries and moves the cursor, which is the
 * kind of thing that makes a form feel broken while being technically correct.
 *
 * EVERY FIELD IS SENT, INCLUDING THE EMPTY ONES
 *
 * The contract is a full replacement rather than a patch, so an omitted field
 * clears the value. That is deliberate (clearing a wrong legal basis is a real
 * edit) and it means this form must be populated with the current record when
 * editing, or saving a rename would wipe everything else.
 */
export function ActivityForm({
  slug,
  action,
  activity,
  onDone,
}: {
  slug: string
  action: (
    slug: string,
    previous: RecordActionState,
    form: FormData,
  ) => Promise<RecordActionState>
  /** Absent when adding. */
  activity?: ProcessingActivity
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
      {activity ? (
        <input
          type="hidden"
          name="processingActivityId"
          value={activity.processingActivityId}
        />
      ) : null}

      <Field
        label="Activity"
        name="name"
        defaultValue={activity?.name}
        placeholder="Payroll"
        required
      />
      <Field
        label="Purpose"
        name="purpose"
        defaultValue={activity?.purpose}
        placeholder="Paying staff and meeting tax obligations"
        hint="Article 30(1)(b)."
      />
      <Field
        label="Legal basis"
        name="legalBasis"
        defaultValue={activity?.legalBasis}
        placeholder="Article 6(1)(b), performance of a contract"
        hint="A legal determination. Leave it empty rather than guessing."
      />
      <Field
        label="Data categories"
        name="dataCategories"
        defaultValue={activity?.dataCategories?.join(', ')}
        placeholder="name, email address, bank details"
        hint="Separate with commas."
      />
      <Field
        label="Recipients"
        name="recipients"
        defaultValue={activity?.recipients?.join(', ')}
        placeholder="our accountant, the tax authority"
        hint="Separate with commas."
      />
      <Field
        label="Retention"
        name="retentionPeriod"
        defaultValue={activity?.retentionPeriod}
        placeholder="7 years after employment ends"
        hint="In your own words. Article 30(1)(f)."
      />

      <FormFooter
        state={state}
        pending={pending}
        submitLabel={activity ? 'Save changes' : 'Add activity'}
        onCancel={onDone}
      />
    </form>
  )
}

export function Field({
  label,
  name,
  defaultValue,
  placeholder,
  hint,
  required,
}: {
  label: string
  name: string
  defaultValue?: string
  placeholder?: string
  hint?: string
  required?: boolean
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={name}>{label}</Label>
      <Input
        id={name}
        name={name}
        defaultValue={defaultValue ?? ''}
        placeholder={placeholder}
        required={required}
      />
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

/**
 * The submit row and whatever the last attempt said.
 *
 * The message is in a live region so a screen reader hears a refusal that
 * appears without the focus moving, which is every refusal here.
 */
export function FormFooter({
  state,
  pending,
  submitLabel,
  onCancel,
}: {
  state: RecordActionState
  pending: boolean
  submitLabel: string
  onCancel?: () => void
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : submitLabel}
        </Button>
        {onCancel ? (
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
      </div>

      <p
        role="status"
        aria-live="polite"
        data-testid="record-form-message"
        className={
          state.status === 'error'
            ? 'text-sm text-destructive'
            : 'text-sm text-muted-foreground'
        }
      >
        {state.message}
      </p>
    </div>
  )
}

/**
 * A disclosure that holds a form, so the page is a register and not a form.
 *
 * CONTROLLED, AND THE CHILD IS AN ELEMENT RATHER THAN A RENDER FUNCTION
 *
 * The obvious shape is `children: (close) => ReactNode`, so the disclosure can
 * hand its own close function to whatever it wraps. That works everywhere
 * except across the server/client boundary, which is exactly where this is
 * used: a function is not serialisable, and a server component passing one to a
 * client component fails at request time with "Functions are not valid as a
 * child of Client Components". It compiles, typechecks, lints and passes unit
 * tests, because a test file is already a client context.
 *
 * So the open state is lifted to the caller, which is a client component that
 * can therefore pass a real element and close it itself. See the add controls
 * in `editable.tsx`.
 */
export function AddDisclosure({
  label,
  title,
  open,
  onOpenChange,
  disabled,
  disabledReason,
  children,
}: {
  label: string
  title: string
  open: boolean
  onOpenChange: (open: boolean) => void
  disabled?: boolean
  disabledReason?: string
  children: React.ReactNode
}) {
  const setOpen = onOpenChange

  if (disabled) {
    // Present and visibly unavailable, with the reason next to it. A control
    // that silently does nothing is worse than one that says why not.
    return (
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" disabled>
          {label}
        </Button>
        {disabledReason ? (
          <p className="text-xs text-muted-foreground">{disabledReason}</p>
        ) : null}
      </div>
    )
  }

  if (!open) {
    return (
      <Button type="button" onClick={() => setOpen(true)}>
        {label}
      </Button>
    )
  }

  return (
    <section className="rounded-xl border border-border/60 p-4">
      <h3 className="text-sm font-medium text-foreground">{title}</h3>
      <div className="mt-4">{children}</div>
    </section>
  )
}
