'use client'

import { useActionState, useMemo } from 'react'

import { Ledger, LedgerSummary } from '@/components/onboarding/ledger'
import { QuestionCard } from '@/components/onboarding/question-card'
import { Summary } from '@/components/onboarding/summary'
import { answersFrom } from '@/lib/onboarding/answers'
import { ANSWERS_ARE_SAVED } from '@/lib/onboarding/copy'
import { assess, ledger } from '@/lib/onboarding/evaluate'
import { idle, type ActionState } from '@/lib/org/action-state'
import type { OnboardingState } from '@/lib/onboarding/client'

/**
 * The interview, which is the readiness assessment (ENT-212, ENT-189, ENT-254).
 *
 * # TWO FLOWS BECAME ONE, AND THIS IS THE ONE
 *
 * `/readiness` was thirteen tapped questions with the corpus narrowing beside
 * them, and it persisted nothing. Onboarding was about eleven typed questions
 * that fed the product. Asking a customer both was the obvious problem, and the
 * ruling was that onboarding takes the readiness flow. So this component is
 * that layout: the question on the left, the corpus column on the right, and
 * every answer a round trip to core-api rather than a React hook.
 *
 * # THE ANSWERS ARE SERVER STATE, WHICH IS WHAT CHANGED
 *
 * `/readiness` held them in `useState` and said so on the page, because "your
 * answers never leave this page" was its whole promise. Here every question is
 * a row and every answer is a row, a refresh picks up where the person left
 * off, and the fact is written the moment the answer is given. There is no
 * confirmation step to skip past because there is no confirmation step.
 *
 * That is why this component derives everything it shows from `state` and holds
 * no answer sheet of its own. A local copy would be a second source of truth
 * for what a customer's profile contains, and the moment the two disagreed the
 * corpus column would be showing an obligation the product does not.
 *
 * # THE CORPUS COLUMN IS EVALUATED IN THE BROWSER, AND ON PURPOSE
 *
 * `lib/onboarding/evaluate.ts` is a port of `watcher_obligation_applies`, and
 * the reason it runs here rather than being asked for is that the column has to
 * resolve between one tap and the next. The fidelity requirement that creates
 * is documented in that file, and it is what forced the closed vocabulary.
 *
 * # THERE IS NO MODEL IN ANY OF THIS
 *
 * ENT-251 was closed on the argument that a model drafting these questions is a
 * fabrication path aimed at the input to every finding. Moving the flow did not
 * reopen it: the questions are a Go script, the parsing is Go, and the only
 * generated text on screen is none.
 */

type Action = (
  slug: string,
  previous: ActionState,
  form: FormData,
) => Promise<ActionState>

export function Interview({
  slug,
  state,
  answer,
  dashboardHref,
  memoryHref,
}: {
  slug: string
  state: OnboardingState
  answer: Action
  dashboardHref: string
  memoryHref: string
}) {
  const [result, submit, pending] = useActionState(
    async (previous: ActionState, form: FormData) =>
      answer(slug, previous, form),
    idle,
  )

  const answers = useMemo(() => answersFrom(state), [state])
  const rows = useMemo(() => ledger(answers), [answers])
  const question = state.nextQuestion

  if (!question?.key) {
    return (
      <Summary
        assessment={assess(answers)}
        dashboardHref={dashboardHref}
        memoryHref={memoryHref}
      />
    )
  }

  const answered = state.answeredQuestions ?? 0
  const total = state.totalQuestions ?? 0

  return (
    <div className="grid gap-12 lg:grid-cols-[1fr_17rem] lg:gap-14">
      <div>
        {/* On a phone the column below is a scroll away, so the counts lead
            here instead: somebody should see the corpus narrowing while they
            answer, not discover it afterwards. */}
        <LedgerSummary rows={rows} className="mb-6 lg:hidden" />

        <QuestionCard
          // Remounted per question, so a multi-select never opens with the
          // chips from the last one still pressed.
          key={question.key}
          question={question}
          step={Math.min(answered + 1, total)}
          total={total}
          submit={submit}
          pending={pending}
          error={result.status === 'error' ? result.message : undefined}
        />

        <p className="mt-12 max-w-[52ch] text-[13px] leading-[1.65] text-muted-foreground">
          {ANSWERS_ARE_SAVED}
        </p>
      </div>

      <Ledger rows={rows} />
    </div>
  )
}
