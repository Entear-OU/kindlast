import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NotificationSettings } from '@/components/settings/notification-settings'
import type { NotificationPreferences } from '@/lib/notifications/preferences'

/**
 * ENT-76 — notification settings form. Mirrors the billing-plans component test:
 * mock the server action + sonner, drive with userEvent, assert the action gets
 * the toggled values and the Free user can't enable the weekly briefing.
 */

const { updateMock } = vi.hoisted(() => ({ updateMock: vi.fn() }))
vi.mock('@/app/(authed)/settings/actions', () => ({ updateNotificationPreferences: updateMock }))

const { toastSuccess, toastError } = vi.hoisted(() => ({ toastSuccess: vi.fn(), toastError: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError } }))

vi.mock('next/link', () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}))

const PREFS: NotificationPreferences = {
  email: 'founder@example.com',
  minSeverityForEmail: 'medium',
  weeklyBriefingEnabled: true,
  deadlineAlertsEnabled: true,
  quietHoursStart: null,
  quietHoursEnd: null,
  timezone: 'Europe/Tallinn',
}

beforeEach(() => {
  updateMock.mockReset()
  toastSuccess.mockReset()
  toastError.mockReset()
})
afterEach(() => vi.clearAllMocks())

describe('NotificationSettings (ENT-76)', () => {
  it('saves the toggled values through the action and toasts success', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue({ ok: true })
    render(<NotificationSettings prefs={PREFS} plan="pro" />)

    // Turn deadline alerts off.
    await user.click(screen.getByRole('switch', { name: /deadline alerts/i }))
    await user.click(screen.getByRole('button', { name: /save changes/i }))

    expect(updateMock).toHaveBeenCalledTimes(1)
    expect(updateMock.mock.calls[0][0]).toMatchObject({
      email: 'founder@example.com',
      minSeverityForEmail: 'medium',
      weeklyBriefingEnabled: true,
      deadlineAlertsEnabled: false,
      timezone: 'Europe/Tallinn',
    })
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled())
  })

  it('disables the weekly briefing toggle for a Free user and shows the upgrade link', () => {
    render(<NotificationSettings prefs={{ ...PREFS, weeklyBriefingEnabled: false }} plan="free" />)
    expect(screen.getByRole('switch', { name: /weekly monday briefing/i })).toBeDisabled()
    expect(screen.getByRole('link', { name: /upgrade to pro/i })).toHaveAttribute('href', '/billing')
  })

  it('surfaces an error toast when the action fails', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue({ ok: false, error: 'Nope' })
    render(<NotificationSettings prefs={PREFS} plan="pro" />)

    await user.click(screen.getByRole('button', { name: /save changes/i }))

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Nope'))
  })
})
