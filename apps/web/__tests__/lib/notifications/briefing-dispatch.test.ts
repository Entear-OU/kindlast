import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'

vi.mock('@/lib/billing/plan', () => ({ getPlan: vi.fn() }))
vi.mock('@/lib/notifications/briefing', () => ({ buildBriefing: vi.fn() }))

import { getPlan } from '@/lib/billing/plan'
import { buildBriefing } from '@/lib/notifications/briefing'
import { briefingWindow, dispatchWeeklyBriefing } from '@/lib/notifications/briefing-dispatch'

/**
 * ENT-74 — weekly briefing dispatcher.
 *
 * `briefingWindow` is tested directly (pure, timezone-aware). The dispatcher's
 * gating (opt-out, window, Pro, dedup) is tested with an in-memory fake stack and
 * mocked getPlan/buildBriefing, mirroring the ENT-73 dispatch unit style.
 */

const STUB_BRIEFING = {
  findingsBySeverity: { critical: 1, high: 0, medium: 0, low: 0 },
  openTotal: 1,
  upcomingDeadlines: [],
  executorActions: [],
}

interface Tables {
  compliance_profiles: Record<string, unknown>[]
  notification_preferences: Record<string, unknown>[]
  weekly_briefing_log: Record<string, unknown>[]
}

function makeFake(tables: Tables, users: Record<string, { email: string | null }>) {
  function from(table: keyof Tables) {
    const filters: [string, unknown][] = []
    let op: 'select' | 'upsert' | 'delete' = 'select'
    let payload: Record<string, unknown> = {}
    let ignoreDuplicates = false
    const match = (r: Record<string, unknown>) => filters.every(([c, v]) => r[c] === v)

    function run() {
      if (op === 'upsert') {
        const exists = tables[table].some(
          (r) => r.user_id === payload.user_id && r.period_start === payload.period_start,
        )
        if (exists && ignoreDuplicates) return { data: [], error: null }
        tables[table].push({ ...payload })
        return { data: [{ ...payload }], error: null }
      }
      if (op === 'delete') {
        tables[table] = tables[table].filter((r) => !match(r))
        return { data: null, error: null }
      }
      return { data: tables[table].filter(match), error: null }
    }

    const b: Record<string, unknown> = {
      select: () => b,
      eq: (c: string, v: unknown) => {
        filters.push([c, v])
        return b
      },
      upsert: (obj: Record<string, unknown>, opts?: { ignoreDuplicates?: boolean }) => {
        op = 'upsert'
        payload = obj
        ignoreDuplicates = !!opts?.ignoreDuplicates
        return b
      },
      delete: () => {
        op = 'delete'
        return b
      },
      then: (resolve: (v: unknown) => void, reject: (e: unknown) => void) => {
        try {
          resolve(run())
        } catch (e) {
          reject(e)
        }
      },
    }
    return b
  }

  return {
    from,
    auth: {
      admin: {
        getUserById: async (id: string) => ({
          data: { user: users[id] ? { id, email: users[id].email } : null },
          error: null,
        }),
      },
    },
  } as never
}

const BASE = 'https://app.kindlast.com'
const SECRET = 'briefing-secret'
// 2024-01-01 07:00 UTC === Monday 09:00 in Europe/Tallinn (UTC+2 in winter).
const MONDAY_0900_TALLINN = Date.UTC(2024, 0, 1, 7, 0, 0)
// A Tuesday — never in-window.
const TUESDAY = Date.UTC(2024, 0, 2, 7, 0, 0)

beforeEach(() => {
  vi.mocked(buildBriefing).mockResolvedValue(STUB_BRIEFING)
  vi.mocked(getPlan).mockResolvedValue('pro')
})

describe('briefingWindow (ENT-74)', () => {
  it('is in-window at Monday 09:00 local and reports the local Monday date', () => {
    const w = briefingWindow(MONDAY_0900_TALLINN, 'Europe/Tallinn')
    expect(w.isWindow).toBe(true)
    expect(w.periodStart).toBe('2024-01-01')
  })

  it('the same instant is off-window in a different timezone', () => {
    // 07:00 UTC is 02:00 in New York — Monday but not the 09:00 hour.
    expect(briefingWindow(MONDAY_0900_TALLINN, 'America/New_York').isWindow).toBe(false)
  })

  it('is off-window on a non-Monday', () => {
    expect(briefingWindow(TUESDAY, 'Europe/Tallinn').isWindow).toBe(false)
  })
})

describe('dispatchWeeklyBriefing (ENT-74)', () => {
  function fixture(overrides?: Partial<{ enabled: boolean; tz: string }>): Tables {
    return {
      compliance_profiles: [{ user_id: 'u1' }],
      notification_preferences: [
        {
          user_id: 'u1',
          timezone: overrides?.tz ?? 'Europe/Tallinn',
          weekly_briefing_enabled: overrides?.enabled ?? true,
        },
      ],
      weekly_briefing_log: [],
    }
  }

  const run = (tables: Tables, opts?: { force?: boolean; nowMs?: number }) =>
    dispatchWeeklyBriefing({
      supabase: makeFake(tables, { u1: { email: 'founder@example.com' } }),
      emailProvider: createCapturingEmailProvider(),
      baseUrl: BASE,
      tokenSecret: SECRET,
      userId: 'u1',
      ...opts,
    })

  it('sends to a Pro, opted-in user in their Monday-09:00 window', async () => {
    const tables = fixture()
    const email = createCapturingEmailProvider()
    const summary = await dispatchWeeklyBriefing({
      supabase: makeFake(tables, { u1: { email: 'founder@example.com' } }),
      emailProvider: email,
      baseUrl: BASE,
      tokenSecret: SECRET,
      userId: 'u1',
      nowMs: MONDAY_0900_TALLINN,
    })
    expect(summary).toMatchObject({ processed: 1, sent: 1, skipped: 0, failed: 0 })
    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].to).toBe('founder@example.com')
    expect(tables.weekly_briefing_log).toHaveLength(1)
  })

  it('skips when off-window', async () => {
    const summary = await run(fixture(), { nowMs: TUESDAY })
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
  })

  it('skips an opted-out user even in-window', async () => {
    const summary = await run(fixture({ enabled: false }), { force: true })
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
  })

  it('skips a non-Pro user', async () => {
    vi.mocked(getPlan).mockResolvedValue('free')
    const summary = await run(fixture(), { force: true })
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
  })

  it('dedups — a second run in the same week sends nothing more', async () => {
    const tables = fixture()
    const first = await run(tables, { force: true, nowMs: MONDAY_0900_TALLINN })
    expect(first.sent).toBe(1)
    const second = await run(tables, { force: true, nowMs: MONDAY_0900_TALLINN })
    expect(second).toMatchObject({ sent: 0, skipped: 1 })
    expect(tables.weekly_briefing_log).toHaveLength(1)
  })
})
