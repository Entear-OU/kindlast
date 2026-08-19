'use client'

import { useEffect, useMemo, useRef, useState } from 'react'

import { Ledger, LedgerSummary } from '@/components/readiness/ledger'
import { QuestionCard } from '@/components/readiness/question-card'
import { Summary } from '@/components/readiness/summary'
import { NO_TRANSMISSION } from '@/lib/readiness/copy'
import { assess, ledger } from '@/lib/readiness/evaluate'
import {
  applicableQuestions,
  nextQuestion,
  type Answer,
  type Answers,
} from '@/lib/readiness/script'

/**
 * The readiness assessment (ENT-189).
 *
 * # THE ANSWERS LIVE IN A REACT STATE HOOK AND NOWHERE ELSE
 *
 * Not a cookie, not `localStorage`, not `sessionStorage`, not a query string,
 * not a server action, not a `fetch`. `__tests__/components/readiness/`
 * asserts that by driving the whole interview with every one of those stubbed
 * to throw, which is the only version of a no-persistence guarantee worth
 * having: a comment saying "we do not store this" is not checkable and a
 * spied-on global is.
 *
 * The cost is real and was chosen. A refresh loses the interview, and thirteen
 * questions is enough for that to sting. `sessionStorage` would fix it and
 * would turn "your answers never leave this page" into a sentence with a
 * footnote, on the page of a company whose product is that you can check its
 * claims. The unqualified sentence is worth more than the refresh.
 *
 * # WHY THERE IS NO SERVER IN THIS AT ALL
 *
 * ENT-189 raised an unauthenticated model endpoint as risk 1 and asked for
 * per-IP rate limiting, a bot check, a turn cap and a spend alert. None of them
 * is here, because none of them has anything to protect: the corpus is bundled
 * at build time, the applicability rules are a pure function, and a visitor
 * tapping buttons for an hour costs exactly one static page. There is no
 * endpoint to rate limit, no credential to spend and no prompt to inject.
 *
 * That is a stronger answer than the four controls the issue asked for, and it
 * is only available because the assessment is scripted. It is the same argument
 * ENT-212 made for the onboarding interview, arriving at the same shape from
 * the other direction.
 */
export function Assessment() {
  const [answers, setAnswers] = useState<Answers>({})
  const [done, setDone] = useState(false)
  const resultRef = useRef<HTMLDivElement>(null)

  // The interview and the result occupy the same place in the page, so
  // finishing leaves the reader wherever the last question happened to sit,
  // with the hero above them and a ten-thousand-pixel result below. Put the
  // top of the result where the question was.
  //
  // `scrollIntoView` is guarded because jsdom does not implement it, and a
  // component test that has to stub a browser API to render is a component
  // that will surprise somebody later.
  useEffect(() => {
    if (!done) return
    resultRef.current?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
  }, [done])

  const rows = useMemo(() => ledger(answers), [answers])
  const question = useMemo(() => nextQuestion(answers), [answers])
  const asked = useMemo(() => applicableQuestions(answers), [answers])
  const step = asked.findIndex((q) => q.key === question?.key) + 1

  function record(key: string, answer: Answer) {
    const next: Answers = { ...answers, [key]: answer }
    setAnswers(next)
    // Ask the next question against the sheet we just built, not the one in
    // state: a branch that closes on this very answer (naming no transfers
    // closes the destination question) has to be gone before we look.
    if (!nextQuestion(next)) setDone(true)
  }

  function back() {
    // The last thing answered, in script order, so "change the last answer"
    // means what it says even after a branch reordered what is on screen.
    const answered = asked.filter((q) => answers[q.key] !== undefined)
    const last = answered[answered.length - 1]
    if (!last) return
    const next = { ...answers }
    delete next[last.key]
    setAnswers(next)
    setDone(false)
  }

  function restart() {
    setAnswers({})
    setDone(false)
  }

  if (done) {
    return (
      <div ref={resultRef} className="mx-auto max-w-5xl px-6 lg:px-8">
        <Summary assessment={assess(answers)} onRestart={restart} />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-5xl px-6 lg:px-8">
      <div className="grid gap-14 lg:grid-cols-[1fr_19rem] lg:gap-16">
        <div>
          {/* On a phone the column below is a scroll away, so the counts lead
              here instead: a visitor should see the corpus narrowing while
              they answer, not discover it afterwards. */}
          <LedgerSummary rows={rows} className="mb-7 lg:hidden" />

          {question ? (
            <QuestionCard
              // Remounted per question, so a multi-select never opens with the
              // chips from the last one still pressed.
              key={question.key}
              question={question}
              step={step}
              total={asked.length}
              onAnswer={record}
              onBack={back}
              canGoBack={Object.keys(answers).length > 0}
            />
          ) : null}

          <p
            className="mt-14 max-w-[52ch] text-[13px] font-medium leading-[1.65]"
            style={{ color: 'rgba(13,27,42,0.38)' }}
          >
            {NO_TRANSMISSION}
          </p>
        </div>

        <Ledger rows={rows} />
      </div>
    </div>
  )
}
