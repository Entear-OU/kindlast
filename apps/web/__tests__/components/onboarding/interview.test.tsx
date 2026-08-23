import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { Interview } from '@/components/onboarding/interview'
import type { ActionState } from '@/lib/org/action-state'
import type { OnboardingState } from '@/lib/onboarding/client'

/**
 * The interview's controls (ENT-212, ENT-254).
 *
 * The assertions are about the things that would let a value into a customer's
 * profile that nobody chose, or lose an answer somebody meant to give.
 *
 *   * A tri-state question offers exactly three buttons, because the server
 *     accepts exactly yes, no and unsure, and a free-text box would invite an
 *     answer that gets refused.
 *   * "Not sure" is one of them, for the same reason it is on the correction
 *     form: an organisation that does not know is not an organisation that
 *     said no.
 *   * Skipping posts a skip rather than an empty answer, because a declined
 *     question leaves the fact absent and a blank box is a mistake.
 *   * Nothing here is typed, because both evaluators that decide which
 *     obligations apply match tokens rather than prose.
 *
 * THE REVIEW SCREEN'S TESTS ARE GONE WITH THE REVIEW SCREEN. ENT-212 held
 * answers in the transcript until a confirm button, and four tests here checked
 * that the screen said so and that nothing was recorded before it. ENT-254
 * removed the step: an answer is a fact the moment it is given. What replaced
 * those assertions is in
 * `apps/core-api/internal/server/interceptor/onboarding_test.go`, which reads
 * the memory service after one answer and finds the fact already there.
 */

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

function show(state: OnboardingState, answer: ReturnType<typeof capturing>) {
  render(
    <Interview
      slug="acme"
      state={state}
      answer={answer.action}
      dashboardHref="/o/acme"
      memoryHref="/o/acme/settings/memory"
    />,
  )
}

const triStateQuestion: OnboardingState = {
  sessionId: 'session-1',
  status: 'in_progress',
  totalQuestions: 11,
  answeredQuestions: 8,
  nextQuestion: {
    key: 'PROFILE_FACT_KEY_HAS_DPO',
    prompt: 'Have you appointed a data protection officer?',
    help: 'A named person formally appointed to be responsible for data protection.',
    shape: 'ANSWER_SHAPE_TRI_STATE',
    choices: ['yes', 'no', 'unsure'],
    basis: 'gdpr-art-37-dpo-appointment',
  },
  draft: [],
}

describe('Interview', () => {
  it('offers exactly the three answers the server accepts', () => {
    show(triStateQuestion, capturing())

    expect(screen.getByRole('button', { name: 'Yes' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'No' })).toBeInTheDocument()
    // "Not sure" is an answer rather than a way of declining. Hiding it would
    // push somebody towards a "no" that changes which obligations they see.
    expect(screen.getByRole('button', { name: 'Not sure' })).toBeInTheDocument()
  })

  it('posts the answer against the key it was asked under', async () => {
    const user = userEvent.setup()
    const captured = capturing()
    show(triStateQuestion, captured)

    await user.click(screen.getByRole('button', { name: 'Not sure' }))

    await waitFor(() => expect(captured.action).toHaveBeenCalled())
    const form = captured.seen[0]
    expect(form.get('key')).toBe('PROFILE_FACT_KEY_HAS_DPO')
    expect(form.get('answer')).toBe('unsure')
    expect(form.get('skip')).toBeNull()
  })

  it('records a skip as a skip rather than as an empty answer', async () => {
    const user = userEvent.setup()
    const captured = capturing()
    show(triStateQuestion, captured)

    await user.click(
      screen.getByRole('button', { name: /would rather not say/i }),
    )

    await waitFor(() => expect(captured.action).toHaveBeenCalled())
    // A skip leaves the fact absent on purpose; a blank answer is a mistake to
    // correct. Collapsing them would record a deliberate refusal every time
    // somebody reached for the wrong control.
    expect(captured.seen[0].get('skip')).toBe('true')
  })

  it('offers no free-text box anywhere in the interview', () => {
    const { container } = render(
      <Interview
        slug="acme"
        state={triStateQuestion}
        answer={capturing().action}
        dashboardHref="/o/acme"
        memoryHref="/o/acme/settings/memory"
      />,
    )
    // Every answer is a tap. A box here would take a sentence the closed
    // vocabulary refuses, or worse, one it accepts and nothing matches.
    expect(container.querySelector('input[type="text"]')).toBeNull()
    expect(container.querySelector('textarea')).toBeNull()
  })

  it('shows the refusal core-api wrote, rather than something vaguer', async () => {
    const user = userEvent.setup()
    const action = vi.fn(async (): Promise<ActionState> => ({
      status: 'error',
      message:
        'onboarding: answer that one with yes, no or unsure; unsure is a real answer',
    }))
    render(
      <Interview
        slug="acme"
        state={triStateQuestion}
        answer={action}
        dashboardHref="/o/acme"
        memoryHref="/o/acme/settings/memory"
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Yes' }))

    // The server's sentence is the specific one and is written for a person to
    // read. Replacing it with "something went wrong" loses the only part that
    // helps them answer again.
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        /unsure is a real answer/,
      ),
    )
  })

  it('tells somebody how far through they are without lying about it', () => {
    show(triStateQuestion, capturing())
    // The counts come from the server, which knows which questions a branch
    // has closed. Counting the script in the browser would keep promising a
    // question that will never be asked.
    expect(screen.getByText('Question 9 of 11')).toBeInTheDocument()
  })
})
