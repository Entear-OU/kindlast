// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-61 — severity adjustment, effort estimate, enum storage.
 *
 * Severity is derived from the obligation baseline, adjusted for proximity to
 * deadline and data sensitivity, never downgrading a Watcher escalation. Effort
 * is a by-kind minutes/hours/days estimate. Both are native enum columns so the
 * feed can `order by severity desc` and get critical first.
 *
 * Data sensitivity is a per-profile signal, so the suite uses two profiles for
 * one user: a non-sensitive one and one whose data_categories include health
 * data. Hermetic per the project convention — own signals only, swept by
 * obligation prefix in afterAll.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent61_'
const SUMMARY =
  'Fixture obligation used by the ENT-61 severity/effort integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

async function seedObligation(slug: string, severity: string): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}', '32016R0679', 'article', 30,
       '{}'::jsonb, '${severity}', null)
    on conflict (slug) do update set severity = excluded.severity;
  `)
}

describe.skipIf(!supabaseRunning)('analyst severity + effort (ENT-61)', () => {
  let user: TestUser
  let profilePlain: string // non-sensitive data categories
  let profileSensitive: string // health data → sensitivity bump

  const sig: Record<string, string> = {}

  async function makeProfile(admin: ReturnType<typeof createServiceRoleClient>, categories: string[]) {
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
        data_categories: categories,
      })
      .select('id')
      .single()
    return profile!.id as string
  }

  async function emitAndConvert(
    profileId: string,
    kind: string,
    dedup: string,
    severity: string,
    slug: string,
    metadata: Record<string, unknown>,
  ): Promise<string> {
    const [{ id }] = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, $2::text, $3::text, 'title', 'detail', $4::text, $5::text, $6::jsonb
       ) as id`,
      [profileId, kind, dedup, severity, slug, JSON.stringify(metadata)],
    )
    await querySql(`select public.analyst_convert_signal($1::uuid)`, [id])
    return id
  }

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)
    profilePlain = await makeProfile(admin, ['email addresses', 'names'])
    profileSensitive = await makeProfile(admin, ['health records'])

    await seedObligation(`${PREFIX}high`, 'high')
    await seedObligation(`${PREFIX}medium`, 'medium')
    await seedObligation(`${PREFIX}low`, 'low')

    // high baseline, far deadline, non-sensitive → stays high
    sig.baseline = await emitAndConvert(profilePlain, 'deadline', `${PREFIX}d1`, 'high', `${PREFIX}high`, { days_remaining: 20 })
    // medium baseline, 5 days out (<7 → +1) → high
    sig.proximity = await emitAndConvert(profilePlain, 'deadline', `${PREFIX}d2`, 'medium', `${PREFIX}medium`, { days_remaining: 5 })
    // medium baseline, 1 day out (<3 → +2) → critical
    sig.proxCritical = await emitAndConvert(profilePlain, 'deadline', `${PREFIX}d3`, 'medium', `${PREFIX}medium`, { days_remaining: 1 })
    // medium baseline, far deadline, sensitive profile (+1) → high
    sig.sensitive = await emitAndConvert(profileSensitive, 'deadline', `${PREFIX}d4`, 'medium', `${PREFIX}medium`, { days_remaining: 20 })
    // low baseline but signal already escalated to critical → not downgraded
    sig.noDowngrade = await emitAndConvert(profilePlain, 'dsar', `${PREFIX}d5`, 'critical', `${PREFIX}low`, {})
    // a profile gap → effort 'days'
    sig.gap = await emitAndConvert(profilePlain, 'profile_gap', `${PREFIX}d6`, 'medium', `${PREFIX}high`, { missing: ['ropa'] })
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

  const severityOf = async (signalId: string) =>
    (
      await querySql<{ severity: string }>(
        `select severity from public.findings where watcher_finding_id = $1::uuid`,
        [signalId],
      )
    )[0].severity

  const effortOf = async (signalId: string) =>
    (
      await querySql<{ effort_estimate: string }>(
        `select effort_estimate from public.findings where watcher_finding_id = $1::uuid`,
        [signalId],
      )
    )[0].effort_estimate

  it('keeps the obligation baseline when nothing adjusts it', async () => {
    expect(await severityOf(sig.baseline)).toBe('high')
  })

  it('bumps severity for proximity to deadline', async () => {
    expect(await severityOf(sig.proximity)).toBe('high') // medium +1 (5 days)
    expect(await severityOf(sig.proxCritical)).toBe('critical') // medium +2 (1 day)
  })

  it('bumps severity for sensitive data categories', async () => {
    expect(await severityOf(sig.sensitive)).toBe('high') // medium +1 (health)
  })

  it('never downgrades a Watcher escalation', async () => {
    expect(await severityOf(sig.noDowngrade)).toBe('critical') // low baseline, critical signal
  })

  it('sets a by-kind effort estimate', async () => {
    expect(await effortOf(sig.baseline)).toBe('days') // deadline
    expect(await effortOf(sig.noDowngrade)).toBe('hours') // dsar
    expect(await effortOf(sig.gap)).toBe('days') // profile_gap
  })

  it('stores severity + effort as native enum columns that sort by rank', async () => {
    const cols = await querySql<{ column_name: string; udt_name: string }>(
      `select column_name, udt_name from information_schema.columns
       where table_schema='public' and table_name='findings'
         and column_name in ('severity','effort_estimate')`,
    )
    const byName = Object.fromEntries(cols.map((c) => [c.column_name, c.udt_name]))
    expect(byName.severity).toBe('severity_level')
    expect(byName.effort_estimate).toBe('effort_level')

    // Enum declaration order = sort order: critical ranks first, not alphabetical.
    const [top] = await querySql<{ severity: string }>(
      `select severity from public.findings
       where profile_id = $1::uuid order by severity desc limit 1`,
      [profilePlain],
    )
    expect(top.severity).toBe('critical')
  })
})

describe.skipIf(!supabaseRunning)('notification preferences (ENT-61)', () => {
  let user: TestUser
  let other: TestUser

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)
    other = await signUpTestUser(admin)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (user?.id) await deleteTestUser(admin, user.id)
    if (other?.id) await deleteTestUser(admin, other.id)
  })

  it('defaults email_frequency to daily and is readable only by its owner', async () => {
    const owner = await createUserClient(user.email, user.password)
    const inserted = await owner.from('notification_preferences').insert({ user_id: user.id }).select('email_frequency').single()
    expect(inserted.error).toBeNull()
    expect(inserted.data!.email_frequency).toBe('daily') // enum default

    const own = await owner.from('notification_preferences').select('user_id').eq('user_id', user.id)
    expect(own.data ?? []).toHaveLength(1)

    const intruder = await createUserClient(other.email, other.password)
    const foreign = await intruder.from('notification_preferences').select('user_id').eq('user_id', user.id)
    expect(foreign.error).toBeNull()
    expect(foreign.data ?? []).toHaveLength(0) // RLS hides another user's prefs
  })
})
