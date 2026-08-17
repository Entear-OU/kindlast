import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ActivityForm, AddDisclosure } from '@/components/records/activity-form'
import { EditableRopa, RespondableDsars } from '@/components/records/editable'
import { RopaTable } from '@/components/records/registers'
import { SystemForm } from '@/components/records/system-form'
import { idle, type RecordActionState } from '@/lib/records/action-state'
import type { AiSystem, Dsar, ProcessingActivity } from '@/lib/records/client'

const ok: RecordActionState = { status: 'ok', message: 'Saved.' }

/** Records what the action was handed, so the tests can assert the payload. */
function spyAction() {
  const calls: FormData[] = []
  const action = vi.fn(
    async (_slug: string, _previous: RecordActionState, form: FormData) => {
      calls.push(form)
      return ok
    },
  )
  return { action, calls }
}

describe('the Article 30 form', () => {
  // The contract is a full replacement, so an edit form that did not carry the
  // current values would wipe every field somebody did not retype. This is the
  // assertion that stops that: the form must post what is on the record.
  it('carries every current value when editing, so saving a rename keeps the rest', async () => {
    const user = userEvent.setup()
    const { action, calls } = spyAction()

    const activity: ProcessingActivity = {
      processingActivityId: 'p-1',
      name: 'Payrol',
      purpose: 'Paying staff',
      legalBasis: 'Article 6(1)(b)',
      dataCategories: ['name', 'bank details'],
      recipients: ['our accountant'],
      retentionPeriod: '7 years',
    }

    render(<ActivityForm slug="acme" action={action} activity={activity} />)

    // Fix the typo in the name and save, touching nothing else.
    const name = screen.getByLabelText('Activity')
    await user.clear(name)
    await user.type(name, 'Payroll')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(calls).toHaveLength(1))

    const sent = calls[0]
    expect(sent.get('processingActivityId')).toBe('p-1')
    expect(sent.get('name')).toBe('Payroll')
    expect(sent.get('legalBasis')).toBe('Article 6(1)(b)')
    expect(sent.get('dataCategories')).toBe('name, bank details')
    expect(sent.get('recipients')).toBe('our accountant')
    expect(sent.get('retentionPeriod')).toBe('7 years')
  })

  it('posts no record id when adding', async () => {
    const user = userEvent.setup()
    const { action, calls } = spyAction()

    render(<ActivityForm slug="acme" action={action} />)

    await user.type(screen.getByLabelText('Activity'), 'Payroll')
    await user.click(screen.getByRole('button', { name: 'Add activity' }))

    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0].get('processingActivityId')).toBeNull()
  })
})

describe('the AI system form', () => {
  const system: AiSystem = {
    aiSystemId: 'a-1',
    name: 'CV ranking model',
    riskClassification: 'high',
    documentationStatus: 'missing',
  }

  // The gate exists because moving a system out of `high` retires Articles 9 to
  // 17 for it. It has to be visible BEFORE submitting, not as a sentence after
  // a refusal.
  it('asks for a reviewed approval as soon as the classification changes', async () => {
    const user = userEvent.setup()
    const { action } = spyAction()

    render(<SystemForm slug="acme" action={action} system={system} />)

    expect(screen.queryByText(/reviewed approval/i)).not.toBeInTheDocument()

    await user.selectOptions(
      screen.getByLabelText('Risk classification'),
      'minimal',
    )

    expect(screen.getByText(/reviewed approval/i)).toBeInTheDocument()

    // The whole sentence, not the two labels separately. Matching /High risk/
    // alone finds the <option> in the select as well as the description, which
    // is how the first version of this test failed: ambiguous rather than
    // wrong. Asserting the sentence also pins that it names BOTH ends of the
    // change, because "confirm this" with no subject is a dialog people learn
    // to click through.
    expect(
      screen.getByText(
        'This changes the classification from High risk to Minimal, which changes which AI Act obligations apply.',
      ),
    ).toBeInTheDocument()
  })

  // A gate that fires on every save is a gate people learn to click through,
  // which is the failure mode a reviewed approval exists to prevent.
  it('does not ask for one when the classification is untouched', async () => {
    const user = userEvent.setup()
    const { action, calls } = spyAction()

    render(<SystemForm slug="acme" action={action} system={system} />)

    const name = screen.getByLabelText('System')
    await user.clear(name)
    await user.type(name, 'CV ranking model v2')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(calls).toHaveLength(1))
    expect(screen.queryByText(/reviewed approval/i)).not.toBeInTheDocument()
  })

  // Adding is the other direction: only `high` switches the obligation stack on.
  it('asks for one when a new system is classified high, and not otherwise', async () => {
    const user = userEvent.setup()
    const { action } = spyAction()

    render(<SystemForm slug="acme" action={action} />)

    expect(screen.queryByText(/reviewed approval/i)).not.toBeInTheDocument()

    await user.selectOptions(
      screen.getByLabelText('Risk classification'),
      'limited',
    )
    expect(screen.queryByText(/reviewed approval/i)).not.toBeInTheDocument()

    await user.selectOptions(
      screen.getByLabelText('Risk classification'),
      'high',
    )
    expect(screen.getByText(/reviewed approval/i)).toBeInTheDocument()
  })

  // The default is the honest one, not the reassuring one.
  it('defaults a new system to unclassified rather than minimal', () => {
    const { action } = spyAction()
    render(<SystemForm slug="acme" action={action} />)

    expect(screen.getByLabelText('Risk classification')).toHaveValue(
      'unclassified',
    )
  })
})

