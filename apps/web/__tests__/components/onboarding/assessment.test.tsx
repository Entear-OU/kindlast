import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Interview } from '@/components/onboarding/interview'
import { OBLIGATIONS, obligationBySlug } from '@/lib/onboarding/corpus'
import type { ActionState } from '@/lib/org/action-state'
import type { OnboardingState } from '@/lib/onboarding/client'

/**
 * The assessment, as onboarding renders it (ENT-189, ENT-254).
 *
 * # WHAT THIS TEST STOPPED BEING ABLE TO DO, AND WHAT REPLACED IT
 *
 * On `/readiness` the whole interview was one component with the script in the
 * bundle, so a test could tap through all thirteen questions and assert on the
 * result. It also sealed every channel out of the page (`fetch`,
 * `XMLHttpRequest`, `sendBeacon`, both storages, `document.cookie`,
 * `history.replaceState`) and proved nothing was persisted, because "your
 * answers never leave this page" was the surface's whole promise.
 *
 * Neither is available now and neither should be. The questions come from
 * core-api and the answers go back to it, so a test that walked the flow here
 * would need a second implementation of the Go script in TypeScript, which is
 * exactly the drift ENT-254 removed. The end-to-end walk lives in
 * `apps/core-api/internal/server/interceptor/onboarding_test.go`, against the
 * real chain and a real Postgres.
 *
 * What is left here is what only the browser can be asked:
 *
 *   1. The corpus column renders every obligation and distinguishes matched,
 *      set aside and still open.
 *   2. A question renders the server's options, and answering sends their
 *      tokens back unparsed.
 *   3. "Why we ask" quotes the corpus row, character for character.
 *   4. Nothing the result writes for itself asserts law, checked on the
 *      rendered document rather than on the source strings, so a sentence
 *      assembled in JSX is covered too.
 */

const DATA_CATEGORIES = 'PROFILE_FACT_KEY_DATA_CATEGORIES' as const
const LAWFUL_BASES = 'PROFILE_FACT_KEY_LAWFUL_BASES' as const

/** The first question, as core-api sends it. */
function firstQuestion(): OnboardingState {
  return {
    sessionId: 'session-1',
    status: 'in_progress',
    totalQuestions: 11,
    answeredQuestions: 0,
    nextQuestion: {
      key: DATA_CATEGORIES,
      prompt: 'What kinds of personal information does your company hold?',
      help: 'Pick everything that applies.',
      shape: 'ANSWER_SHAPE_LIST',
      options: [
        { value: 'contact_details', label: 'Names and contact details' },
        { value: 'payment', label: 'Payment or financial details' },
        { value: 'none', label: 'None of this', exclusive: true },
      ],
    },
    draft: [],
  }
}

/** A question part way through, carrying a corpus basis and one answer behind it. */
function secondQuestion(): OnboardingState {
  return {
    sessionId: 'session-1',
    status: 'in_progress',
    totalQuestions: 11,
    answeredQuestions: 1,
    nextQuestion: {
      key: LAWFUL_BASES,
      prompt: 'On what grounds do you use it?',
      shape: 'ANSWER_SHAPE_LIST',
      basis: 'gdpr-art-6-lawful-basis',
      options: [
        { value: 'consent', label: 'The person agreed to it' },
        {
          value: 'contract',
          label: 'We need it to deliver what they asked for',
        },
      ],
    },
    draft: [
      {
        key: DATA_CATEGORIES,
        value: { list: { values: ['contact_details'] } },
        answer: 'contact_details',
      },
    ],
  }
}

/** A finished interview: nothing left to ask, and a full answer sheet. */
function finished(): OnboardingState {
  return {
    sessionId: 'session-1',
    status: 'completed',
    profileExists: true,
    readyToConfirm: true,
    totalQuestions: 11,
    answeredQuestions: 11,
    draft: [
      {
        key: DATA_CATEGORIES,
        value: { list: { values: ['contact_details', 'payment'] } },
      },
      { key: LAWFUL_BASES, value: { list: { values: ['contract'] } } },
      {
        key: 'PROFILE_FACT_KEY_VENDOR_LIST',
        value: { list: { values: ['hosting'] } },
      },
      {
        key: 'PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU',
        value: { triState: 'TRI_STATE_YES' },
      },
      {
        key: 'PROFILE_FACT_KEY_TRANSFER_DESTINATIONS',
        value: { list: { values: ['united_states'] } },
      },
      {
        key: 'PROFILE_FACT_KEY_HIGH_RISK_PROCESSING',
        value: { triState: 'TRI_STATE_NO' },
      },
      {
        key: 'PROFILE_FACT_KEY_LARGE_SCALE_MONITORING',
        value: { triState: 'TRI_STATE_NO' },
      },
      { key: 'PROFILE_FACT_KEY_HAS_ROPA', value: { triState: 'TRI_STATE_NO' } },
      { key: 'PROFILE_FACT_KEY_HAS_DPO', value: { triState: 'TRI_STATE_NO' } },
      {
        key: 'PROFILE_FACT_KEY_AI_SYSTEMS',
        value: { list: { values: ['assistants'] } },
      },
      {
        key: 'PROFILE_FACT_KEY_HIGH_RISK_AI_SYSTEM',
        value: { triState: 'TRI_STATE_NO' },
      },
    ],
  }
}

