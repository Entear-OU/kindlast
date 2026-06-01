// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, createUserClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-73 — finding → notification_outbox enqueue trigger.
 *
 * Asserts the AC "one email per finding (deduplicated by finding id)": the
 * AFTER INSERT trigger on findings enqueues exactly one pending outbox row, the
 * UNIQUE(finding_id) constraint blocks a duplicate, and a user only sees their
 * own queued notifications under RLS. Findings are produced through the real
 * pipeline with a unique dedup key, swept by obligation prefix in afterAll.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent73_outbox_'
const SUMMARY =
  'Fixture obligation for the ENT-73 notification-outbox test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeFinding(profileId: string, slug: string): Promise<string> {
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
  return findingId
}

describe.skipIf(!supabaseRunning)('finding notification outbox (ENT-73)', () => {
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
        ('${PREFIX}ropa', 'Fixture ${PREFIX}ropa', '${SUMMARY}',
         '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
      on conflict (slug) do nothing;
    `)

    findingId = await makeFinding(profileId, `${PREFIX}ropa`)
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

  it('enqueues exactly one pending outbox row for the new finding', async () => {
    const rows = await querySql<{ status: string; user_id: string; channel: string }>(
      `select status, user_id, channel from public.notification_outbox where finding_id = $1`,
      [findingId],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].status).toBe('pending')
    expect(rows[0].channel).toBe('email')
    expect(rows[0].user_id).toBe(user.id)
  })

  it('rejects a duplicate row for the same finding (one email per finding)', async () => {
    await expect(
      querySql(
        `insert into public.notification_outbox (finding_id, user_id) values ($1, $2)`,
        [findingId, user.id],
      ),
    ).rejects.toThrow(/duplicate key|unique/i)
  })

  it('does not leak a queued notification to another user (RLS)', async () => {
    const ownerClient = await createUserClient(user.email, user.password)
    const { data: own } = await ownerClient
      .from('notification_outbox')
      .select('finding_id')
      .eq('finding_id', findingId)
    expect(own).toHaveLength(1)

    const admin = createServiceRoleClient()
    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const { data: leaked } = await otherClient
        .from('notification_outbox')
        .select('finding_id')
        .eq('finding_id', findingId)
      expect(leaked).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
