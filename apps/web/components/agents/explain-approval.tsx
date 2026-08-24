'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import type { LeftForYou, PreparedField } from '@/lib/agents/approval'
import { idle, type ExplainState } from '@/lib/agents/explain-state'

/**
 * What approving this finding will do, from the Hands (ENT-278, §26.5).
 *
 * ENT-261 built the agent and left it with nowhere to be asked anything, so the
 * rail said "Working, in part" about a skill nobody could reach. This is where
 * a person reaches it: on the finding page, immediately above the decision it
 * is about.
 *
 * # THIS IS THE MOST CONSEQUENTIAL PLACE IN THE PRODUCT TO PUT GENERATED PROSE
 *
 * Everything below the heading is arranged around that. The paragraph the model
 * wrote is marked as the Hands' in the same words the Analyst's narrative is
 * marked with, because a second phrasing for the same claim is a second thing to
 * keep true. The register it names is drawn OUTSIDE that paragraph, because what
 * approving does is a statement about this product rather than a model's
 * opinion, and core-api authors it. The plan says what was left as plainly as
 * what was filled, and every filled value carries the fact it came from.
 *
 * # AND IT RUNS ONLY WHEN SOMEBODY ASKS
 *
 * A run spends a model budget and writes a proposed payload onto the finding.
 * Running on arrival would do both to every person who opened a finding to read
 * it, and the second of those changes what approving would create. The Analyst's
 * box refuses to run on arrival for the first reason alone; this one has both.
 *
 * A client component only because it needs `useActionState` to show the answer
 * without a full navigation. The action is a server action, so no token and no
 * organisation id ever reaches the browser.
 */
export function ExplainApproval({
  slug,
  findingId,
  action,
}: {
  slug: string
  findingId: string
  action: (state: ExplainState, form: FormData) => Promise<ExplainState>
}) {
  const [state, submit, asking] = useActionState(action, idle)

  return (
    <section
      aria-label="What approving will do"
      className="mt-8 rounded-xl border border-border/60 bg-background p-5"
    >
      <h2 className="text-sm font-medium text-foreground">
        What approving will do
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        The Hands reads this finding and what you have told us about your
        organisation, and says what approving would add to your records and what
        it could not fill in. It prepares and it never decides: the decision
        below stays yours.
      </p>

      <form
        action={submit}
        aria-label="Ask the Hands"
        className="mt-4 flex items-center gap-3"
      >
        {/* The finding, and the slug the action re-resolves the organisation
            from. Deliberately no org id: a hidden field carrying one is a field
            somebody can edit. */}
        <input type="hidden" name="findingId" value={findingId} readOnly />
        <input type="hidden" name="slug" value={slug} readOnly />

        <Button type="submit" disabled={asking}>
          {asking ? 'Asking the Hands' : 'Ask the Hands'}
        </Button>
      </form>

      <Result state={state} />
    </section>
  )
}

/**
 * The five states, each drawn as what it is.
 *
 * A switch rather than a chain of conditionals, so adding a state to
 * `ExplainState` without drawing it is a type error rather than a panel that
 * renders nothing.
 */
