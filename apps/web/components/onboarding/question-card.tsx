'use client'

import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { citationLabel, obligationBySlug } from '@/lib/onboarding/corpus'
import { QUOTE_PROVENANCE } from '@/lib/onboarding/copy'
import { NONE, UNSURE } from '@/lib/onboarding/answers'
import type { Question } from '@/lib/onboarding/client'

/**
 * One question (ENT-189, ENT-254).
 *
 * # EVERY ANSWER IS A TAP, AND THERE IS NOTHING TO TYPE
 *
 * The readiness assessment made that choice because a text box on an
 * unauthenticated page is an input somebody eventually feeds to a model. That
 * argument does not transfer, since this surface has a session and no model in
 * it either way. A different one does, and it is stronger: both evaluators that
 * decide which obligations apply match on tokens, so a closed set is the only
 * kind of answer either of them can read. A typed list of processors would be a
 * fact the rules never match, which is an obligation quietly ceasing to apply.
 *
 * It is also faster, which is what the ruling asked for: eleven taps beat
 * eleven sentences.
 *
 * # THE OPTIONS COME FROM THE SERVER, ALWAYS
 *
 * `question.options` is core-api's closed vocabulary, sent so this component
 * can render exactly the tokens `Parse` accepts. A list declared here instead
 * would be a second vocabulary, and the drift between two vocabularies is
 * silent: the console would offer a token the server refuses, or worse, one it
 * accepts and nothing downstream matches.
 *
 * # "NOT SURE" IS A BUTTON BECAUSE IT IS AN ANSWER
 *
 * ENT-228 kept `unsure` as its own value because "we asked and they did not
 * know" is a different claim from "they said no", and the applicability rules
 * read the difference: an organisation that does not know whether its
 * processing is high-risk has not done the screening. Hiding it would push
 * people towards a "no" that changes the result.
 *
 * # AND THERE IS NO BACK BUTTON, WHICH IS A CHANGE FROM `/readiness`
 *
 * That page held the answers in a React hook, so "change the last answer" was
 * deleting a key. Here an answer is a fact the moment it is given, and a back
 * button would have to un-write one, silently, with no history entry saying
 * why. The route that does exist is better: every answer is on the memory page
 * from the moment it is given, correctable, and a correction keeps what the
 * value was before. The line under the interview says so.
 *
 * # "WHY WE ASK" QUOTES THE CORPUS AND SAYS NOTHING OF ITS OWN
 *
 * Somebody who wants to know why a question matters is asking about the law,
 * and ENT-248 says the statement of law comes from a corpus row. So the
 * disclosure renders `obligation.summary` verbatim with its citation, and this
 * component writes not one word about what any of it requires.
 */

const TRI_LABELS: Record<string, string> = {
  yes: 'Yes',
  no: 'No',
  unsure: 'Not sure',
}

export function QuestionCard({
  question,
  step,
  total,
  submit,
  pending,
  error,
}: {
  question: Question
  step: number
  total: number
  submit: (form: FormData) => void
  pending: boolean
  error?: string
}) {
  const [picked, setPicked] = useState<readonly string[]>([])
  const [showBasis, setShowBasis] = useState(false)

  const basis = question.basis ? obligationBySlug(question.basis) : undefined
  const options = question.options ?? []
  const triState = question.shape === 'ANSWER_SHAPE_TRI_STATE'

  function toggle(value: string, exclusive: boolean) {
    setPicked((current) => {
      if (exclusive) return current.includes(value) ? [] : [value]
      // Picking a real answer clears "none of these" and "I could not say",
      // because they are complete answers and this one contradicts them.
      const kept = current.filter((v) => v !== NONE && v !== UNSURE)
      return kept.includes(value)
        ? kept.filter((v) => v !== value)
        : [...kept, value]
    })
  }

  return (
    <form action={submit} className="signal-fade">
      <input type="hidden" name="key" value={question.key ?? ''} />

      <p className="font-mono text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground">
        Question {step} of {total}
      </p>

      <h2 className="mt-5 max-w-[24ch] text-2xl font-semibold leading-[1.12] tracking-[-0.03em] text-balance text-foreground sm:text-[2rem]">
        {question.prompt}
      </h2>

      {question.help ? (
        <p className="mt-3 max-w-[46ch] text-sm leading-[1.7] text-muted-foreground">
          {question.help}
        </p>
      ) : null}

      {triState ? (
        <div className="mt-7 flex flex-wrap gap-3">
          {(question.choices ?? []).map((choice) => (
            <Button
              key={choice}
              type="submit"
              name="answer"
              value={choice}
              variant="outline"
              size="lg"
              className="rounded-full px-7"
              disabled={pending}
            >
              {TRI_LABELS[choice] ?? choice}
            </Button>
          ))}
        </div>
      ) : (
        <>
          {/* The tokens ride in a hidden field, comma separated, because that
              is the shape `AnswerQuestion` takes: a string the server decides
              the meaning of. Nothing here parses. */}
          <input type="hidden" name="answer" value={picked.join(',')} />

          <div className="mt-7 flex flex-wrap gap-2.5" role="group">
            {options.map((option) => {
              const value = option.value ?? ''
              const on = picked.includes(value)
              return (
                <button
                  key={value}
                  type="button"
                  aria-pressed={on}
                  disabled={pending}
                  onClick={() => toggle(value, option.exclusive ?? false)}
                  className={[
                    'cursor-pointer rounded-full border px-5 py-2.5 text-sm font-medium',
                    'transition-all duration-150 active:scale-[0.97]',
                    'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                    'motion-reduce:transition-none motion-reduce:active:scale-100',
                    'disabled:cursor-not-allowed disabled:opacity-50',
                    on
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'border-border bg-card text-foreground hover:bg-muted',
                  ].join(' ')}
                >
                  {option.label ?? value}
                </button>
              )
            })}
          </div>

          <div className="mt-7">
            <Button
              type="submit"
              size="lg"
              className="rounded-full px-8"
              disabled={pending || picked.length === 0}
            >
              {pending ? 'Recording' : 'Continue'}
            </Button>
            {picked.length === 0 ? (
              <p className="mt-3 text-[13px] text-muted-foreground">
                Pick at least one. Every option here is an answer, including the
                last ones.
              </p>
            ) : null}
          </div>
        </>
      )}

      <div className="mt-6">
        {/* Deliberately quiet and deliberately present. Somebody who cannot
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
      </div>

      {error ? (
        <p className="mt-3 text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}

      {basis ? (
        <div className="mt-10 border-t border-border pt-6">
          <button
            type="button"
            aria-expanded={showBasis}
            data-citation="true"
            onClick={() => setShowBasis((v) => !v)}
            className="cursor-pointer font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {showBasis ? '−' : '+'} Why we ask &middot;{' '}
            {citationLabel(basis.citation)}
          </button>

          {showBasis ? (
            <blockquote
              data-corpus="true"
              className="signal-fade mt-4 max-w-[62ch] border-l-2 border-primary pl-5"
            >
              <p className="text-[0.9375rem] leading-[1.72] text-foreground/80">
                {basis.summary}
              </p>
              <footer className="mt-3 font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                {QUOTE_PROVENANCE}
              </footer>
            </blockquote>
          ) : null}
        </div>
      ) : null}
    </form>
  )
}
