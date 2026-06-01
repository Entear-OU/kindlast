// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchPendingNotifications } from '@/lib/notifications/dispatch'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, createUserClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-76 — notification preferences against the live stack.
 *
 * The new columns persist and are RLS-scoped; the finding dispatcher honours
 * min_severity_for_email (below-floor → skipped) and quiet hours (critical still
 * sends). Findings come through the real Watcher→Analyst pipeline; severity is
 * pinned for determinism.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent76_'
const SUMMARY =
  'Fixture obligation for the ENT-76 preferences test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeFinding(profileId: string, slug: string, severity: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, 'A control is missing.', 'high', $4::text, '{}'::jsonb
     ) as id`,
    [profileId, `gap:${slug}:${(seq += 1)}`, `Profile gap: ${slug}`, slug],
  )
  const [{ id }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  await querySql(`update public.findings set severity = $2 where id = $1`, [id, severity])
  return id
}

function dispatch(userId: string, email: ReturnType<typeof createCapturingEmailProvider>, nowSeconds?: number) {
  return dispatchPendingNotifications({
    supabase: createServiceRoleClient(),
    emailProvider: email,
    baseUrl: 'https://app.kindlast.com',
    tokenSecret: 'integration-secret',
    userId,
    nowSeconds,
  })
}

describe.skipIf(!supabaseRunning)('notification preferences (ENT-76)', () => {
  let user: TestUser
  let profileId: string

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

  it('persists the new columns and scopes them by RLS', async () => {
    const admin = createServiceRoleClient()
    await admin.from('notification_preferences').upsert({
      user_id: user.id,
      email: 'alerts@example.com',
      min_severity_for_email: 'high',
      deadline_alerts_enabled: false,
      quiet_hours_start: '22:00',
      quiet_hours_end: '07:00',
      timezone: 'UTC',
    })

    const ownerClient = await createUserClient(user.email, user.password)
    const { data: own } = await ownerClient
      .from('notification_preferences')
      .select('email,min_severity_for_email,deadline_alerts_enabled,quiet_hours_start,timezone')
      .eq('user_id', user.id)
      .single()
    expect(own).toMatchObject({
      email: 'alerts@example.com',
      min_severity_for_email: 'high',
      deadline_alerts_enabled: false,
      timezone: 'UTC',
    })

    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const { data: leaked } = await otherClient
        .from('notification_preferences')
        .select('user_id')
        .eq('user_id', user.id)
      expect(leaked).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })

  it('skips a finding below min_severity_for_email', async () => {
    // floor is 'high' (set above); a medium finding is gated out.
    const id = await makeFinding(profileId, `${PREFIX}ob`, 'medium')
    const email = createCapturingEmailProvider()
    const summary = await dispatch(user.id, email)
    expect(email.sent.find((m) => m.to === 'alerts@example.com')).toBeUndefined()

    const [{ status, last_error }] = await querySql<{ status: string; last_error: string | null }>(
      `select status, last_error from public.notification_outbox where finding_id = $1`,
      [id],
    )
    expect(status).toBe('skipped')
    expect(last_error ?? '').toContain('min_severity')
    expect(summary.skipped).toBeGreaterThanOrEqual(1)
  })

  it('sends a critical finding even inside quiet hours', async () => {
    const id = await makeFinding(profileId, `${PREFIX}ob`, 'critical')
    const email = createCapturingEmailProvider()
    // 2024-01-01 02:00 UTC — inside the 22:00→07:00 quiet window.
    const nowSeconds = Math.floor(Date.UTC(2024, 0, 1, 2, 0, 0) / 1000)
    await dispatch(user.id, email, nowSeconds)

    expect(email.sent.some((m) => m.to === 'alerts@example.com' && m.subject.includes('[Critical]'))).toBe(true)
    const [{ status }] = await querySql<{ status: string }>(
      `select status from public.notification_outbox where finding_id = $1`,
      [id],
    )
    expect(status).toBe('sent')
  })

  it('holds a non-critical finding inside quiet hours (left pending)', async () => {
    // floor must allow it through to the quiet-hours check: lower the floor.
    await createServiceRoleClient()
      .from('notification_preferences')
      .upsert({ user_id: user.id, email: 'alerts@example.com', min_severity_for_email: 'low', quiet_hours_start: '22:00', quiet_hours_end: '07:00', timezone: 'UTC' })

    const id = await makeFinding(profileId, `${PREFIX}ob`, 'high')
    const email = createCapturingEmailProvider()
    const nowSeconds = Math.floor(Date.UTC(2024, 0, 1, 2, 0, 0) / 1000)
    const summary = await dispatch(user.id, email, nowSeconds)

    expect(email.sent.find((m) => m.subject.includes('[High]'))).toBeUndefined()
    expect(summary.deferred).toBeGreaterThanOrEqual(1)
    const [{ status }] = await querySql<{ status: string }>(
      `select status from public.notification_outbox where finding_id = $1`,
      [id],
    )
    expect(status).toBe('pending')
  })
})
