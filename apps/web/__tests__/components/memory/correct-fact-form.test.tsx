import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import {
  CorrectFactForm,
  CorrectableFact,
} from '@/components/memory/correct-fact-form'
import { ProfileFactList } from '@/components/memory/profile-fact-list'
import type { ActionState } from '@/lib/org/action-state'
import type { ProfileFact } from '@/lib/memory/client'

/**
 * Correcting a fact (ENT-228).
 *
 * The assertions are about the things that would make the form describe
 * something the product cannot do, or lose an answer somebody meant to give.
 *
 *   * "Not sure" is on the form, because a form offering only yes and no makes
 *     every unsure organisation a "no", which is a different claim.
 *   * An empty list box submits an empty list, because "we operate no AI
 *     systems" is an answer and refusing it leaves somebody unable to say the
 *     one thing that clears an obligation.
 *   * The button says Record, not Save, because the previous answer survives.
 *   * The key is posted and the organisation is not, because the organisation
 *     comes from the slug in the URL and never from a field a browser can edit.
 */

const dpo: ProfileFact = {
  key: 'PROFILE_FACT_KEY_HAS_DPO',
  value: { triState: 'TRI_STATE_UNSURE' },
  source: 'onboarding',
  validFrom: '2026-06-01T10:00:00Z',
}

const aiSystems: ProfileFact = {
  key: 'PROFILE_FACT_KEY_AI_SYSTEMS',
  value: { list: { values: ['support triage'] } },
  source: 'human',
  validFrom: '2026-06-01T10:00:00Z',
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
      return { status: 'ok', message: 'Recorded.' }
    },
  )
  return { action, seen }
}

describe('CorrectFactForm', () => {
  it('offers "Not sure" as an answer', () => {
    const { action } = capturing()
    render(<CorrectFactForm slug="alpha" fact={dpo} action={action} />)

    // Without this option every organisation that does not know is recorded as
    // a no, and "we do not know whether we keep a record of processing
    // activities" is a finding in itself.
    expect(screen.getByRole('option', { name: 'Not sure' })).toBeTruthy()
  })

  it('says Record rather than Save', () => {
    const { action } = capturing()
    render(<CorrectFactForm slug="alpha" fact={dpo} action={action} />)

    // A person told their change was saved reasonably assumes the old one is
    // gone. Here it is not: the correction closes the old value and keeps it.
    expect(screen.getByRole('button', { name: 'Record' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
  })

  it('posts the fact key and no organisation', async () => {
    const user = userEvent.setup()
    const { action, seen } = capturing()
    render(<CorrectFactForm slug="alpha" fact={dpo} action={action} />)

    await user.selectOptions(
      screen.getByLabelText(/data protection officer/i),
      'yes',
    )
    await user.click(screen.getByRole('button', { name: 'Record' }))

    await waitFor(() => expect(seen).toHaveLength(1))
    const form = seen[0]
    expect(form.get('key')).toBe('PROFILE_FACT_KEY_HAS_DPO')

    // THE SECURITY PROPERTY. An organisation id in a hidden field is an id
    // somebody can edit. The action re-resolves the organisation from the slug
    // in the URL against the caller's own memberships, so there is nothing
    // here to tamper with.
    expect(form.get('orgId')).toBeNull()
    expect(form.get('org')).toBeNull()
  })

  it('lets somebody empty a list, because none is an answer', async () => {
    const user = userEvent.setup()
    const { action, seen } = capturing()
    render(<CorrectFactForm slug="alpha" fact={aiSystems} action={action} />)

    await user.clear(screen.getByLabelText(/AI systems in use/i))
    await user.click(screen.getByRole('button', { name: 'Record' }))

    await waitFor(() => expect(seen).toHaveLength(1))
    // Submitted as an empty value, which the action reads as an empty list
    // rather than refusing. "We operate no AI systems" clears an obligation,
    // and a form that would not accept it leaves somebody stuck.
    expect(seen[0].get('value')).toBe('')
  })

  it('shows the current value so somebody edits rather than retypes', () => {
    const { action } = capturing()
    render(<CorrectFactForm slug="alpha" fact={aiSystems} action={action} />)

    const field = screen.getByLabelText(
      /AI systems in use/i,
    ) as HTMLInputElement
    expect(field.value).toBe('support triage')
  })

  it('does not confuse a list holding "None" with an empty one', () => {
    const { action } = capturing()
    render(
      <CorrectFactForm
        slug="alpha"
        fact={{ ...aiSystems, value: { list: { values: ['None'] } } }}
        action={action}
      />,
    )

    // `readValue` renders an empty list as "None" for reading, which is right
    // there and wrong in a field: seeding this box from the rendering would
    // blank a list that genuinely holds one item called None, and editing it
    // would silently empty it.
    const field = screen.getByLabelText(
      /AI systems in use/i,
    ) as HTMLInputElement
    expect(field.value).toBe('None')
  })
})

describe('CorrectableFact', () => {
  it('stays closed until somebody asks to correct', async () => {
    const user = userEvent.setup()
    const { action } = capturing()
    render(<CorrectableFact slug="alpha" fact={dpo} action={action} />)

    // A page of ten open forms is a page that invites submitting ten
    // corrections when somebody meant one.
    expect(screen.queryByRole('button', { name: 'Record' })).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Correct' }))
    expect(screen.getByRole('button', { name: 'Record' })).toBeTruthy()
  })
})

describe('ProfileFactList with no correction offered', () => {
  it('renders without a Correct control', () => {
    // The list is still useful read-only, and a caller with no write to offer
    // should not have to pass a no-op action to get one.
    render(<ProfileFactList facts={[dpo]} slug="alpha" />)
    expect(screen.queryByRole('button', { name: 'Correct' })).toBeNull()
    expect(screen.getByText('Not sure')).toBeTruthy()
  })
})