function Result({ state }: { state: ExplainState }) {
  switch (state.status) {
    case 'idle':
      return null

    case 'explained':
      return (
        <div className="mt-5 space-y-4">
          {state.registerLabel ? (
            <p
              data-testid="approval-register"
              className="text-sm text-muted-foreground"
            >
              Approving adds an entry to {state.registerLabel}.
            </p>
          ) : null}

          <p
            data-testid="approval-explanation"
            className="text-[15px] leading-relaxed whitespace-pre-line text-foreground"
          >
            {state.explanation}
          </p>

          {/* GENERATED PROSE IS NEVER UNMARKED, and the wording is the
              Analyst's narrative's wording on purpose (ENT-248). The run is
              named rather than linked because `agent_runs` has no read path,
              so the id is shown as the reference it is: quotable in a support
              conversation rather than a link to nowhere. */}
          <p
            data-testid="approval-attribution"
            className="text-xs text-muted-foreground"
          >
            Prepared by the Hands about your organisation, not a statement of
            the law
            {state.agentRunId ? (
              <>
                , run <span className="font-mono">{state.agentRunId}</span>
              </>
            ) : null}
            .
          </p>

          <Filled fields={state.prepared} />
          <Left fields={state.leftForYou} />
        </div>
      )

    case 'refused':
      return (
        <div
          data-testid="approval-refused"
          className="mt-5 rounded-lg border border-border/60 bg-muted/40 p-4"
        >
          {/* Not an alert. Nothing went wrong, and a screen reader interrupting
              with an error would describe the guardrail as a failure. */}
          <p className="text-[13px] font-medium text-foreground">
            The Hands did not prepare this one
          </p>
          <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
            {state.reason}
          </p>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            That is a guardrail working rather than something broken. Nothing
            was added to your records, and the decision below is unchanged.
            {state.agentRunId ? (
              <>
                {' '}
                The run is <span className="font-mono">{state.agentRunId}</span>
                .
              </>
            ) : null}
          </p>
        </div>
      )

    case 'unavailable':
      return (
        <p
          data-testid="approval-unavailable"
          className="mt-5 rounded-lg border border-border/60 bg-muted/40 px-4 py-3 text-sm text-muted-foreground"
        >
          This deployment runs no model, so there is nothing to ask. An operator
          brings one up with the model profile. Approving still works, and the
          record it creates will say which columns are not recorded.
        </p>
      )

    case 'error':
      return (
        <p
          role="alert"
          className="mt-5 rounded-lg border border-border/60 bg-muted/40 px-4 py-3 text-sm text-muted-foreground"
        >
          {state.message}
        </p>
      )
  }
}

/**
 * What the run filled, and where each value came from.
 *
 * A run that filled nothing says so in a sentence rather than rendering an
 * empty box. An organisation that has recorded little about itself is the
 * ordinary case rather than a defect, and an empty list under a heading reads
 * like one.
 */
function Filled({ fields }: { fields: PreparedField[] }) {
  if (fields.length === 0) {
    return (
      <p
        data-testid="approval-nothing-filled"
        className="text-sm text-muted-foreground"
      >
        It could fill in nothing from what you have told us so far, so every
        column below is yours to complete.
      </p>
    )
  }

  return (
    <div data-testid="approval-prepared">
      <h3 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
        Filled in from what you have told us
      </h3>
      <dl className="mt-2 space-y-3">
        {fields.map((field) => (
          <div key={field.name}>
            <dt className="text-sm text-muted-foreground">
              {field.label || field.name}
            </dt>
            <dd className="text-[15px] text-foreground">
              {(field.values ?? []).join(', ')}
            </dd>
            {/* THE SOURCE, BESIDE THE VALUE. A value attributed to the
                organisation's own memory that came from nowhere is a
                fabrication, and this line is how a customer checks rather than
                trusts. It is refused on the way in too: core-api will not
                accept a field naming a fact the organisation does not hold. */}
            {field.fromFact ? (
              <dd className="text-xs text-muted-foreground">
                from what you recorded as{' '}
                <span className="font-mono">{field.fromFact}</span>
              </dd>
            ) : null}
          </div>
        ))}
      </dl>
    </div>
  )
}

/**
 * What it left for a person, and why.
 *
 * Carried everywhere the filled half is. A plan listing three filled columns
 * and saying nothing about the fourth reads as complete, and a record that
 * reads as complete and is not would be worse than the empty one this agent
 * exists to improve on.
 */
function Left({ fields }: { fields: LeftForYou[] }) {
  if (fields.length === 0) return null

  return (
    <div data-testid="approval-left">
      <h3 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
        Left for you
      </h3>
      <ul className="mt-2 space-y-2">
        {fields.map((field) => (
          <li key={field.name} className="text-sm">
            <span className="text-foreground">{field.label || field.name}</span>
            {field.why ? (
              <span className="text-muted-foreground">: {field.why}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  )
}
