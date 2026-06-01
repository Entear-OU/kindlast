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
 * ENT-63 — Reject / Snooze the founder applies to a finding from the feed.
 *
 * Both writes go through SECURITY DEFINER RPCs whose actor is auth.uid() (never a
 * trusted parameter), scoped to the caller's own rows — so the client cannot
 * touch another user's finding. Snoozes re-emerge via a system sweep
 * (`expire_snoozed_findings`), the directly-callable body the daily cron runs.
 *
 * Acceptance criteria exercised here:
 *   * reject sets status='rejected' and persists an optional reason (empty → null)
 *   * snooze sets status='snoozed' with snoozed_until = now + N days (default 7)
 *   * a foreign caller cannot reject/snooze a finding it does not own
 *   * expired snoozes re-emerge as pending; future snoozes and non-snoozed rows
 *     are left untouched
 *
 * Findings are produced through the real pipeline (emit a Watcher signal →
 * analyst_convert_signal) so each row satisfies every Analyst-era constraint.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent63_'

const SUMMARY =
  'Fixture obligation for the ENT-63 reject/snooze test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

async function seedObligation(slug: string): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}',
       '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
    on conflict (slug) do nothing;
  `)
}

// Each call needs a distinct signal: emit_watcher_finding dedups on
// (profile_id, dedup_key) and analyst_convert_signal upserts on
// watcher_finding_id, so a constant key would hand every test the same finding.
let findingSeq = 0

/** Emit a signal and convert it to a finding, returning the finding id. */
async function makeFinding(profileId: string, slug: string): Promise<string> {
  const dedupKey = `gap:${slug}:${(findingSeq += 1)}`
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
     ) as id`,
    [profileId, dedupKey, `Profile gap: ${slug}`, 'A ROPA entry is missing for this activity.', slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return findingId
}

interface FindingRow {
  status: string
  rejection_reason: string | null
  snoozed_until: string | null
}

const findingRow = (id: string) =>
  querySql<FindingRow>(
    `select status, rejection_reason, snoozed_until from public.findings where id = $1::uuid`,
    [id],
  ).then((r) => r[0])

describe.skipIf(!supabaseRunning)('reject / snooze a finding from the feed (ENT-63)', () => {
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
    const { data: profile, error } = await admin
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
    expect(error).toBeNull()
    profileId = profile!.id as string

    await seedObligation(`${PREFIX}gap`)
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

  it('reject sets status=rejected and persists the reason', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}gap`)
    const owner = await createUserClient(user.email, user.password)

    const { data: changed, error } = await owner.rpc('reject_finding', {
      p_finding_id: findingId,
      p_reason: 'Not in scope — handled by our processor',
    })
    expect(error).toBeNull()
    expect(changed).toBe(true)

    const row = await findingRow(findingId)
    expect(row.status).toBe('rejected')
    expect(row.rejection_reason).toBe('Not in scope — handled by our processor')
  })

  it('reject with an empty / omitted reason stores null', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}gap`)
    const owner = await createUserClient(user.email, user.password)

    const { error } = await owner.rpc('reject_finding', {
      p_finding_id: findingId,
      p_reason: '   ',
    })
    expect(error).toBeNull()

    const row = await findingRow(findingId)
    expect(row.status).toBe('rejected')
    expect(row.rejection_reason).toBeNull()
  })

  it('snooze sets status=snoozed with snoozed_until ≈ now + 7 days by default', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}gap`)
    const owner = await createUserClient(user.email, user.password)

    const { data: until, error } = await owner.rpc('snooze_finding', {
      p_finding_id: findingId,
    })
    expect(error).toBeNull()

    const row = await findingRow(findingId)
    expect(row.status).toBe('snoozed')
    const days = (new Date(until as string).getTime() - Date.now()) / 86_400_000
    expect(days).toBeGreaterThan(6.9)
    expect(days).toBeLessThan(7.1)
  })

  it('snooze honours a custom duration', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}gap`)
    const owner = await createUserClient(user.email, user.password)

    const { data: until, error } = await owner.rpc('snooze_finding', {
      p_finding_id: findingId,
      p_days: 30,
    })
    expect(error).toBeNull()
    const days = (new Date(until as string).getTime() - Date.now()) / 86_400_000
    expect(days).toBeGreaterThan(29.9)
    expect(days).toBeLessThan(30.1)
  })

  it('a foreign caller cannot reject or snooze a finding it does not own', async () => {
    const admin = createServiceRoleClient()
    const findingId = await makeFinding(profileId, `${PREFIX}gap`)
    const other = await signUpTestUser(admin)
    try {
      const intruder = await createUserClient(other.email, other.password)

      const reject = await intruder.rpc('reject_finding', { p_finding_id: findingId })
      expect(reject.error).toBeNull()
      expect(reject.data).toBe(false) // no row matched the intruder's user_id

      const snooze = await intruder.rpc('snooze_finding', { p_finding_id: findingId })
      expect(snooze.error).toBeNull()
      expect(snooze.data).toBeNull() // nothing snoozed

      // Finding is untouched.
      const row = await findingRow(findingId)
      expect(row.status).toBe('pending')
      expect(row.snoozed_until).toBeNull()
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })

  it('expire_snoozed_findings re-emerges only expired snoozes as pending', async () => {
    const expired = await makeFinding(profileId, `${PREFIX}gap`)
    const future = await makeFinding(profileId, `${PREFIX}gap`)
    const pending = await makeFinding(profileId, `${PREFIX}gap`)

    // An expired snooze (set directly — the RPC always snoozes into the future).
    await querySql(
      `update public.findings
         set status = 'snoozed', snoozed_until = now() - interval '1 hour'
       where id = $1::uuid`,
      [expired],
    )
    // A still-active snooze.
    await querySql(
      `update public.findings
         set status = 'snoozed', snoozed_until = now() + interval '7 days'
       where id = $1::uuid`,
      [future],
    )

    const [{ n }] = await querySql<{ n: number }>(
      `select public.expire_snoozed_findings() as n`,
    )
    expect(Number(n)).toBeGreaterThanOrEqual(1)

    expect((await findingRow(expired)).status).toBe('pending')
    expect((await findingRow(expired)).snoozed_until).toBeNull()
    expect((await findingRow(future)).status).toBe('snoozed')
    expect((await findingRow(pending)).status).toBe('pending')
  })
})
