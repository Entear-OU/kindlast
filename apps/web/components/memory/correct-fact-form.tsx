'use client'

import { useActionState, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { idle, type ActionState } from '@/lib/org/action-state'
import {
  FACT_LABELS,
  readValue,
  type ProfileFact,
  type ProfileFactKey,
} from '@/lib/memory/client'

/**
 * Recording what is true now (ENT-228, §26.5).
 *
 * # IT IS NOT AN EDIT AND IT DOES NOT LOOK LIKE ONE
 *
 * A correction closes the current value and records a new one; the previous
 * answer survives on the history page. `kindlast_app` holds `update
 * (valid_to)` and not `update (value)`, so an in-place rewrite is refused by
 * the database rather than merely discouraged.
 *
 * That shapes the copy as much as the code. The button says "Record", not
 * "Save", and the confirmation says the previous answer is kept. A person told
 * their change was saved reasonably assumes the old one is gone, and here it
 * is not.
 *
 * # ONE FIELD AT A TIME, OPENED FROM THE LIST
 *
 * Not a form of every fact at once. A profile page that submits ten values
 * together makes one correction indistinguishable from ten, and the history is
 * per fact: submitting the form would stamp a change against every field
 * somebody did not touch. Opening one field keeps a correction a correction.
 *
 * # "NOT SURE" IS AN OPTION, NOT AN EMPTY FIELD
 *
 * "We do not know whether we keep a record of processing activities" is a
 * finding in itself, and a form offering only yes and no would push every such
 * organisation to "no", which is a different claim about the same
 * organisation.
 */

const TRI_STATE_FACTS: ReadonlySet<string> = new Set([
  'PROFILE_FACT_KEY_HAS_DPO',
  'PROFILE_FACT_KEY_HAS_ROPA',
  'PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU',
])

const LIST_FACTS: ReadonlySet<string> = new Set([
  'PROFILE_FACT_KEY_EU_JURISDICTIONS',
  'PROFILE_FACT_KEY_DATA_CATEGORIES',
  'PROFILE_FACT_KEY_DATA_SUBJECTS',
  'PROFILE_FACT_KEY_AI_SYSTEMS',
  'PROFILE_FACT_KEY_TRANSFER_DESTINATIONS',
])

type Action = (
  slug: string,
  previous: ActionState,
  form: FormData,
) => Promise<ActionState>

export function CorrectFactForm({
  slug,
  fact,
  action,
  onDone,
}: {
  slug: string
  fact: ProfileFact
  action: Action
  onDone?: () => void
}) {
  const key = fact.key as ProfileFactKey
  const [state, submit, pending] = useActionState(
    async (previous: ActionState, form: FormData) => {
      const next = await action(slug, previous, form)
      if (next.status === 'ok') onDone?.()
      return next
    },
    idle,
  )

  return (
    <form action={submit} className="space-y-4">
      <input type="hidden" name="key" value={key} />

      <div className="space-y-1.5">
        <Label htmlFor="value">{FACT_LABELS[key]}</Label>
        <ValueField fact={fact} />
        <FieldHint factKey={key} />
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="note">Why it changed</Label>
        <Input id="note" name="note" placeholder="We appointed a DPO in June" />
        <p className="text-xs text-muted-foreground">
          Optional, and worth writing. This is what makes the change make sense
          to somebody reading the history in a year.
        </p>
      </div>

      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? 'Recording' : 'Record'}
        </Button>
        {state.message ? (
          <p
            className="text-xs text-muted-foreground"
            role={state.status === 'error' ? 'alert' : 'status'}
          >
            {state.message}
          </p>
        ) : null}
      </div>
    </form>
  )
}

function ValueField({ fact }: { fact: ProfileFact }) {
  const key = fact.key as ProfileFactKey
  const current = readValue(fact)

  if (TRI_STATE_FACTS.has(key)) {
    const selected =
      fact.value?.triState === 'TRI_STATE_YES'
        ? 'yes'
        : fact.value?.triState === 'TRI_STATE_NO'
          ? 'no'
          : fact.value?.triState === 'TRI_STATE_UNSURE'
            ? 'unsure'
            : ''

    return (
      <select
        id="value"
        name="value"
        defaultValue={selected}
        className="h-9 w-full rounded-md border border-border/60 bg-background px-3 text-sm text-foreground"
      >
        <option value="">Choose an answer</option>
        <option value="yes">Yes</option>
        <option value="no">No</option>
        {/* Third, and present. See the component comment. */}
        <option value="unsure">Not sure</option>
      </select>
    )
  }

  if (key === 'PROFILE_FACT_KEY_STAFF_COUNT') {
    return (
      <Input
        id="value"
        name="value"
        inputMode="numeric"
        defaultValue={current ?? ''}
      />
    )
  }

  if (LIST_FACTS.has(key)) {
    return (
      <Input
        id="value"
        name="value"
        // THE RAW LIST, NOT `readValue`'s RENDERING OF IT.
        //
        // `readValue` returns "None" for an empty list, which is right for
        // reading and wrong here: a list holding the single item "None" would
        // be indistinguishable from an empty one, and editing it would silently
        // empty it. Rare, and the kind of rare that is impossible to diagnose
        // from a bug report.
        //
        // Comma separated so somebody edits what they can see rather than
        // retyping it. An empty box means an empty list, which is an answer.
        defaultValue={(fact.value?.list?.values ?? []).join(', ')}
      />
    )
  }

  return <Input id="value" name="value" defaultValue={current ?? ''} />
}

function FieldHint({ factKey }: { factKey: ProfileFactKey }) {
  if (LIST_FACTS.has(factKey)) {
    return (
      <p className="text-xs text-muted-foreground">
        Separate several with commas. Leave it empty if there are none, which is
        an answer rather than a blank.
      </p>
    )
  }
  if (TRI_STATE_FACTS.has(factKey)) {
    return (
      <p className="text-xs text-muted-foreground">
        Not sure is a real answer. We would rather know you are unsure than
        record a no.
      </p>
    )
  }
  return null
}

/**
 * The list, with one row openable for correction.
 *
 * The open row is the only state that lives in the browser. The page stays a
 * server component reading through the session, so no token or organisation id
 * crosses the boundary: the form posts a fact key, and the server action
 * re-resolves the organisation from the slug in the URL.
 */
export function CorrectableFact({
  slug,
  fact,
  action,
}: {
  slug: string
  fact: ProfileFact
  action: Action
}) {
  const [open, setOpen] = useState(false)

  if (!open) {
    return (
      <div className="mt-2 flex justify-end">
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
        >
          Correct
        </button>
      </div>
    )
  }

  return (
    <div className="mt-3 w-full rounded-lg border border-border/60 p-4">
      <CorrectFactForm
        slug={slug}
        fact={fact}
        action={action}
        onDone={() => setOpen(false)}
      />
      <button
        type="button"
        onClick={() => setOpen(false)}
        className="mt-3 text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
      >
        Cancel
      </button>
    </div>
  )
}
