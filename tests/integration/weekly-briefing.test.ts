// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchWeeklyBriefing } from '@/lib/notifications/briefing-dispatch'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-74 — weekly briefing dispatcher against the live stack.
 *
 * A Pro, opted-in user with open findings (incl. a deadline-signal finding) and
 * a recent Executor action gets one briefing with all three sections; the
 * weekly_briefing_log dedups a second run. A Free user and an opted-out user are
 * skipped. `force: true` + `userId` keep it deterministic without a real Monday.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent74_brief_'
const SUMMARY =
  'Fixture obligation for the ENT-74 weekly-briefing test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeDeadlineFinding(profileId: string, slug: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'deadline', $2::text, $3::text, 'Effective date approaching.', 'high', $4::text,
       jsonb_build_object('days_remaining', 9, 'effective_date', (current_date + 9))
     ) as id`,
    [profileId, `deadline:${slug}:${(seq += 1)}`, `Deadline: ${slug}`, slug],
  )
  const [{ id }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return id
}

async function setup(admin: ReturnType<typeof createServiceRoleClient>) {
  const user = await signUpTestUser(admin)
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
  return { user, profileId: profile!.id as string }
}

describe.skipIf(!supabaseRunning)('weekly briefing dispatcher (ENT-74)', () => {
  let pro: TestUser
  let proProfile: string

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    const s = await setup(admin)
    pro = s.user
    proProfile = s.profileId

    await applyFixtureSql(`
      insert into public.obligations
        (slug, title, summary, citation_celex, citation_kind, citation_article,
         applies_when, severity, effective_date)
      values
        ('${PREFIX}ob', 'Fixture ${PREFIX}ob', '${SUMMARY}',
         '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
      on conflict (slug) do nothing;
    `)

    await admin.from('subscriptions').update({ plan: 'pro' }).eq('user_id', pro.id)
    await admin
      .from('notification_preferences')
      .upsert({ user_id: pro.id, weekly_briefing_enabled: true, timezone: 'Europe/Tallinn' })

    await makeDeadlineFinding(proProfile, `${PREFIX}ob`)

    // A recent Executor action for the "what shipped" section.
    await querySql(
      `insert into public.audit_log
         (user_id, action_type, target_table, approving_user_id, occurred_at)
       values ($1, 'create_ropa', 'processing_activities', $1, now() - interval '2 days')`,
      [pro.id],
    )
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`
      delete from public.audit_log where target_table = 'processing_activities'
        and user_id in (select user_id from public.compliance_profiles where id = '${proProfile}');
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (pro?.id) await deleteTestUser(admin, pro.id)
  })

  it('sends one briefing with all three sections, then dedups', async () => {
    const email = createCapturingEmailProvider()
    const summary = await dispatchWeeklyBriefing({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: pro.id,
      force: true,
    })

    expect(summary).toMatchObject({ processed: 1, sent: 1, skipped: 0, failed: 0 })
    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].to).toBe(pro.email)
    const body = email.sent[0].text
    expect(body).toContain('Open findings')
    expect(body).toContain('Upcoming deadlines')
    expect(body).toContain('What shipped')
    expect(body).toContain('9 day') // the deadline finding
    expect(body).toContain('record of processing') // the executor action label

    const log = await querySql<{ user_id: string }>(
      `select user_id from public.weekly_briefing_log where user_id = $1`,
      [pro.id],
    )
    expect(log).toHaveLength(1)

    // Second run in the same week sends nothing.
    const again = await dispatchWeeklyBriefing({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: pro.id,
      force: true,
    })
    expect(again).toMatchObject({ sent: 0, skipped: 1 })
    expect(email.sent).toHaveLength(1)
  })

  it('skips a Free-tier user', async () => {
    const admin = createServiceRoleClient()
    const { user } = await setup(admin)
    try {
      // default plan is free; ensure briefing enabled
      await admin.from('notification_preferences').upsert({ user_id: user.id, weekly_briefing_enabled: true })
      const email = createCapturingEmailProvider()
      const summary = await dispatchWeeklyBriefing({
        supabase: createServiceRoleClient(),
        emailProvider: email,
        baseUrl: 'https://app.kindlast.com',
        tokenSecret: 'integration-secret',
        userId: user.id,
        force: true,
      })
      expect(summary).toMatchObject({ sent: 0, skipped: 1 })
      expect(email.sent).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, user.id)
    }
  })

  it('skips an opted-out Pro user', async () => {
    const admin = createServiceRoleClient()
    const { user } = await setup(admin)
    try {
      await admin.from('subscriptions').update({ plan: 'pro' }).eq('user_id', user.id)
      await admin.from('notification_preferences').upsert({ user_id: user.id, weekly_briefing_enabled: false })
      const email = createCapturingEmailProvider()
      const summary = await dispatchWeeklyBriefing({
        supabase: createServiceRoleClient(),
        emailProvider: email,
        baseUrl: 'https://app.kindlast.com',
        tokenSecret: 'integration-secret',
        userId: user.id,
        force: true,
      })
      expect(summary).toMatchObject({ sent: 0, skipped: 1 })
      expect(email.sent).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, user.id)
    }
  })
})
