import { describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchPendingNotifications } from '@/lib/notifications/dispatch'

/**
 * ENT-73 — Comms dispatcher.
 *
 * Hermetic: a tiny in-memory fake of the Supabase client stands in for the
 * stack so the gate / send / status-marking logic is asserted without a DB.
 * Sends when the severity gate passes, skips when it doesn't, marks the outbox,
 * and never double-sends (drains only `pending`).
 */

interface Tables {
  notification_outbox: Record<string, unknown>[]
  findings: Record<string, unknown>[]
  notification_preferences: Record<string, unknown>[]
}

function makeFakeSupabase(tables: Tables, users: Record<string, { email: string | null }>) {
  function query(table: keyof Tables) {
    // Unknown tables (e.g. `subscriptions`, read by getPlan for the upsell
    // footer) resolve empty → getPlan falls back to 'free'.
    const rows = tables[table] ?? []
    const filters: [string, unknown][] = []
    let op: 'select' | 'update' = 'select'
    let patch: Record<string, unknown> = {}

    const matches = (r: Record<string, unknown>) => filters.every(([c, v]) => r[c] === v)

    const builder: Record<string, unknown> = {
      select() {
        return builder
      },
      update(p: Record<string, unknown>) {
        op = 'update'
        patch = p
        return builder
      },
      eq(col: string, val: unknown) {
        filters.push([col, val])
        if (op === 'update') {
          for (const r of rows) if (matches(r)) Object.assign(r, patch)
          return Promise.resolve({ data: null, error: null })
        }
        return builder
      },
      order() {
        return builder
      },
      limit() {
        return Promise.resolve({ data: rows.filter(matches), error: null })
      },
      single() {
        const found = rows.find(matches) ?? null
        return Promise.resolve({ data: found, error: found ? null : { message: 'not found' } })
      },
      maybeSingle() {
        return Promise.resolve({ data: rows.find(matches) ?? null, error: null })
      },
    }
    return builder
  }

  return {
    from: (table: keyof Tables) => query(table),
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
const SECRET = 'dispatch-secret'

function baseFinding(id: string, severity: string, userId: string) {
  return {
    id,
    detected: 'Something needs action',
    severity,
    proposed_action: 'Do the thing',
    regulatory_obligation: 'GDPR Art. 30',
    citation_url: null,
    effort_estimate: 'hours',
    user_id: userId,
  }
}

describe('dispatchPendingNotifications (ENT-73)', () => {
  it('sends a passing finding and marks the outbox sent', async () => {
    const tables: Tables = {
      notification_outbox: [{ id: 'o1', finding_id: 'f1', user_id: 'u1', status: 'pending', channel: 'email', attempts: 0 }],
      findings: [baseFinding('f1', 'critical', 'u1')],
      notification_preferences: [{ user_id: 'u1', email_frequency: 'immediate' }],
    }
    const email = createCapturingEmailProvider()
    const supabase = makeFakeSupabase(tables, { u1: { email: 'founder@example.com' } })

    const summary = await dispatchPendingNotifications({ supabase, emailProvider: email, baseUrl: BASE, tokenSecret: SECRET })

    expect(summary).toMatchObject({ processed: 1, sent: 1, skipped: 0, failed: 0 })
    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].to).toBe('founder@example.com')
    expect(email.sent[0].subject).toContain('[Critical]')
    expect(tables.notification_outbox[0].status).toBe('sent')
    expect(tables.notification_outbox[0].sent_at).toBeTruthy()
  })

  it('skips a finding gated out by the severity preference', async () => {
    const tables: Tables = {
      // low severity + 'immediate' (high+ only) → gated out
      notification_outbox: [{ id: 'o1', finding_id: 'f1', user_id: 'u1', status: 'pending', channel: 'email', attempts: 0 }],
      findings: [baseFinding('f1', 'low', 'u1')],
      notification_preferences: [{ user_id: 'u1', email_frequency: 'immediate' }],
    }
    const email = createCapturingEmailProvider()
    const supabase = makeFakeSupabase(tables, { u1: { email: 'founder@example.com' } })

    const summary = await dispatchPendingNotifications({ supabase, emailProvider: email, baseUrl: BASE, tokenSecret: SECRET })

    expect(summary).toMatchObject({ processed: 1, sent: 0, skipped: 1 })
    expect(email.sent).toHaveLength(0)
    expect(tables.notification_outbox[0].status).toBe('skipped')
  })

  it('defaults to daily frequency when no preference row exists', async () => {
    const tables: Tables = {
      // medium + default 'daily' (medium+) → sends
      notification_outbox: [{ id: 'o1', finding_id: 'f1', user_id: 'u1', status: 'pending', channel: 'email', attempts: 0 }],
      findings: [baseFinding('f1', 'medium', 'u1')],
      notification_preferences: [],
    }
    const email = createCapturingEmailProvider()
    const supabase = makeFakeSupabase(tables, { u1: { email: 'founder@example.com' } })

    const summary = await dispatchPendingNotifications({ supabase, emailProvider: email, baseUrl: BASE, tokenSecret: SECRET })
    expect(summary.sent).toBe(1)
  })

  it('does not drain rows that are already sent', async () => {
    const tables: Tables = {
      notification_outbox: [{ id: 'o1', finding_id: 'f1', user_id: 'u1', status: 'sent', channel: 'email', attempts: 0 }],
      findings: [baseFinding('f1', 'critical', 'u1')],
      notification_preferences: [{ user_id: 'u1', email_frequency: 'immediate' }],
    }
    const email = createCapturingEmailProvider()
    const supabase = makeFakeSupabase(tables, { u1: { email: 'founder@example.com' } })

    const summary = await dispatchPendingNotifications({ supabase, emailProvider: email, baseUrl: BASE, tokenSecret: SECRET })
    expect(summary.processed).toBe(0)
    expect(email.sent).toHaveLength(0)
  })

  it('skips (does not fail) when the user has no email address', async () => {
    const tables: Tables = {
      notification_outbox: [{ id: 'o1', finding_id: 'f1', user_id: 'u1', status: 'pending', channel: 'email', attempts: 0 }],
      findings: [baseFinding('f1', 'critical', 'u1')],
      notification_preferences: [{ user_id: 'u1', email_frequency: 'immediate' }],
    }
    const email = createCapturingEmailProvider()
    const supabase = makeFakeSupabase(tables, { u1: { email: null } })

    const summary = await dispatchPendingNotifications({ supabase, emailProvider: email, baseUrl: BASE, tokenSecret: SECRET })
    expect(summary).toMatchObject({ sent: 0, skipped: 1, failed: 0 })
    expect(tables.notification_outbox[0].status).toBe('skipped')
  })
})
