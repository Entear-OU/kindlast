'use client'

import { useActionState, useState } from 'react'

import { AddDisclosure, Field } from '@/components/records/activity-form'
import { FormFooter } from '@/components/records/activity-form'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { idle, type RecordActionState } from '@/lib/records/action-state'
import type { DsarTrailEntry } from '@/lib/records/client'

/**
 * The trail a DSAR response was built from (ENT-226).
 *
 * WHAT THIS IS FOR, AND WHY IT IS NOT A LIST OF EDITS
 *
 * `respondedAt` says a response went out. On its own it is an assertion with
 * nothing behind it, and a regulator reading the register cannot see what was
 * searched, what was found or what was returned. These entries are that record.
 *
 * So there is no edit control and no delete control on an entry, anywhere. The
 * database refuses an UPDATE with a trigger that binds even the migrator, and
 * the application holds no DELETE grant, so a control for either would be a
 * button that cannot work. A correction is another entry, which is how a paper
 * file works.
 *
 * TWO TIMESTAMPS, BOTH SHOWN
 *
 * When the search happened and when it was written up are different facts, and
 * the gap between them is itself informative: a trail written up entirely on the
 * last day looks different from one kept as the work was done. Showing only the
 * first would hide that; showing only the second would misdate the search.
 */

/** The five actions, with the words a person reads instead of the stored value. */
const ACTIONS: { value: string; label: string; hint: string }[] = [
  {
    value: 'searched',
    label: 'Searched',
    hint: 'Somebody looked here.',
  },
  {
    value: 'found',
    label: 'Found data',
    hint: 'Personal data about the subject was here.',
  },
  {
    value: 'none_found',
    label: 'Nothing found',
    hint: 'Somebody looked here and there was nothing. Different from never having looked.',
  },
  {
    value: 'disclosed',
    label: 'Disclosed',
    hint: 'What was found went into the response.',
  },
  {
    value: 'withheld',
    label: 'Withheld',
    hint: 'Found and deliberately not disclosed. Say why: Article 15(4) and the exemptions are real, and a silent omission is evidence of nothing.',
  },
]

const LABELS = new Map(ACTIONS.map(({ value, label }) => [value, label]))

/**
 * Stores worth suggesting when the request reaches data Kindlast itself holds.
 *
 * A datalist and not a closed list, because the stores are the customer's
 * estate. These three are Kindlast's own, from `docs/personal-data-runbook.md`,
 * and they are here for the case where a customer's DSAR reaches data held on
 * their behalf rather than as an instruction about where to look.
 */
const SUGGESTED_STORES = ['postgres-app', 'postgres-platform', 'Redis']

function when(value: string | undefined): string {
  if (!value) return 'unknown'
  return new Date(value).toLocaleString()
}

/** One request's trail, oldest first, as the server ordered it. */
export function Trail({ entries }: { entries: DsarTrailEntry[] }) {
  if (entries.length === 0) {
    return (
      <p
        data-testid="trail-empty"
        className="rounded-xl border border-border/60 p-4 text-sm text-muted-foreground"
      >
        Nothing recorded yet. Until something is, a date in{' '}
        <span className="font-medium text-foreground">Responded</span> is the
        organisation&rsquo;s word for it and nothing more. Record each store as
        you search it, including the ones that turn out to hold nothing.
      </p>
    )
  }

  return (
    <ol data-testid="trail" className="space-y-3">
      {entries.map((entry) => (
        <li
          key={entry.entryId}
          className="rounded-xl border border-border/60 p-4"
        >
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <h3 className="text-sm font-medium text-foreground">
              {entry.source}
            </h3>
            <span className="text-xs text-muted-foreground">
              {LABELS.get(entry.action) ?? entry.action}
            </span>
          </div>

          {entry.detail ? (
            <p className="mt-2 text-sm text-muted-foreground">{entry.detail}</p>
          ) : null}

          <dl className="mt-3 grid gap-x-6 gap-y-1 text-xs text-muted-foreground sm:grid-cols-2">
            <div>
              <dt className="inline font-medium text-foreground">Happened </dt>
              <dd className="inline">{when(entry.occurredAt)}</dd>
            </div>
            <div>
              <dt className="inline font-medium text-foreground">Recorded </dt>
              <dd className="inline">{when(entry.recordedAt)}</dd>
            </div>
          </dl>

          {/* Provenance, when a run contributed it. Shown rather than hidden
              behind a toggle: "a model did this" is the first thing somebody
              checking the trail needs to know, and it is empty on every entry a
              person wrote. */}
          {entry.agentRunId ? (
            <p className="mt-2 text-xs text-muted-foreground">
              Contributed by agent run{' '}
              <code className="font-mono">{entry.agentRunId}</code>
            </p>
          ) : null}
        </li>
      ))}
    </ol>
  )
}

