'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { idle, type ActionState } from '@/lib/org/action-state'
import { FACT_LABELS, readValue, type ProfileFact } from '@/lib/memory/client'
import type { DraftFact, OnboardingState } from '@/lib/onboarding/client'

/**
 * The interview (ENT-212).
 *
 * # IT LOOKS LIKE A CONVERSATION BECAUSE IT IS ONE, AND IT IS ALSO A FORM
 *
 * Every question is a row, every answer is a row, and a refresh picks up
 * exactly where the person left off, because the transcript is server state
 * rather than something held in this component. ENT-212 asks that an instance
 * with no model configured "degrades to a form rather than failing"; this is
 * that form, and it is also what a deployment WITH a model runs, so there is no
 * second path to keep working.
 *
 * # THE CONTROL FOLLOWS THE SHAPE CORE-API DECLARES
 *
 * A tri-state question renders three buttons and nothing else, because the
 * server accepts exactly yes, no and unsure, and a free-text box would invite
 * an answer that gets refused. Nothing here parses: what is typed is sent
 * verbatim and the server decides what it means.
 *
 * # "NOT SURE" IS AN ANSWER AND SKIPPING IS A DIFFERENT ONE
 *
 * "We do not know whether we keep a record of processing activities" is a
 * finding in itself, so it is one of the three buttons. Skipping is beside
 * them, and it records nothing at all: the fact stays absent rather than being
 * guessed, which is what makes the profile checkable.
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
  confirm,
}: {
  slug: string
  state: OnboardingState
  answer: Action
  confirm: Action
}) {
  return (
    <div>
      <Transcript state={state} />

      {state.nextQuestion ? (
        <QuestionForm slug={slug} state={state} answer={answer} />
      ) : (
        <Review slug={slug} state={state} confirm={confirm} />
      )}
    </div>
  )
}

function Transcript({ state }: { state: OnboardingState }) {
  const turns = state.transcript ?? []
  if (turns.length === 0) return null

  return (
    <ol className="space-y-4" aria-label="Onboarding conversation">
      {turns.map((turn) => (
        <li
          key={turn.id}
          className={
            turn.role === 'user'
              ? 'ml-auto max-w-lg rounded-lg bg-muted px-4 py-3'
              : 'max-w-xl'
          }
        >
          <p
            className={
              turn.role === 'user'
                ? 'text-sm text-foreground'
                : 'text-sm text-muted-foreground'
            }
          >
            {turn.content}
          </p>
          {turn.role === 'user' && turn.value ? (
            // What we took the answer to mean, shown against what was typed.
            // This is the check that makes the profile worth trusting, and it
            // is worth showing at the moment of answering rather than only on
            // the review screen: a list that split the wrong way is easiest to
            // fix while the sentence is still on screen.
            <p className="mt-1 text-xs text-muted-foreground">
              Recorded as:{' '}
              {readValue({ value: turn.value } as ProfileFact) ?? 'nothing'}
            </p>
          ) : null}
        </li>
      ))}
    </ol>
  )
}

function QuestionForm({
  slug,
  state,
  answer,
}: {
  slug: string
  state: OnboardingState
  answer: Action
}) {
  const question = state.nextQuestion
  const [result, submit, pending] = useActionState(
    async (previous: ActionState, form: FormData) =>
      answer(slug, previous, form),
    idle,
  )

  if (!question?.key) return null
  const triState = question.shape === 'ANSWER_SHAPE_TRI_STATE'

  return (
    <form
      action={submit}
      // Remounted per question, so the box a person typed into is empty for
      // the next one rather than carrying the previous answer forward.
      key={question.key}
      className="mt-8 border-t border-border pt-6"
    >
      <input type="hidden" name="key" value={question.key} />

      <Label htmlFor="answer" className="text-sm font-medium text-foreground">
        {question.prompt}
      </Label>
      {question.help ? (
        <p className="mt-1 text-xs text-muted-foreground">{question.help}</p>
      ) : null}

      {triState ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {(question.choices ?? []).map((choice) => (
            <Button
              key={choice}
              type="submit"
              name="answer"
              value={choice}
              variant="outline"
              disabled={pending}
            >
              {choice === 'unsure' ? 'Not sure' : choice}
            </Button>
          ))}
        </div>
      ) : (
        <div className="mt-3 flex flex-col gap-2 sm:flex-row">
          {question.shape === 'ANSWER_SHAPE_LIST' ? (
            <Textarea
              id="answer"
              name="answer"
              rows={2}
              className="flex-1"
              placeholder="Separate them with commas"
              disabled={pending}
            />
          ) : (
            <Input
              id="answer"
              name="answer"
              className="flex-1"
              inputMode={
                question.shape === 'ANSWER_SHAPE_NUMBER' ? 'numeric' : 'text'
              }
              disabled={pending}
            />
          )}
          <Button type="submit" disabled={pending}>
            {pending ? 'Recording' : 'Answer'}
          </Button>
        </div>
      )}

      <div className="mt-3 flex items-center gap-4">
        {/* Deliberately quiet and deliberately present. A person who cannot
            answer has to have somewhere to go that is not a wrong answer, and
            a skipped question leaves the fact absent rather than guessed. */}
        <Button
          type="submit"
          name="skip"
          value="true"
          variant="ghost"
          size="sm"
          disabled={pending}
        >
          I would rather not say
        </Button>
        <p className="text-xs text-muted-foreground">
          Question {(state.answeredQuestions ?? 0) + 1} of{' '}
          {state.totalQuestions ?? 0}
        </p>
      </div>

      {result.status === 'error' ? (
        <p className="mt-3 text-sm text-destructive" role="alert">
          {result.message}
        </p>
      ) : null}
    </form>
  )
}