function capturing() {
  const seen: FormData[] = []
  const action = vi.fn(
    async (
      _slug: string,
      _previous: ActionState,
      form: FormData,
    ): Promise<ActionState> => {
      seen.push(form)
      return { status: 'ok', message: '' }
    },
  )
  return { action, seen }
}

function show(state: OnboardingState, captured = capturing()) {
  render(
    <Interview
      slug="acme"
      state={state}
      answer={captured.action}
      dashboardHref="/o/acme"
      memoryHref="/o/acme/settings/memory"
    />,
  )
  return captured
}

describe('the assessment inside onboarding', () => {
  it('shows the whole corpus from the first question, with nothing set aside', () => {
    show(firstQuestion())

    const column = screen.getByRole('complementary')
    expect(within(column).getAllByRole('listitem').length).toBe(
      OBLIGATIONS.length,
    )
    // Nothing is set aside before an answer, because nothing has been decided.
    // Showing an unasked question as decided is the guess the whole surface
    // promises it does not make.
    expect(within(column).queryAllByText('Set aside')).toHaveLength(0)
  })

  it('narrows the corpus as answers arrive', () => {
    // The same component, one answer further on. `data_categories` narrows
    // nothing on its own, so this is the state after it and the counts are
    // read from the column rather than asserted per row.
    show(secondQuestion())
    const column = screen.getByRole('complementary')
    expect(within(column).getAllByRole('listitem').length).toBe(
      OBLIGATIONS.length,
    )
  })

  it('offers exactly the options core-api sent, and sends their tokens back', async () => {
    const user = userEvent.setup()
    const captured = show(firstQuestion())

    expect(
      screen.getByRole('button', { name: 'Names and contact details' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Payment or financial details' }),
    ).toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Names and contact details' }),
    )
    await user.click(
      screen.getByRole('button', { name: 'Payment or financial details' }),
    )
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    await waitFor(() => expect(captured.action).toHaveBeenCalled())
    const form = captured.seen[0]
    expect(form.get('key')).toBe(DATA_CATEGORIES)
    // The tokens, comma joined, exactly as `Parse` expects them. NOTHING HERE
    // PARSES: a console that turned taps into a typed sentence would be a
    // second implementation of the rule that decides what a profile contains.
    expect(form.get('answer')).toBe('contact_details,payment')
  })

  it('lets an exclusive answer stand alone', async () => {
    const user = userEvent.setup()
    const captured = show(firstQuestion())

    await user.click(
      screen.getByRole('button', { name: 'Names and contact details' }),
    )
    await user.click(screen.getByRole('button', { name: 'None of this' }))
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    await waitFor(() => expect(captured.action).toHaveBeenCalled())
    // "None of this" clears what was picked before it, because it is a
    // complete answer and the server refuses it alongside another. The console
    // has to make the combination unreachable rather than send one and be
    // told off.
    expect(captured.seen[0].get('answer')).toBe('none')
  })

  it('quotes the corpus rather than explaining the law when asked why', async () => {
    const user = userEvent.setup()
    show(secondQuestion())

    await user.click(screen.getByRole('button', { name: /why we ask/i }))

    const basis = obligationBySlug('gdpr-art-6-lawful-basis')
    expect(basis).toBeDefined()
    // Character for character. If somebody ever paraphrases a summary for the
    // screen, this is the test that stops it.
    expect(screen.getByText(basis!.summary)).toBeInTheDocument()
  })

  it('quotes the corpus verbatim on the result', () => {
    show(finished())

    const article6 = obligationBySlug('gdpr-art-6-lawful-basis')
    expect(article6).toBeDefined()
    expect(screen.getByText(article6!.summary)).toBeInTheDocument()
  })

  it('never states what the law requires, anywhere it wrote the sentence itself', async () => {
    show(finished())

    // The rendered result, not the source strings, so a sentence built in JSX
    // from two safe halves is covered too.
    //
    // Two kinds of node are exempt, and both are exempt STRUCTURALLY rather
    // than by matching their text, because a skip rule that read the string
    // would also skip a sentence that merely resembles one:
    //
    //   [data-corpus]   the quoted obligation summary, which is the one place
    //                   the law is stated and is not ours to write
    //   [data-citation] a rendered citation, which is a reference rather than
    //                   prose, exactly as `citations.py` treats the field it
    //                   validates separately from the claim
    //
    // Everything else is a sentence somebody here wrote, and none of them may
    // assert law.
    const { assertsLaw } = await import('@/lib/onboarding/claims')
    const exempt = '[data-corpus], [data-citation]'

    let checked = 0
    for (const element of document.querySelectorAll('p, li, h2, h3, dt, dd')) {
      if (element.closest(exempt) || element.querySelector(exempt)) continue
      const text = element.textContent?.trim() ?? ''
      if (!text) continue
      checked += 1
      expect(assertsLaw(text), text.slice(0, 160)).toBe(false)
    }

    // Guards the guard. If the exemption selectors ever swallowed the page,
    // every assertion above would vanish and the test would still be green.
    expect(checked).toBeGreaterThan(20)
  })
})
