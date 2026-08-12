// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { dispatchDeadlineAlerts } from '@/lib/notifications/deadline-dispatch'
import { dispatchPendingNotifications } from '@/lib/notifications/dispatch'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-75 — deadline alert dispatcher against the live stack.
 *
 * A deadline finding at 14 days fires once (subject names the obligation + "14"
 * days); a second run dedups; dropping days-remaining to 7 fires once more. The
 * generic ENT-73 finding dispatcher skips the same finding (deadline alerts own
 * it). Findings are produced through the real Watcher→Analyst pipeline.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent75_'
const SUMMARY =
  'Fixture obligation for the ENT-75 deadline-alert test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeDeadlineFinding(profileId: string, slug: string, days: number): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'deadline', $2::text, $3::text, 'Effective date approaching.', 'high', $4::text,
       jsonb_build_object('days_remaining', $5::int, 'effective_date', (current_date + $5::int))
     ) as id`,
    [profileId, `deadline:${slug}:${(seq += 1)}`, `Deadline: ${slug}`, slug, days],
  )
  const [{ id }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return id
}

async function setDays(findingId: string, days: number) {
  // Refresh the finding's live days-remaining (as the daily Analyst re-convert would).
  await querySql(
    `update public.findings
        set metadata = jsonb_set(metadata, '{signal_metadata,days_remaining}', to_jsonb($2::int))
      where id = $1`,
    [findingId, days],
  )
}

const dispatch = (userId: string) =>
  dispatchDeadlineAlerts({
    supabase: createServiceRoleClient(),
    emailProvider: createCapturingEmailProvider(),
    baseUrl: 'https://app.kindlast.com',
    tokenSecret: 'integration-secret',
    userId,
  })

describe.skipIf(!supabaseRunning)('deadline alert dispatcher (ENT-75)', () => {
  let user: TestUser
  let profileId: string
  let findingId: string

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

    findingId = await makeDeadlineFinding(profileId, `${PREFIX}ob`, 14)
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

  it('fires once at the 14-day threshold, then dedups', async () => {
    const email = createCapturingEmailProvider()
    const summary = await dispatchDeadlineAlerts({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: user.id,
    })
    expect(summary).toMatchObject({ sent: 1, failed: 0 })
    expect(email.sent).toHaveLength(1)
    expect(email.sent[0].to).toBe(user.email)
    expect(email.sent[0].subject).toMatch(/^\[Deadline\]/)
    expect(email.sent[0].subject).toContain('GDPR Art. 30') // the resolved obligation
    expect(email.sent[0].subject).toContain('14 days left')

    const log = await querySql<{ threshold: number }>(
      `select threshold from public.deadline_alert_log where finding_id = $1`,
      [findingId],
    )
    expect(log.map((r) => r.threshold)).toEqual([14])

    // Second run: same threshold, no further send.
    const again = await dispatch(user.id)
    expect(again.sent).toBe(0)
  })

  it('fires again when the finding crosses into the 7-day bucket', async () => {
    await setDays(findingId, 7)
    const email = createCapturingEmailProvider()
    const summary = await dispatchDeadlineAlerts({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: user.id,
    })
    expect(summary.sent).toBe(1)
    expect(email.sent[0].subject).toContain('7 days left')

    const log = await querySql<{ threshold: number }>(
      `select threshold from public.deadline_alert_log where finding_id = $1 order by threshold`,
      [findingId],
    )
    expect(log.map((r) => r.threshold)).toEqual([7, 14])
  })

  it('the generic finding dispatcher skips this deadline finding', async () => {
    // Reset its outbox row to pending so the dispatcher re-evaluates it.
    await querySql(
      `update public.notification_outbox set status = 'pending', sent_at = null where finding_id = $1`,
      [findingId],
    )
    const email = createCapturingEmailProvider()
    await dispatchPendingNotifications({
      supabase: createServiceRoleClient(),
      emailProvider: email,
      baseUrl: 'https://app.kindlast.com',
      tokenSecret: 'integration-secret',
      userId: user.id,
    })
    const [{ status, last_error }] = await querySql<{ status: string; last_error: string | null }>(
      `select status, last_error from public.notification_outbox where finding_id = $1`,
      [findingId],
    )
    expect(status).toBe('skipped')
    expect(last_error ?? '').toContain('deadline')
    // No generic email for this finding.
    expect(email.sent.find((m) => m.to === user.email && !m.subject.includes('Deadline'))).toBeUndefined()
  })
})