function Review({
  slug,
  state,
  confirm,
}: {
  slug: string
  state: OnboardingState
  confirm: Action
}) {
  const [result, submit, pending] = useActionState(
    async (previous: ActionState, form: FormData) =>
      confirm(slug, previous, form),
    idle,
  )
  const draft = state.draft ?? []

  return (
    <section className="mt-8 border-t border-border pt-6">
      <h2 className="text-sm font-medium text-foreground">
        Before we record any of this
      </h2>
      <p className="mt-1 max-w-xl text-sm text-muted-foreground">
        Nothing below has been saved yet, and nothing is checking your
        compliance until you say this is right. Each line shows what you said
        and what we took it to mean.
      </p>

      <dl className="mt-5 divide-y divide-border border-y border-border">
        {draft.map((fact) => (
          <DraftRow key={fact.key} fact={fact} />
        ))}
      </dl>

      {draft.length < (state.totalQuestions ?? 0) ? (
        // Said plainly rather than hidden. A skipped question is a blank in the
        // profile, and a blank changes which obligations the Watcher decides
        // apply, so somebody should know they left one.
        <p className="mt-4 text-xs text-muted-foreground">
          {(state.totalQuestions ?? 0) - draft.length} question(s) were skipped
          and will be left blank. You can fill them in later on the memory page.
        </p>
      ) : null}

      <form action={submit} className="mt-6">
        <Button type="submit" disabled={pending}>
          {pending ? 'Recording' : 'Yes, this is right'}
        </Button>
      </form>

      {result.status === 'error' ? (
        <p className="mt-3 text-sm text-destructive" role="alert">
          {result.message}
        </p>
      ) : null}
    </section>
  )
}

function DraftRow({ fact }: { fact: DraftFact }) {
  const label = fact.key ? FACT_LABELS[fact.key] : ''
  const value = readValue({ key: fact.key, value: fact.value } as ProfileFact)

  return (
    <div className="grid gap-1 py-3 sm:grid-cols-[14rem_1fr]">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd>
        <p className="text-sm text-foreground">{value ?? 'Not recorded'}</p>
        {fact.answer && fact.answer !== value ? (
          <p className="mt-0.5 text-xs text-muted-foreground">
            You said: {fact.answer}
          </p>
        ) : null}
      </dd>
    </div>
  )
}
