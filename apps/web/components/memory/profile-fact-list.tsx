import Link from 'next/link'

import { CorrectableFact } from '@/components/memory/correct-fact-form'
import type { ActionState } from '@/lib/org/action-state'
import {
  FACT_LABELS,
  SOURCE_LABELS,
  readValue,
  type ProfileFact,
} from '@/lib/memory/client'
import { orgPath } from '@/lib/auth/org'

/**
 * What Kindlast believes, one row per fact (ENT-228).
 *
 * # THE SOURCE IS SHOWN AGAINST EVERY VALUE, NOT AGAINST THE PROFILE
 *
 * A profile where one field came from a connected tool and another from a
 * guess somebody typed during setup is the normal case rather than a corner
 * one. Showing provenance once at the top would let a reader assume the tool
 * vouched for all of it, which is exactly the wrong impression to give about a
 * record a compliance decision rests on.
 *
 * # "NOT SURE" IS RENDERED, NOT BLANKED
 *
 * A value can be absent or it can be recorded as unknown, and those are
 * different states. The second is actionable: "you told us you do not know
 * whether you keep a record of processing activities" is a finding waiting to
 * happen. Rendering both as empty throws away the one worth acting on.
 */
export function ProfileFactList({
  facts,
  slug,
  correct,
}: {
  facts: ProfileFact[]
  slug: string
  /** Omitted by the tests that only assert rendering, and by any caller with
   *  no write to offer. A list with no correction is still a useful list. */
  correct?: (
    slug: string,
    previous: ActionState,
    form: FormData,
  ) => Promise<ActionState>
}) {
  return (
    <ul className="mt-3 divide-y divide-border/60 rounded-xl border border-border/60 bg-background">
      {facts.map((fact) => {
        const key = fact.key
        if (!key) return null

        const value = readValue(fact)
        const source = fact.source ? SOURCE_LABELS[fact.source] : undefined

        return (
          <li key={key} className="p-4">
            {/* The row, and then the correction below it rather than inside
                it. A form opening inside a baseline-aligned flex row would be
                squeezed into the width of a link. */}
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <div className="min-w-0">
                <p className="text-sm font-medium text-foreground">
                  {FACT_LABELS[key]}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {source ?? fact.source}
                  {fact.validFrom ? (
                    <>
                      {' · since '}
                      <time dateTime={fact.validFrom}>
                        {formatDay(fact.validFrom)}
                      </time>
                    </>
                  ) : null}
                </p>
              </div>

              <div className="flex items-baseline gap-4">
                <p className="text-sm text-foreground">
                  {value ?? (
                    <span className="text-muted-foreground">Not recorded</span>
                  )}
                </p>
                <Link
                  href={orgPath(slug, `/settings/memory/${key}`)}
                  className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
                >
                  History
                </Link>
              </div>
            </div>

            {correct ? (
              <CorrectableFact slug={slug} fact={fact} action={correct} />
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}

/**
 * The day, not the instant.
 *
 * The stored timestamp is precise to the microsecond because the intervals
 * have to meet exactly, and none of that precision means anything to a person
 * reading when we started believing something.
 */
function formatDay(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}
