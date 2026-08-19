'use client'

import { useState } from 'react'

import { citationLabel, obligationBySlug } from '@/lib/readiness/corpus'
import { QUOTE_PROVENANCE } from '@/lib/readiness/copy'
import {
  NONE,
  UNSURE,
  type Answer,
  type Question,
  type TriState,
} from '@/lib/readiness/script'

/**
 * One question (ENT-189).
 *
 * # EVERY ANSWER IS A TAP, AND THERE IS NOTHING TO TYPE
 *
 * That is a security decision wearing a usability one. A text box on an
 * unauthenticated page is an input somebody eventually feeds to a model, and
 * AGENTS.md's rule that user input is data rather than instruction is easiest
 * to keep when there is no input channel at all. It is also faster: thirteen
 * taps beat thirteen sentences, and a closed set of tokens is the only kind of
 * answer the applicability rules can read anyway.
 *
 * # "NOT SURE" IS A BUTTON BECAUSE IT IS AN ANSWER
 *
 * ENT-228 kept `unsure` as its own value because "we asked and they did not
 * know" is a different claim from "they said no", and the applicability rules
 * read the difference: an organisation that does not know whether its
 * processing is high-risk has not done the screening. Hiding it would push
 * people towards a "no" that changes the result.
 *
 * # "WHY WE ASK" QUOTES THE CORPUS AND SAYS NOTHING OF ITS OWN
 *
 * A visitor who wants to know why a question matters is asking about the law,
 * and ENT-248 says the statement of law comes from a corpus row. So the
 * disclosure renders `obligation.summary` verbatim with its citation, and this
 * component writes not one word about what any of it requires.
 */

const INK = '#0D1B2A'
const TEAL = '#00C9A7'

const TRI_LABELS: Array<{ value: TriState; label: string }> = [
  { value: 'yes', label: 'Yes' },
  { value: 'no', label: 'No' },
  { value: 'unsure', label: 'Not sure' },
]

const choiceClasses =
  'cursor-pointer rounded-full border px-6 py-3 text-[15px] font-semibold ' +
  'tracking-[-0.01em] transition-all duration-150 active:scale-[0.97] ' +
  'focus-visible:outline-2 focus-visible:outline-offset-2 ' +
  'focus-visible:outline-[#00C9A7] motion-reduce:transition-none ' +
  'motion-reduce:active:scale-100'

export function QuestionCard({
  question,
  step,
  total,
  onAnswer,
  onBack,
  canGoBack,
}: {
  question: Question
  step: number
  total: number
  onAnswer: (key: string, answer: Answer) => void
  onBack: () => void
  canGoBack: boolean
}) {
  const [picked, setPicked] = useState<readonly string[]>([])
  const [showBasis, setShowBasis] = useState(false)
  const basis = question.basis ? obligationBySlug(question.basis) : undefined

  function toggle(value: string, exclusive: boolean) {
    setPicked((current) => {
      if (exclusive) return current.includes(value) ? [] : [value]
      // Picking a real answer clears "none of this" and "I could not say",
      // because they are complete answers and this one contradicts them.
      const kept = current.filter((v) => v !== NONE && v !== UNSURE)
      return kept.includes(value)
        ? kept.filter((v) => v !== value)
        : [...kept, value]
    })
  }

  return (
    // Keyed by the caller so React remounts on every question: the chips a
    // person tapped for the last one must not be sitting there for the next.
    <div className="signal-fade">
      <div className="flex items-baseline justify-between gap-4">
        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.2em]"
          style={{ color: 'rgba(13,27,42,0.35)' }}
        >
          Question {step} of {total}
        </p>
        {canGoBack ? (
          <button
            type="button"
            onClick={onBack}
            className="cursor-pointer font-mono text-[11px] font-medium uppercase tracking-[0.14em] underline underline-offset-4 transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7]"
            style={{ color: 'rgba(13,27,42,0.4)' }}
          >
            Change the last answer
          </button>
        ) : null}
      </div>

      <h2
        className="mt-6 max-w-[22ch] text-[1.75rem] font-black leading-[1.06] tracking-[-0.035em] text-balance sm:text-[2.375rem]"
        style={{ color: INK }}
      >
        {question.prompt}
      </h2>

      {question.help ? (
        <p
          className="mt-4 max-w-[46ch] text-[1rem] font-medium leading-[1.7] tracking-[-0.005em]"
          style={{ color: 'rgba(13,27,42,0.5)' }}
        >
          {question.help}
        </p>
      ) : null}

      {question.kind === 'tri' ? (
        <div className="mt-9 flex flex-wrap gap-3">
          {TRI_LABELS.map((choice) => (
            <button
              key={choice.value}
              type="button"
              onClick={() => onAnswer(question.key, choice.value)}
              className={`${choiceClasses} hover:bg-[#0D1B2A] hover:text-white`}
              style={{ borderColor: 'rgba(13,27,42,0.18)', color: INK }}
            >
              {choice.label}
            </button>
          ))}
        </div>
      ) : (
        <>
          <div className="mt-9 flex flex-wrap gap-2.5" role="group">
            {question.options.map((option) => {
              const on = picked.includes(option.value)
              return (
                <button
                  key={option.value}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    toggle(option.value, option.exclusive ?? false)
                  }
                  className={choiceClasses}
                  style={
                    on
                      ? {
                          backgroundColor: INK,
                          borderColor: INK,
                          color: '#fff',
                        }
                      : { borderColor: 'rgba(13,27,42,0.18)', color: INK }
                  }
                >
                  {option.label}
                </button>
              )
            })}
          </div>

          <div className="mt-7">
            <button
              type="button"
              disabled={picked.length === 0}
              onClick={() => onAnswer(question.key, picked)}
              className="cursor-pointer rounded-full px-8 py-3.5 text-[16px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7] disabled:cursor-not-allowed disabled:opacity-30 motion-reduce:transition-none motion-reduce:active:scale-100"
              style={{ backgroundColor: INK }}
            >
              Continue
            </button>
            {picked.length === 0 ? (
              <p
                className="mt-3 text-[13px] font-medium"
                style={{ color: 'rgba(13,27,42,0.4)' }}
              >
                Pick at least one. Every option here is an answer, including the
                last two.
              </p>
            ) : null}
          </div>
        </>
      )}

      {basis ? (
        <div
          className="mt-11 border-t pt-6"
          style={{ borderColor: 'rgba(13,27,42,0.1)' }}
        >
          <button
            type="button"
            aria-expanded={showBasis}
            data-citation="true"
            onClick={() => setShowBasis((v) => !v)}
            className="cursor-pointer font-mono text-[11px] font-medium uppercase tracking-[0.16em] transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7]"
            style={{ color: 'rgba(13,27,42,0.45)' }}
          >
            {showBasis ? '−' : '+'} Why we ask &middot;{' '}
            {citationLabel(basis.citation)}
          </button>

          {showBasis ? (
            <blockquote
              data-corpus="true"
              className="signal-fade mt-4 max-w-[62ch] border-l-2 pl-5"
              style={{ borderColor: TEAL }}
            >
              <p
                className="text-[0.9375rem] font-medium leading-[1.72] tracking-[-0.005em]"
                style={{ color: 'rgba(13,27,42,0.72)' }}
              >
                {basis.summary}
              </p>
              <footer
                className="mt-3 font-mono text-[10px] font-medium uppercase tracking-[0.14em]"
                style={{ color: 'rgba(13,27,42,0.38)' }}
              >
                {QUOTE_PROVENANCE}
              </footer>
            </blockquote>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
