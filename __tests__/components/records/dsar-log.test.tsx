import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { DsarLog } from '@/components/records/dsar-log'
import type { Dsar } from '@/lib/records/dsar'

/**
 * ENT-71 — RTL coverage for the DSAR Log table.
 *
 *   * Renders every DSAR with its status pill and deadline.
 *   * Empty state explains how the log fills up.
 *   * "Mark as responded" is a two-step (reviewed) confirmation that calls
 *     markResponded only after the founder confirms.
 *   * Without the Pro capability, completion is gated (no mark button).
 *   * Manual "Log a DSAR" submits through logDsar.
 */

const { logDsarMock, markRespondedMock, refreshMock } = vi.hoisted(() => ({
  logDsarMock: vi.fn(),
  markRespondedMock: vi.fn(),
  refreshMock: vi.fn(),
}))

vi.mock('@/app/(authed)/records/dsar/actions', () => ({
  logDsar: logDsarMock,
  markResponded: markRespondedMock,
}))

vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: refreshMock }),
}))

const inDays = (n: number) => new Date(Date.now() + n * 86_400_000).toISOString()

function dsar(over: Partial<Dsar> = {}): Dsar {
  return {
    id: 'd1',
    subject_name: 'Jane Roe',
    request_type: 'access',
    handler: 'Privacy Team',
    status: 'open',
    received_at: inDays(-3),
    response_due_at: inDays(27),
    responded_at: null,
    finding_id: null,
    created_at: inDays(-3),
    updated_at: inDays(-3),
    ...over,
  }
}

beforeEach(() => {
  logDsarMock.mockReset().mockResolvedValue({ ok: true })
  markRespondedMock.mockReset().mockResolvedValue({ ok: true })
  refreshMock.mockReset()
})

describe('DsarLog (ENT-71)', () => {
  it('renders an empty state explaining how the log fills up', () => {
    render(<DsarLog dsars={[]} />)
    expect(screen.getByText(/no data-subject requests yet/i)).toBeInTheDocument()
    expect(screen.getByText(/approve a request finding/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /log a dsar/i })).toBeEnabled()
  })

  it('renders DSARs with their status pill and handler', () => {
    render(
      <DsarLog
        dsars={[
          dsar(),
          dsar({ id: 'd2', handler: 'Legal', status: 'responded', responded_at: inDays(-1) }),
        ]}
      />,
    )
    expect(screen.getByText('Privacy Team')).toBeInTheDocument()
    expect(screen.getByText('Legal')).toBeInTheDocument()
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.getByText('Responded')).toBeInTheDocument()
  })

  it('marks a DSAR responded only after a reviewed confirmation', async () => {
    const user = userEvent.setup()
    render(<DsarLog dsars={[dsar()]} />)

    // First click reveals the reviewed-approval confirmation; no call yet.
    await user.click(screen.getByRole('button', { name: /mark as responded/i }))
    expect(markRespondedMock).not.toHaveBeenCalled()
    expect(screen.getByText(/confirm reviewed\?/i)).toBeInTheDocument()

    // Confirming performs the Executor write.
    await user.click(screen.getByRole('button', { name: /^confirm$/i }))
    expect(markRespondedMock).toHaveBeenCalledWith('d1')
    expect(refreshMock).toHaveBeenCalled()
  })

  it('gates completion behind Pro when the capability is absent', () => {
    render(<DsarLog dsars={[dsar()]} canComplete={false} />)
    expect(screen.queryByRole('button', { name: /mark as responded/i })).not.toBeInTheDocument()
    expect(screen.getByText('Pro')).toBeInTheDocument()
  })

  it('logs a DSAR through logDsar', async () => {
    const user = userEvent.setup()
    render(<DsarLog dsars={[dsar()]} />)

    await user.click(screen.getByRole('button', { name: /log a dsar/i }))
    await user.type(screen.getByLabelText('Requester'), 'John Doe')
    await user.type(screen.getByLabelText('Request type'), 'erasure')
    await user.click(screen.getByRole('button', { name: /log dsar/i }))

    expect(logDsarMock).toHaveBeenCalledTimes(1)
    expect(logDsarMock.mock.calls[0][0]).toMatchObject({
      subject_name: 'John Doe',
      request_type: 'erasure',
    })
    expect(refreshMock).toHaveBeenCalled()
  })

  // ENT-168: an empty submit used to log a requester-less DSAR and start a live
  // 30-day Article 12(3) countdown for a request that does not exist.
  it('keeps the log button disabled until the request names a requester', async () => {
    const user = userEvent.setup()
    render(<DsarLog dsars={[dsar()]} />)

    await user.click(screen.getByRole('button', { name: /log a dsar/i }))
    const save = screen.getByRole('button', { name: /^log dsar$/i })
    expect(save).toBeDisabled()

    await user.type(screen.getByLabelText('Requester'), '  ')
    expect(save).toBeDisabled()

    await user.type(screen.getByLabelText('Requester'), 'John Doe')
    expect(save).toBeEnabled()

    await user.click(save)
    expect(logDsarMock).toHaveBeenCalledTimes(1)
  })

  it('surfaces a mark-responded error', async () => {
    const user = userEvent.setup()
    markRespondedMock.mockResolvedValue({ ok: false, error: 'requires a reviewed approval' })
    render(<DsarLog dsars={[dsar()]} />)

    await user.click(screen.getByRole('button', { name: /mark as responded/i }))
    await user.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(await screen.findByText(/requires a reviewed approval/i)).toBeInTheDocument()
    expect(refreshMock).not.toHaveBeenCalled()
  })
})
