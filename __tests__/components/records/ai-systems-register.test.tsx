import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AiSystemsRegister } from '@/components/records/ai-systems-register'
import type { AiSystem } from '@/lib/records/ai-system'

/**
 * ENT-72 — RTL coverage for the AI Systems Register.
 *
 *   * Renders systems with risk + documentation pills and "last reviewed".
 *   * Empty state mentions shadow AI.
 *   * Editing a non-classification field saves directly (reviewed=false).
 *   * Changing the classification is a two-step reviewed approval (reviewed=true).
 *   * Manual "Add system" with a High-risk class also requires the reviewed step.
 */

const { addSystemMock, editSystemMock, refreshMock } = vi.hoisted(() => ({
  addSystemMock: vi.fn(),
  editSystemMock: vi.fn(),
  refreshMock: vi.fn(),
}))

vi.mock('@/app/(authed)/records/ai-systems/actions', () => ({
  addSystem: addSystemMock,
  editSystem: editSystemMock,
}))

vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: refreshMock }),
}))

function system(over: Partial<AiSystem> = {}): AiSystem {
  return {
    id: 's1',
    name: 'Resume Screener',
    vendor: 'Acme',
    purpose: 'Rank applicants',
    risk_classification: 'limited',
    documentation_status: 'in_progress',
    last_reviewed_at: '2026-05-10T10:00:00.000Z',
    finding_id: null,
    created_at: '2026-05-10T10:00:00.000Z',
    updated_at: '2026-05-10T10:00:00.000Z',
    ...over,
  }
}

beforeEach(() => {
  addSystemMock.mockReset().mockResolvedValue({ ok: true })
  editSystemMock.mockReset().mockResolvedValue({ ok: true })
  refreshMock.mockReset()
})

describe('AiSystemsRegister (ENT-72)', () => {
  it('renders an empty state mentioning shadow AI', () => {
    render(<AiSystemsRegister systems={[]} />)
    expect(screen.getByText(/no ai systems registered yet/i)).toBeInTheDocument()
    expect(screen.getByText(/shadow ai/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /add system/i })).toBeEnabled()
  })

  it('renders systems with risk and documentation pills', () => {
    render(
      <AiSystemsRegister
        systems={[
          system(),
          system({
            id: 's2',
            name: 'Credit Scorer',
            risk_classification: 'high',
            documentation_status: 'complete',
          }),
        ]}
      />,
    )
    expect(screen.getByText('Resume Screener')).toBeInTheDocument()
    expect(screen.getByText('Limited')).toBeInTheDocument()
    expect(screen.getByText('High risk')).toBeInTheDocument()
    expect(screen.getByText('In progress')).toBeInTheDocument()
  })

  it('saves a non-classification edit directly (no reviewed approval)', async () => {
    const user = userEvent.setup()
    render(<AiSystemsRegister systems={[system()]} />)

    await user.click(screen.getByRole('button', { name: /edit/i }))
    const vendor = screen.getByLabelText('Vendor')
    await user.clear(vendor)
    await user.type(vendor, 'Acme Corp')
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    expect(editSystemMock).toHaveBeenCalledTimes(1)
    const [id, input, reviewed] = editSystemMock.mock.calls[0]
    expect(id).toBe('s1')
    expect(input.vendor).toBe('Acme Corp')
    expect(reviewed).toBe(false)
    expect(refreshMock).toHaveBeenCalled()
  })

  it('requires a reviewed approval to change the classification', async () => {
    const user = userEvent.setup()
    render(<AiSystemsRegister systems={[system()]} />)

    await user.click(screen.getByRole('button', { name: /edit/i }))
    await user.selectOptions(screen.getByLabelText('Risk classification'), 'high')

    // First save reveals the reviewed-approval confirmation; no write yet.
    await user.click(screen.getByRole('button', { name: /^save$/i }))
    expect(editSystemMock).not.toHaveBeenCalled()
    expect(screen.getByText(/recorded as a reviewed approval/i)).toBeInTheDocument()

    // Confirming performs the reclassification with reviewed=true.
    await user.click(screen.getByRole('button', { name: /confirm reviewed approval/i }))
    expect(editSystemMock).toHaveBeenCalledTimes(1)
    const [, input, reviewed] = editSystemMock.mock.calls[0]
    expect(input.risk_classification).toBe('high')
    expect(reviewed).toBe(true)
    expect(refreshMock).toHaveBeenCalled()
  })

  it('requires a reviewed approval to add a High-risk system', async () => {
    const user = userEvent.setup()
    render(<AiSystemsRegister systems={[system()]} />)

    await user.click(screen.getByRole('button', { name: /add system/i }))
    await user.type(screen.getByLabelText('System name'), 'Biometric ID')
    await user.selectOptions(screen.getByLabelText('Risk classification'), 'high')

    await user.click(screen.getByRole('button', { name: /add system/i }))
    expect(addSystemMock).not.toHaveBeenCalled() // confirmation first

    await user.click(screen.getByRole('button', { name: /confirm reviewed approval/i }))
    expect(addSystemMock).toHaveBeenCalledTimes(1)
    const [input, reviewed] = addSystemMock.mock.calls[0]
    expect(input.name).toBe('Biometric ID')
    expect(input.risk_classification).toBe('high')
    expect(reviewed).toBe(true)
  })

  // ENT-168: an empty submit used to register an "Untitled system".
  it('keeps the add button disabled until the system has a name', async () => {
    const user = userEvent.setup()
    render(<AiSystemsRegister systems={[system()]} />)

    await user.click(screen.getByRole('button', { name: /add system/i }))
    const save = screen.getByRole('button', { name: /add system/i })
    expect(save).toBeDisabled()

    await user.type(screen.getByLabelText('System name'), '  ')
    expect(save).toBeDisabled()

    await user.type(screen.getByLabelText('System name'), 'Chatbot')
    expect(save).toBeEnabled()
  })

  it('adds a non-high system without the reviewed step', async () => {
    const user = userEvent.setup()
    render(<AiSystemsRegister systems={[system()]} />)

    await user.click(screen.getByRole('button', { name: /add system/i }))
    await user.type(screen.getByLabelText('System name'), 'Chatbot')
    await user.selectOptions(screen.getByLabelText('Risk classification'), 'minimal')
    await user.click(screen.getByRole('button', { name: /add system/i }))

    expect(addSystemMock).toHaveBeenCalledTimes(1)
    const [input, reviewed] = addSystemMock.mock.calls[0]
    expect(input.name).toBe('Chatbot')
    expect(reviewed).toBe(false)
  })
})
