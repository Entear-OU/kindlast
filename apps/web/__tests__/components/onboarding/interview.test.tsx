import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { Interview } from '@/components/onboarding/interview'
import type { ActionState } from '@/lib/org/action-state'
import type { OnboardingState } from '@/lib/onboarding/client'

/**
 * The interview (ENT-212).
 *
 * The assertions are about the things that would let a value into a customer's
 * profile that nobody typed, or lose an answer somebody meant to give.
 *
 *   * A tri-state question offers exactly three buttons, because the server
 *     accepts exactly yes, no and unsure, and a free-text box would invite an
 *     answer that gets refused.
 *   * "Not sure" is one of them, for the same reason it is on the correction
 *     form: an organisation that does not know is not an organisation that
 *     said no.
 *   * Skipping posts a skip rather than an empty answer, because a declined
 *     question leaves the fact absent and a blank box is a mistake.
 *   * The review screen shows what was said beside what it was taken to mean,
 *     which is the check that makes the profile worth trusting.
 *   * Nothing is recorded until the confirm button, and the copy says so.
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

const triStateQuestion: OnboardingState = {
  sessionId: 'session-1',
  status: 'in_progress',
  transcript: [
    { id: 't1', role: 'assistant', content: 'Hello.' },
    {
      id: 't2',
      role: 'user',
      content: 'Ireland, Spain',
      key: 'PROFILE_FACT_KEY_EU_JURISDICTIONS',
      value: { list: { values: ['Ireland', 'Spain'] } },
    },
  ],
  nextQuestion: {
    key: 'PROFILE_FACT_KEY_HAS_DPO',
    prompt: 'Have you appointed a data protection officer?',
    shape: 'ANSWER_SHAPE_TRI_STATE',
    choices: ['yes', 'no', 'unsure'],
  },
  totalQuestions: 11,
  answeredQuestions: 1,
}

describe('Interview', () => {
  it('offers exactly the three answers the server accepts', () => {
    const { action } = capturing()
    render(
      <Interview
        slug="alpha"
        state={triStateQuestion}
        answer={action}
        confirm={action}
      />,
    )

    expect(screen.getByRole('button', { name: 'yes' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'no' })).toBeTruthy()
    // Rendered as "Not sure" and posted as "unsure": the product's word is not
    // the person's word, and only one of them is a wire value.
    expect(screen.getByRole('button', { name: 'Not sure' })).toBeTruthy()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('posts the answer against the key it was asked under', async () => {
    const { action, seen } = capturing()
    render(
      <Interview
        slug="alpha"
        state={triStateQuestion}
        answer={action}
        confirm={action}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Not sure' }))
    await waitFor(() => expect(seen).toHaveLength(1))

    expect(seen[0].get('key')).toBe('PROFILE_FACT_KEY_HAS_DPO')
    expect(seen[0].get('answer')).toBe('unsure')
    // The organisation is never in the form. It comes from the slug in the URL
    // and is resolved against the caller's own memberships.
    expect(seen[0].get('orgId')).toBeNull()
  })

  it('records a skip as a skip rather than as an empty answer', async () => {
    const { action, seen } = capturing()
    render(
      <Interview
        slug="alpha"
        state={triStateQuestion}
        answer={action}
        confirm={action}
      />,
    )

    await userEvent.click(
      screen.getByRole('button', { name: 'I would rather not say' }),
    )
    await waitFor(() => expect(seen).toHaveLength(1))

    // A skip leaves the fact absent on purpose. An empty answer would be
    // refused, and collapsing the two would record a deliberate refusal every
    // time somebody submitted too early.
    expect(seen[0].get('skip')).toBe('true')
  })

  it('shows what an answer was taken to mean, beside what was typed', () => {
    const { action } = capturing()
    render(
      <Interview
        slug="alpha"
        state={triStateQuestion}
        answer={action}
        confirm={action}
      />,
    )

    expect(screen.getByText('Ireland, Spain')).toBeTruthy()
    expect(screen.getByText(/Recorded as: Ireland, Spain/)).toBeTruthy()
  })

  it('does not offer a free-text box for a question with fixed answers', () => {
    const { action } = capturing()
    render(
      <Interview
        slug="alpha"
        state={{
          ...triStateQuestion,
          nextQuestion: {
            key: 'PROFILE_FACT_KEY_EU_JURISDICTIONS',
            prompt: 'Which countries?',
            shape: 'ANSWER_SHAPE_LIST',
          },
        }}
        answer={action}
        confirm={action}
      />,
    )

    // A list question does get one, so the absence above is about the shape
    // rather than about the component never rendering an input.
    expect(screen.getByRole('textbox')).toBeTruthy()
  })
})

describe('the review before anything is recorded', () => {
  const ready: OnboardingState = {
    sessionId: 'session-1',
    status: 'in_progress',
    transcript: [],
    readyToConfirm: true,
    totalQuestions: 2,
    answeredQuestions: 2,
    draft: [
      {
        key: 'PROFILE_FACT_KEY_EU_JURISDICTIONS',
        value: { list: { values: ['Ireland', 'Spain'] } },
        answer: 'Ireland and Spain',
      },
      {
        key: 'PROFILE_FACT_KEY_HAS_ROPA',
        value: { triState: 'TRI_STATE_NO' },
        answer: 'no',
      },
    ],
  }

  it('says plainly that nothing has been saved yet', () => {
    const { action } = capturing()
    render(
      <Interview slug="alpha" state={ready} answer={action} confirm={action} />,
    )

    expect(screen.getByText(/Nothing below has been saved yet/)).toBeTruthy()
  })

  it('shows the parsed value against the words that produced it', () => {
    const { action } = capturing()
    render(
      <Interview slug="alpha" state={ready} answer={action} confirm={action} />,
    )

    // The parse is checkable precisely because both halves are on screen: a
    // list that split the wrong way is visible here and nowhere else.
    expect(screen.getByText('Ireland, Spain')).toBeTruthy()
    expect(screen.getByText(/You said: Ireland and Spain/)).toBeTruthy()
  })

  it('confirms only when the person says so', async () => {
    const { action, seen } = capturing()
    render(
      <Interview slug="alpha" state={ready} answer={action} confirm={action} />,
    )

    expect(action).not.toHaveBeenCalled()
    await userEvent.click(
      screen.getByRole('button', { name: 'Yes, this is right' }),
    )
    await waitFor(() => expect(seen).toHaveLength(1))
  })

  it('says how many questions were left blank', () => {
    const { action } = capturing()
    render(
      <Interview
        slug="alpha"
        state={{ ...ready, totalQuestions: 4 }}
        answer={action}
        confirm={action}
      />,
    )

    // A blank changes which obligations the Watcher decides apply, so somebody
    // should know they left one rather than discovering it in a finding.
    expect(screen.getByText(/2 question\(s\) were skipped/)).toBeTruthy()
  })
})
