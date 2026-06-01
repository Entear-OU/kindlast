import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-76 — the notification-preferences server action. Pins: validation
 * (severity / time / email), the server-side Pro gate on the weekly briefing,
 * and the happy-path upsert of the caller's own row.
 */

const { getUserMock, upsertMock, getPlanMock } = vi.hoisted(() => ({
  getUserMock: vi.fn(),
  upsertMock: vi.fn(),
  getPlanMock: vi.fn(),
}))

vi.mock('@/lib/supabase/server', () => ({
  createClient: async () => ({
    auth: { getUser: getUserMock },
    from: () => ({ upsert: upsertMock }),
  }),
}))

vi.mock('@/lib/billing/plan', () => ({ getPlan: getPlanMock }))

import { updateNotificationPreferences } from '@/app/(authed)/settings/actions'

const VALID = {
  email: 'founder@example.com',
  minSeverityForEmail: 'medium' as const,
  weeklyBriefingEnabled: false,
  deadlineAlertsEnabled: true,
  quietHoursStart: '22:00',
  quietHoursEnd: '07:00',
  timezone: 'Europe/Tallinn',
}

beforeEach(() => {
  vi.clearAllMocks()
  getUserMock.mockResolvedValue({ data: { user: { id: 'u1', email: 'founder@example.com' } } })
  getPlanMock.mockResolvedValue('pro')
  upsertMock.mockResolvedValue({ error: null })
})
afterEach(() => vi.clearAllMocks())

describe('updateNotificationPreferences (ENT-76)', () => {
  it('upserts the caller row on valid input', async () => {
    const res = await updateNotificationPreferences(VALID)
    expect(res).toEqual({ ok: true })
    expect(upsertMock).toHaveBeenCalledTimes(1)
    expect(upsertMock.mock.calls[0][0]).toMatchObject({
      user_id: 'u1',
      min_severity_for_email: 'medium',
      quiet_hours_start: '22:00',
      timezone: 'Europe/Tallinn',
    })
  })

  it('coerces an empty quiet-hours field to null', async () => {
    await updateNotificationPreferences({ ...VALID, quietHoursStart: '', quietHoursEnd: '' })
    expect(upsertMock.mock.calls[0][0]).toMatchObject({ quiet_hours_start: null, quiet_hours_end: null })
  })

  it('rejects an invalid time', async () => {
    const res = await updateNotificationPreferences({ ...VALID, quietHoursStart: '25:00' })
    expect(res.ok).toBe(false)
    expect(upsertMock).not.toHaveBeenCalled()
  })

  it('rejects an invalid email', async () => {
    const res = await updateNotificationPreferences({ ...VALID, email: 'not-an-email' })
    expect(res.ok).toBe(false)
  })

  it('blocks a Free user from enabling the weekly briefing', async () => {
    getPlanMock.mockResolvedValue('free')
    const res = await updateNotificationPreferences({ ...VALID, weeklyBriefingEnabled: true })
    expect(res).toMatchObject({ ok: false, upgrade: true })
    expect(upsertMock).not.toHaveBeenCalled()
  })

  it('rejects when unauthenticated', async () => {
    getUserMock.mockResolvedValue({ data: { user: null } })
    const res = await updateNotificationPreferences(VALID)
    expect(res).toMatchObject({ ok: false })
    expect(upsertMock).not.toHaveBeenCalled()
  })
})
