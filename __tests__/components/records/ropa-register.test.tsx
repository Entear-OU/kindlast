import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RopaRegister } from '@/components/records/ropa-register'
import type { ProcessingActivity } from '@/lib/records/ropa'

/**
 * ENT-70 — RTL coverage for the ROPA register table.
 *
 *   * Renders every activity with its fields and derived status pill.
 *   * Empty state explains how the ROPA gets populated (approve a finding).
 *   * Inline edit submits through the editActivity action with parsed values.
 *   * Manual "Add activity" submits through addActivity, and is capped on the
 *     Free tier at 3 manual rows.
 */

const { addActivityMock, editActivityMock, refreshMock } = vi.hoisted(() => ({
  addActivityMock: vi.fn(),
  editActivityMock: vi.fn(),
  refreshMock: vi.fn(),
}))

vi.mock('@/app/(authed)/records/ropa/actions', () => ({
  addActivity: addActivityMock,
  editActivity: editActivityMock,
}))

vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: refreshMock }),
}))

function activity(over: Partial<ProcessingActivity> = {}): ProcessingActivity {
  return {
    id: 'a1',
    name: 'Email marketing',
    purpose: 'Send newsletters',
    legal_basis: 'consent',
    data_categories: ['email', 'name'],
    recipients: ['Mailchimp'],
    retention_period: '24 months',
    finding_id: null,
    created_at: '2026-05-01T10:00:00.000Z',
    updated_at: '2026-05-02T10:00:00.000Z',
    ...over,
  }
}

beforeEach(() => {
  addActivityMock.mockReset().mockResolvedValue({ ok: true })
  editActivityMock.mockReset().mockResolvedValue({ ok: true })
  refreshMock.mockReset()
})

describe('RopaRegister (ENT-70)', () => {
  it('renders an empty state explaining how the ROPA fills up', () => {
    render(<RopaRegister activities={[]} />)
    expect(screen.getByText(/no processing activities yet/i)).toBeInTheDocument()
    expect(screen.getByText(/approve findings/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /add activity/i })).toBeEnabled()
  })

  it('renders each activity with its fields and a status pill', () => {
    render(
      <RopaRegister
        activities={[
          activity(),
          activity({
            id: 'a2',
            name: 'Analytics',
            legal_basis: 'legitimate interest',
            data_categories: ['device'],
            retention_period: null, // missing mandatory field → incomplete
          }),
        ]}
      />,
    )
    expect(screen.getByText('Email marketing')).toBeInTheDocument()
    expect(screen.getByText('consent')).toBeInTheDocument()
    expect(screen.getByText('email, name')).toBeInTheDocument()
    // Derived statuses surface as pills.
    expect(screen.getByText('Complete')).toBeInTheDocument()
    expect(screen.getByText('Incomplete')).toBeInTheDocument()
  })

  it('submits an inline edit through editActivity with parsed values', async () => {
    const user = userEvent.setup()
    render(<RopaRegister activities={[activity()]} />)

    await user.click(screen.getByRole('button', { name: /edit/i }))

    const recipients = screen.getByLabelText('Recipients')
    await user.clear(recipients)
    await user.type(recipients, 'AWS, Stripe')
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    expect(editActivityMock).toHaveBeenCalledTimes(1)
    const [id, input] = editActivityMock.mock.calls[0]
    expect(id).toBe('a1')
    expect(input.recipients).toEqual(['AWS', 'Stripe'])
    expect(input.name).toBe('Email marketing')
    expect(refreshMock).toHaveBeenCalled()
  })

  it('adds a manual activity through addActivity', async () => {
    const user = userEvent.setup()
    render(<RopaRegister activities={[activity()]} />)

    await user.click(screen.getByRole('button', { name: /add activity/i }))
    await user.type(screen.getByLabelText('Activity name'), 'Payroll')
    await user.type(screen.getByLabelText('Data categories'), 'salary, bank')
    await user.click(screen.getByRole('button', { name: /add activity/i }))

    expect(addActivityMock).toHaveBeenCalledTimes(1)
    const [input] = addActivityMock.mock.calls[0]
    expect(input.name).toBe('Payroll')
    expect(input.data_categories).toEqual(['salary', 'bank'])
    expect(refreshMock).toHaveBeenCalled()
  })

  it('caps manual adds on the Free tier at 3 and explains why', () => {
    const manual = [
      activity({ id: 'm1', finding_id: null }),
      activity({ id: 'm2', finding_id: null }),
      activity({ id: 'm3', finding_id: null }),
      activity({ id: 'x1', finding_id: 'f1' }), // Executor row — doesn't count
    ]
    render(<RopaRegister activities={manual} />)
    expect(screen.getByRole('button', { name: /add activity/i })).toBeDisabled()
    expect(screen.getByText(/3 of 3 manual activities used/i)).toBeInTheDocument()
  })

  it('surfaces an action error without closing the form', async () => {
    const user = userEvent.setup()
    addActivityMock.mockResolvedValue({ ok: false, error: 'free tier limit reached' })
    render(<RopaRegister activities={[activity()]} />)

    await user.click(screen.getByRole('button', { name: /add activity/i }))
    await user.type(screen.getByLabelText('Activity name'), 'Another')
    await user.click(screen.getByRole('button', { name: /add activity/i }))

    expect(await screen.findByText(/free tier limit reached/i)).toBeInTheDocument()
    // The form stays open (Activity name input still present).
    expect(screen.getByLabelText('Activity name')).toBeInTheDocument()
    expect(refreshMock).not.toHaveBeenCalled()
  })
})
