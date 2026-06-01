// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchPendingNotifications } from '@/lib/notifications/dispatch'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-73 — Comms dispatcher against the live stack.
 *
 * Drains the outbox for one user (scoped, so it stays hermetic next to other
 * suites): a finding that clears the severity gate is emailed and its outbox row
 * marked sent; a finding gated out by the preference is marked skipped and never
 * sent. The email is captured in-memory (no network).
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent73_dispatch_'
const SUMMARY =
  'Fixture obligation for the ENT-73 dispatcher test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeFinding(profileId: string, slug: string, severity: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, 'A control is missing.', 'high', $4::text, '{}'::jsonb
     ) as id`,
    [profileId, `gap:${slug}:${(seq += 1)}`, `Profile gap: ${slug}`, slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  // Pin severity deterministically — the Analyst may otherwise adjust it (ENT-61).
  await querySql(`update public.findings set severity = $2 where id = $1`, [findingId, severity])
  return findingId
}

describe.skipIf(!supabaseRunning)('notification dispatcher (ENT-73)', () => {
  let user: TestUser
  let profileId: string
  let criticalId: string
  let lowId: string

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)

    const { data: session } = await admin
      .from('onboarding_sessions')
      .insert({ user_id: user.id, status: 'completed' })
      .select('id')
      .single()
    const { data: profile } = await admin
      .from('compliance_profiles')
      .insert({
        session_id: session!.id,
        user_id: user.id,
        industry: 'SaaS',
        has_dpo: 'no',
        has_ropa: 'no',
        transfers_outside_eu: 'no',
      })
      .select('id')
      .single()
    profileId = profile!.id as string

    await applyFixtureSql(`
      insert into public.obligations
        (slug, title, summary, citation_celex, citation_kind, citation_article,
         applies_when, severity, effective_date)
      values
        ('${PREFIX}ob', 'Fixture ${PREFIX}ob', '${SUMMARY}',
         '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
      on conflict (slug) do nothing;
    `)

    // Medium floor: critical passes, low is gated out (ENT-76).
    await admin
      .from('notification_preferences')
      .upsert({ user_id: user.id, min_severity_for_email: 'medium' })

    criticalId = await makeFinding(profileId, `${PREFIX}ob`, 'critical')
    lowId = await makeFinding(profileId, `${PREFIX}ob`, 'low')
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('emails the gate-passing finding and skips the gated one', async () => {
    const email = createCapturingEmailProvider()
    const summary = await dispatchPendingNotifications({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: user.id,
    })

    expect(summary).toMatchObject({ processed: 2, sent: 1, skipped: 1, failed: 0 })

    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].to).toBe(user.email)
    expect(email.sent[0].subject).toContain('[Critical]')

    const rows = await querySql<{ finding_id: string; status: string; sent_at: string | null }>(
      `select finding_id, status, sent_at from public.notification_outbox
        where finding_id in ($1, $2)`,
      [criticalId, lowId],
    )
    const byId = Object.fromEntries(rows.map((r) => [r.finding_id, r]))
    expect(byId[criticalId].status).toBe('sent')
    expect(byId[criticalId].sent_at).toBeTruthy()
    expect(byId[lowId].status).toBe('skipped')
  })

  it('is idempotent — a second drain sends nothing more', async () => {
    const email = createCapturingEmailProvider()
    const summary = await dispatchPendingNotifications({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: user.id,
    })
    expect(summary.processed).toBe(0)
    expect(email.sent).toHaveLength(0)
  })
})
