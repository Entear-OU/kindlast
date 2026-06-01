import { describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchDeadlineAlerts } from '@/lib/notifications/deadline-dispatch'

/**
 * ENT-75 — deadline alert dispatcher.
 *
 * In-memory fake stack (mirrors briefing-dispatch.test.ts). Verifies the
 * crossing/dedup behaviour: send on first reaching a threshold, dedup the same
 * threshold, fire again at the next bucket, skip beyond 30 days.
 */

interface Tables {
  findings: Record<string, unknown>[]
  deadline_alert_log: Record<string, unknown>[]
  notification_preferences?: Record<string, unknown>[]
}

function makeFake(tables: Tables, users: Record<string, { email: string | null }>) {
  function from(table: keyof Tables) {
    // Unknown/empty tables (e.g. notification_preferences when no row is seeded)
    // resolve empty → resolvePreferences falls back to defaults + auth email.
    if (!tables[table]) tables[table] = []
    const filters: [string, unknown][] = []
    let op: 'select' | 'upsert' | 'delete' = 'select'
    let payload: Record<string, unknown> = {}
    let ignoreDuplicates = false
    const match = (r: Record<string, unknown>) => filters.every(([c, v]) => r[c] === v)

    function run() {
      const rows = tables[table] as Record<string, unknown>[]
      if (op === 'upsert') {
        const exists = rows.some(
          (r) => r.finding_id === payload.finding_id && r.threshold === payload.threshold,
        )
        if (exists && ignoreDuplicates) return { data: [], error: null }
        rows.push({ ...payload })
        return { data: [{ ...payload }], error: null }
      }
      if (op === 'delete') {
        tables[table] = rows.filter((r) => !match(r))
        return { data: null, error: null }
      }
      return { data: rows.filter(match), error: null }
    }

    const b: Record<string, unknown> = {
      select: () => b,
      eq: (c: string, v: unknown) => {
        filters.push([c, v])
        return b
      },
      in: () => b, // jsonb-path filter is a no-op here; test rows are pre-scoped
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
      maybeSingle: () => Promise.resolve({ data: (tables[table] ?? []).find(match) ?? null, error: null }),
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
const SECRET = 'deadline-secret'

function finding(id: string, days: number, userId = 'u1') {
  return {
    id,
    user_id: userId,
    status: 'pending',
    detected: 'Deadline approaching',
    regulatory_obligation: 'GDPR Art. 30',
    citation_url: null,
    proposed_action: 'File the ROPA',
    metadata: {
      signal_kind: 'deadline',
      signal_metadata: { days_remaining: days, effective_date: '2026-06-20' },
    },
  }
}

const run = (tables: Tables, opts?: { userId?: string }) =>
  dispatchDeadlineAlerts({
    supabase: makeFake(tables, { u1: { email: 'founder@example.com' } }),
    emailProvider: createCapturingEmailProvider(),
    baseUrl: BASE,
    tokenSecret: SECRET,
    ...opts,
  })

describe('dispatchDeadlineAlerts (ENT-75)', () => {
  it('sends a deadline alert on reaching a threshold and logs it', async () => {
    const tables: Tables = { findings: [finding('f1', 14)], deadline_alert_log: [] }
    const email = createCapturingEmailProvider()
    const summary = await dispatchDeadlineAlerts({
      supabase: makeFake(tables, { u1: { email: 'founder@example.com' } }),
      emailProvider: email,
      baseUrl: BASE,
      tokenSecret: SECRET,
    })
    expect(summary).toMatchObject({ processed: 1, sent: 1, skipped: 0, failed: 0 })
    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].subject).toContain('14 days left')
    expect(tables.deadline_alert_log).toEqual([
      expect.objectContaining({ finding_id: 'f1', threshold: 14, user_id: 'u1' }),
    ])
  })

  it('dedups the same threshold on a second run', async () => {
    const tables: Tables = { findings: [finding('f1', 14)], deadline_alert_log: [] }
    await run(tables)
    const summary = await run(tables)
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
    expect(tables.deadline_alert_log).toHaveLength(1)
  })

  it('fires again when the finding crosses into the next bucket', async () => {
    const tables: Tables = { findings: [finding('f1', 14)], deadline_alert_log: [] }
    await run(tables) // logs threshold 14
    tables.findings[0].metadata = {
      signal_kind: 'deadline',
      signal_metadata: { days_remaining: 7, effective_date: '2026-06-20' },
    }
    const summary = await run(tables)
    expect(summary.sent).toBe(1)
    expect(tables.deadline_alert_log.map((r) => r.threshold).sort((a, b) => (a as number) - (b as number))).toEqual([7, 14])
  })

  it('skips a finding still more than 30 days out', async () => {
    const tables: Tables = { findings: [finding('f1', 40)], deadline_alert_log: [] }
    const summary = await run(tables)
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
    expect(tables.deadline_alert_log).toHaveLength(0)
  })

  it('skips a user who opted out of deadline alerts (and does not claim the threshold)', async () => {
    const tables: Tables = {
      findings: [finding('f1', 7)],
      deadline_alert_log: [],
      notification_preferences: [{ user_id: 'u1', deadline_alerts_enabled: false }],
    }
    const summary = await run(tables)
    expect(summary).toMatchObject({ sent: 0, skipped: 1 })
    expect(tables.deadline_alert_log).toHaveLength(0) // not claimed → re-enable can still fire
  })
})