describe('editing from the register', () => {
  const activity: ProcessingActivity = {
    processingActivityId: 'p-1',
    name: 'Payroll',
    legalBasis: 'Article 6(1)(b)',
  }

  it('names the record in each edit button, not just "Edit"', () => {
    const { action } = spyAction()
    render(<EditableRopa slug="acme" items={[activity]} action={action} />)

    // A table of buttons all called "Edit" is a list of identical controls to
    // anyone navigating by them.
    expect(
      screen.getByRole('button', { name: 'Edit Payroll' }),
    ).toBeInTheDocument()
  })

  it('replaces the table with the form, populated, and puts it back', async () => {
    const user = userEvent.setup()
    const { action } = spyAction()

    render(<EditableRopa slug="acme" items={[activity]} action={action} />)

    await user.click(screen.getByRole('button', { name: 'Edit Payroll' }))

    // The form carries the current values, which is what stops a rename from
    // clearing every field the contract replaces.
    expect(screen.getByLabelText('Activity')).toHaveValue('Payroll')
    expect(screen.getByLabelText('Legal basis')).toHaveValue('Article 6(1)(b)')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Back to the register' }),
    )
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  // Read-only is the default, so a server component that renders a table
  // without handing in an action gets no controls rather than dead ones.
  it('shows no edit control when no action is given', () => {
    render(<RopaTable items={[activity]} />)
    expect(screen.queryByRole('button', { name: /^Edit/ })).toBeNull()
  })
})

describe('recording that a response went out', () => {
  const open: Dsar = {
    dsarId: 'd-1',
    requestType: 'access',
    status: 'open',
    urgency: 'due_soon',
    daysUntilDue: 4,
  }

  it('offers the control while a request is unanswered', () => {
    const { action } = spyAction()
    render(<RespondableDsars slug="acme" items={[open]} action={action} />)

    expect(
      screen.getByRole('button', { name: 'Mark responded' }),
    ).toBeInTheDocument()
  })

  // The transition is one way. A second "mark responded" is a control whose
  // only outcome is being told it changed nothing.
  it('offers nothing once a request has been answered', () => {
    const { action } = spyAction()
    render(
      <RespondableDsars
        slug="acme"
        items={[{ ...open, status: 'responded', urgency: 'answered' }]}
        action={action}
      />,
    )

    expect(screen.queryByRole('button', { name: 'Mark responded' })).toBeNull()
  })

  // The gate is the point: this is the assertion a regulator reads as evidence
  // the Article 12(3) deadline was met, so it takes two deliberate steps.
  it('asks for a reviewed approval before recording it', async () => {
    const user = userEvent.setup()
    const { action, calls } = spyAction()

    render(<RespondableDsars slug="acme" items={[open]} action={action} />)

    await user.click(screen.getByRole('button', { name: 'Mark responded' }))

    expect(screen.getByText(/reviewed approval/i)).toBeInTheDocument()
    expect(calls).toHaveLength(0)

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Confirm response sent' }),
    )

    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0].get('dsarId')).toBe('d-1')
    expect(calls[0].get('reviewed')).toBe('on')
  })
})

describe('the add control', () => {
  // A control that silently does nothing is worse than one that says why not.
  it('stays visible and says why when the plan cap is reached', () => {
    render(
      <AddDisclosure
        label="Add activity"
        title="Add a processing activity"
        disabled
        disabledReason="Your plan allows 3 manually added activities."
      >
        {() => <p>the form</p>}
      </AddDisclosure>,
    )

    expect(screen.getByRole('button', { name: 'Add activity' })).toBeDisabled()
    expect(screen.getByText(/plan allows 3/)).toBeInTheDocument()
    expect(screen.queryByText('the form')).not.toBeInTheDocument()
  })

  it('reveals the form when opened and hides it again on cancel', async () => {
    const user = userEvent.setup()

    render(
      <AddDisclosure label="Add activity" title="Add a processing activity">
        {(close) => (
          <button type="button" onClick={close}>
            close me
          </button>
        )}
      </AddDisclosure>,
    )

    await user.click(screen.getByRole('button', { name: 'Add activity' }))
    expect(
      screen.getByRole('heading', { name: 'Add a processing activity' }),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'close me' }))
    expect(
      screen.queryByRole('heading', { name: 'Add a processing activity' }),
    ).not.toBeInTheDocument()
  })
})

describe('what a form says after an attempt', () => {
  // Announced without the focus moving, which is every refusal here.
  it('puts the outcome in a live region', async () => {
    const user = userEvent.setup()
    const action = vi.fn(async () => ({
      status: 'error' as const,
      message: 'Give the activity a name.',
    }))

    render(<ActivityForm slug="acme" action={action} />)

    await user.type(screen.getByLabelText('Activity'), 'x')
    await user.click(screen.getByRole('button', { name: 'Add activity' }))

    await waitFor(() => {
      const message = screen.getByTestId('record-form-message')
      expect(message).toHaveTextContent('Give the activity a name.')
      expect(message).toHaveAttribute('aria-live', 'polite')
    })
  })

  it('starts with nothing to say', () => {
    const { action } = spyAction()
    render(<ActivityForm slug="acme" action={action} />)

    expect(screen.getByTestId('record-form-message')).toHaveTextContent('')
    expect(idle.status).toBe('idle')
  })
})
