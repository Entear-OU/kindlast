'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { idle, type AskState } from '@/lib/agents/ask-state'
import { MAX_QUESTION_CHARS } from '@/lib/agents/conversation'

/**
 * Asking the Analyst about one finding (ENT-270, §26.5).
 *
 * The rail has said "talking to them is coming" since ENT-222 over three icons
 * that did nothing. This is the first of the three, and it lives on the finding
 * rather than in a chat window because the finding is what makes it safe: a
 * finding names exactly one obligation, the run is offered that obligation and
 * nothing else, and every citation outside it is refused. A chat with no
 * subject would have nothing to check a citation against.
 *
 * # A REFUSAL AND A FAULT ARE DRAWN DIFFERENTLY, WHICH IS THE POINT
 *
 * §26.3 makes refusal what a working guardrail produces. So "the model cited an
 * obligation we never showed it" is not an apology: it is the product doing the
 * thing a customer is paying for, reported plainly, with the run behind it so
 * they can check it. A panel that drew that the way it draws an unreachable
 * service would report the guardrail firing as the guardrail breaking.
 *
 * # AND IT SAYS WHAT IT WILL NOT DO BEFORE SOMEBODY ASKS
 *
 * The most natural question about a finding is "what does the article say", and
 * it is the one question this refuses (ENT-248: two live runs on the 2B tier
 * stated the law backwards beside a citation that resolved). The statement of
 * law is on the same page already, quoted from the corpus, written by a person.
 * Saying so above the box is the difference between a bounded assistant and one
 * that seems broken the first time somebody tries it.
 *
 * A client component only because it needs `useActionState` to show the answer
 * without a full navigation. The action is a server action, so no token and no
 * organisation id ever reaches the browser.
 */
export function AskAnalyst({
  slug,
  findingId,
  action,
  initialQuestion,
}: {
  slug: string
  findingId: string
  action: (state: AskState, form: FormData) => Promise<AskState>
  /**
   * Words that arrived through Kindy's composer in the rail, prefilled and
   * nothing more: the send is still the person's, on this page, with the
   * finding and its regulation in front of them.
   */
  initialQuestion?: string
}) {
  const [state, submit, asking] = useActionState(action, idle)

  return (
    <section
      aria-label="Ask the Analyst"
      className="mt-8 rounded-xl border border-border/60 bg-background p-5"
    >
      <h2 className="text-sm font-medium text-foreground">Ask the Analyst</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        About this finding and your organisation. It will not tell you what the
        law says: that is the quoted text above, which a person wrote.
      </p>

      <form
        action={submit}
        aria-label="Ask the Analyst"
        className="mt-4 space-y-3"
      >
        {/* The finding, and the slug the action re-resolves the organisation
            from. Deliberately no org id: a hidden field carrying one is a field
            somebody can edit, and the slug is checked against the caller's own
            memberships on every request. */}
        <input type="hidden" name="findingId" value={findingId} readOnly />
        <input type="hidden" name="slug" value={slug} readOnly />

        <label htmlFor="analyst-question" className="sr-only">
          Ask the Analyst about this finding
        </label>
        <textarea
          id="analyst-question"
          name="question"
          rows={3}
          required
          // Mirrors the harness's own limit as a courtesy, not as the control:
          // the refusal that matters is recorded on the run. See
          // MAX_QUESTION_CHARS.
          maxLength={MAX_QUESTION_CHARS}
          defaultValue={initialQuestion}
          placeholder="Why does this apply to us?"
          className="w-full resize-y rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/70"
        />

        <Button type="submit" disabled={asking}>
          {asking ? 'Asking' : 'Ask'}
        </Button>
      </form>

      <Result state={state} />
    </section>
  )
}

/**
 * The five states, each drawn as what it is.
 *
 * A switch rather than a chain of conditionals, so adding a state to `AskState`
 * without drawing it is a type error rather than a panel that renders nothing.
 */
function Result({ state }: { state: AskState }) {
  switch (state.status) {
    case 'idle':
      return null

    case 'answered':
      return (
        <div className="mt-5 space-y-3">
          <Asked question={state.question} />
          <p
            data-testid="answer-text"
            className="text-[15px] leading-relaxed whitespace-pre-line text-foreground"
          >
            {state.answer}
          </p>
          <RunRecord run={state.run} />
        </div>
      )

    case 'refused':
      return (
        <div className="mt-5 space-y-3">
          <Asked question={state.question} />
          {/* Not an alert. Nothing went wrong, and a screen reader interrupting
              with an error would be describing the guardrail as a failure. */}
          <div
            data-testid="answer-refused"
            className="rounded-lg border border-border/60 bg-muted/40 p-4"
          >
            <p className="text-[13px] font-medium text-foreground">
              The Analyst did not answer this one
            </p>
            <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
              {state.reason}
            </p>
            <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
              That is a guardrail working rather than something broken. The run
              below is the record of it.
            </p>
          </div>
          <RunRecord run={state.run} />
        </div>
      )

    case 'unavailable':
      return (
        <p
          data-testid="answer-unavailable"
          className="mt-5 rounded-lg border border-border/60 bg-muted/40 px-4 py-3 text-sm text-muted-foreground"
        >
          This deployment runs no model, so there is nothing to ask. An operator
          brings one up with the model profile; nothing about this finding is
          missing without it.
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

/** What was asked, because the box is cleared by the time an answer arrives. */
function Asked({ question }: { question: string }) {
  return (
    <p className="border-l-2 border-border/60 pl-3 text-sm text-muted-foreground">
      {question}
    </p>
  )
}

/**
 * How this was produced (§26).
 *
 * Every field here is what the run recorded in `agent_runs`, carried back in
 * the same response that recorded it. It is a summary rather than a link
 * because that table has no read path yet, which is worth knowing when this
 * grows a "see the full run" control: the id shown here is what such a control
 * would resolve.
 */
function RunRecord({ run }: { run?: { [K in RunField]?: string | string[] } }) {
  if (!run?.agentRunId) return null

  return (
    <dl
      data-testid="agent-run"
      className="rounded-lg border border-border/60 bg-muted/40 p-4 text-xs"
    >
      <p className="text-[13px] font-medium text-foreground">
        How this was produced
      </p>
      <Row label="Skill">
        {run.skill} {run.skillVersion}
      </Row>
      <Row label="Model">
        {run.model} {run.modelVersion}
      </Row>
      {/* Where the words went, which is what a sub-processor record needs.
          `instance` means this deployment's own model and nothing left it. */}
      <Row label="Processed by">
        {run.provider === 'instance' ? 'this deployment' : run.provider}
      </Row>
      {Array.isArray(run.resolvedCitations) && run.resolvedCitations.length ? (
        <Row label="Relied on">{run.resolvedCitations.join(', ')}</Row>
      ) : null}
      <Row label="Run">{run.agentRunId}</Row>
    </dl>
  )
}

type RunField =
  | 'agentRunId'
  | 'skill'
  | 'skillVersion'
  | 'model'
  | 'modelVersion'
  | 'provider'
  | 'resolvedCitations'

function Row({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="mt-2 flex gap-2">
      <dt className="w-24 shrink-0 text-muted-foreground">{label}</dt>
      <dd className="font-mono break-all text-muted-foreground">{children}</dd>
    </div>
  )
}
