import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-151 — the console rail's Billing destination.
 * ENT-155 — the header pill + Alerts dot are derived from real agent state.
 *
 * `ConsoleShell` is an async server component, so each case renders the awaited
 * element. The auth client and the status read are mocked so the rail and pill
 * assertions stay deterministic.
 */

const { getUserMock, loadStatusMock } = vi.hoisted(() => ({
  getUserMock: vi.fn(),
  loadStatusMock: vi.fn(),
}))

vi.mock('@/lib/supabase/server', () => ({
  createClient: async () => ({ auth: { getUser: getUserMock } }),
}))

vi.mock('@/lib/console/agent-status', () => ({
  loadConsoleAgentStatus: loadStatusMock,
}))

import { ConsoleShell } from '@/components/console/console-shell'

async function renderShell(props: { activeRail: 'alerts' | 'billing'; title: string }) {
  render(await ConsoleShell({ ...props, children: <div>body</div> }))
}

beforeEach(() => {
  vi.clearAllMocks()
  getUserMock.mockResolvedValue({ data: { user: { id: 'u1' } } })
  loadStatusMock.mockResolvedValue({
    agentStatus: { running: true, text: 'Agent running · last scan 5 min ago' },
    hasPendingFindings: false,
  })
})

describe('ConsoleShell rail — Billing (ENT-151)', () => {
  it('links Billing to /billing when not the active rail', async () => {
    await renderShell({ activeRail: 'alerts', title: 'Agent feed' })
    expect(screen.getByRole('link', { name: 'Billing' })).toHaveAttribute('href', '/billing')
  })

  it('marks Billing active (aria-current, no link) on the billing page', async () => {
    await renderShell({ activeRail: 'billing', title: 'Billing' })
    expect(screen.queryByRole('link', { name: 'Billing' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Billing')).toHaveAttribute('aria-current', 'page')
  })

  it('keeps the other real destinations linked', async () => {
    await renderShell({ activeRail: 'billing', title: 'Billing' })
    expect(screen.getByRole('link', { name: 'Records' })).toHaveAttribute('href', '/records/ropa')
    expect(screen.getByRole('link', { name: 'Alerts' })).toHaveAttribute('href', '/feed')
  })
})

describe('ConsoleShell header status (ENT-155)', () => {
  it('renders the real agent-status pill text', async () => {
    loadStatusMock.mockResolvedValue({
      agentStatus: { running: false, text: "Watcher hasn't run yet" },
      hasPendingFindings: false,
    })
    await renderShell({ activeRail: 'alerts', title: 'Agent feed' })
    expect(screen.getByText("Watcher hasn't run yet")).toBeInTheDocument()
    // The misleading hard-coded default must be gone.
    expect(screen.queryByText(/last scan 4 min ago/)).not.toBeInTheDocument()
  })

  it('shows no Alerts unread dot when nothing is pending', async () => {
    loadStatusMock.mockResolvedValue({
      agentStatus: { running: true, text: 'Agent running · last scan 1 min ago' },
      hasPendingFindings: false,
    })
    await renderShell({ activeRail: 'billing', title: 'Billing' })
    const alerts = screen.getByRole('link', { name: 'Alerts' })
    expect(alerts.querySelector('span.bg-rose-500')).toBeNull()
  })

  it('shows the Alerts unread dot when findings are pending', async () => {
    loadStatusMock.mockResolvedValue({
      agentStatus: { running: true, text: 'Agent running · last scan 1 min ago' },
      hasPendingFindings: true,
    })
    await renderShell({ activeRail: 'billing', title: 'Billing' })
    const alerts = screen.getByRole('link', { name: 'Alerts' })
    expect(alerts.querySelector('span.bg-rose-500')).not.toBeNull()
  })
})