/** Appending one step, which is the only write this record has. */
export function AddTrailEntry({
  slug,
  dsarId,
  action,
}: {
  slug: string
  dsarId: string
  action: (
    slug: string,
    dsarId: string,
    previous: RecordActionState,
    form: FormData,
  ) => Promise<RecordActionState>
}) {
  const [open, setOpen] = useState(false)
  const [state, submit, pending] = useActionState(
    async (previous: RecordActionState, form: FormData) => {
      const next = await action(slug, dsarId, previous, form)
      if (next.status === 'ok') setOpen(false)
      return next
    },
    idle,
  )

  // Now, in the browser's own timezone and in the shape `datetime-local` wants.
  // Capped at the same value, because a search cannot have happened later than
  // now. The server refuses a future time too; the attribute is what stops
  // somebody meeting that refusal after filling the form in.
  const now = new Date()
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16)

  return (
    <AddDisclosure
      label="Record a step"
      title="What was searched, and what came back"
      open={open}
      onOpenChange={setOpen}
    >
      <form action={submit} className="space-y-4">
        <Field
          label="Store"
          name="source"
          placeholder="Salesforce, the HR system, our payroll export…"
          hint="In your own words. These are your systems, so the suggestions are a starting point rather than the set."
          required
          list="trail-store-suggestions"
        />
        <datalist id="trail-store-suggestions">
          {SUGGESTED_STORES.map((store) => (
            <option key={store} value={store} />
          ))}
        </datalist>

        <div className="space-y-1.5">
          <Label htmlFor="action">What happened</Label>
          <select
            id="action"
            name="action"
            defaultValue="searched"
            className="h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            {ACTIONS.map(({ value, label }) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          {/* Every value with what it means, rather than the selected one's
              hint. The distinction that matters most is between two of them,
              so a reader has to see both at once: nothing found and never
              looked are different facts, and only one is evidence. */}
          <dl className="space-y-0.5 text-xs text-muted-foreground">
            {ACTIONS.map(({ value, label, hint }) => (
              <div key={value}>
                <dt className="inline font-medium text-foreground">
                  {label}.{' '}
                </dt>
                <dd className="inline">{hint}</dd>
              </div>
            ))}
          </dl>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="occurredAt">When it happened</Label>
          <input
            id="occurredAt"
            name="occurredAt"
            type="datetime-local"
            defaultValue={local}
            max={local}
            className="h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
          <p className="text-xs text-muted-foreground">
            The moment you searched, not the moment you are writing it down. The
            record keeps both.
          </p>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="detail">Detail</Label>
          <Textarea
            id="detail"
            name="detail"
            rows={3}
            placeholder="What you looked for, what came back, or why something was withheld"
          />
          <p className="text-xs text-muted-foreground">
            Describe the data, do not paste it. Everyone who can read the
            register can read this, so &ldquo;employment record, 2019 to
            2024&rdquo; belongs here and the record itself does not.
          </p>
        </div>

        <FormFooter
          state={state}
          pending={pending}
          submitLabel="Record it"
          onCancel={() => setOpen(false)}
        />
      </form>
    </AddDisclosure>
  )
}
