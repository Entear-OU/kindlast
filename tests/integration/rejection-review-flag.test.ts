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
 * ENT-65 — Repeated rejection of the same condition raises a one-time
 * product-review flag (PRD §14 Q4).
 *
 * "Same condition" = (profile_id, obligation_slug). The Analyst emits a fresh
 * finding per sweep, so a founder rejecting the same obligation again and again
 * produces distinct findings under one slug. On the THIRD rejection,
 * reject_finding() raises exactly one product_review_flags row (unique on the
 * condition, ON CONFLICT DO NOTHING), capturing the count and the distinct
 * non-null reasons. reject_finding keeps its ENT-63 behaviour otherwise.
 *
 * Findings are produced through the real pipeline (emit a Watcher signal →
 * analyst_convert_signal) so each row satisfies every Analyst-era constraint.
 * Each makeFinding call uses a distinct dedup_key but the SAME obligation_slug,
 * so every finding belongs to the same condition. Rejection goes through the
 * OWNER's user client so auth.uid() scoping is in the loop.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent65_'

const SUMMARY =
  'Fixture obligation for the ENT-65 rejection review-flag test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

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
// The obligation_slug stays constant so every finding is the SAME condition.
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

interface FlagRow {
  obligation_slug: string
  finding_id: string | null
  rejection_count: number
  reasons: string[]
  [key: string]: unknown
}

const flagRows = (profileId: string, slug: string) =>
  querySql<FlagRow>(
    `select obligation_slug, finding_id, rejection_count, reasons
       from public.product_review_flags
      where profile_id = $1::uuid and obligation_slug = $2::text`,
    [profileId, slug],
  )

interface FindingRow {
  status: string
  rejection_reason: string | null
  [key: string]: unknown
}

const findingRow = (id: string) =>
  querySql<FindingRow>(
    `select status, rejection_reason from public.findings where id = $1::uuid`,
    [id],
  ).then((r) => r[0])

describe.skipIf(!supabaseRunning)('repeated rejection raises a product-review flag (ENT-65)', () => {
  let user: TestUser
  let profileId: string
  const slug = `${PREFIX}gap`

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

    await seedObligation(slug)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`
      delete from public.product_review_flags where profile_id = '${profileId}';
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('rejecting two findings of the same condition raises NO flag (count < 3)', async () => {
    const owner = await createUserClient(user.email, user.password)

    const first = await makeFinding(profileId, slug)
    const second = await makeFinding(profileId, slug)

    await owner.rpc('reject_finding', { p_finding_id: first, p_reason: 'reason-A' })
    await owner.rpc('reject_finding', { p_finding_id: second, p_reason: 'reason-B' })

    const flags = await flagRows(profileId, slug)
    expect(flags).toHaveLength(0)
  })

  it('the THIRD rejection raises exactly one flag with count, reasons and finding_id', async () => {
    const owner = await createUserClient(user.email, user.password)

    // Two already rejected above; a third distinct finding trips the flag.
    const third = await makeFinding(profileId, slug)
    const { data: changed, error } = await owner.rpc('reject_finding', {
      p_finding_id: third,
      p_reason: 'reason-C',
    })
    expect(error).toBeNull()
    expect(changed).toBe(true)

    const flags = await flagRows(profileId, slug)
    expect(flags).toHaveLength(1)

    const flag = flags[0]
    expect(flag.obligation_slug).toBe(slug)
    expect(flag.rejection_count).toBeGreaterThanOrEqual(3)
    expect(flag.finding_id).toBe(third)
    // Distinct non-null reasons collected across the condition (order-independent).
    expect(flag.reasons).toEqual(expect.arrayContaining(['reason-A', 'reason-B', 'reason-C']))
  })

  it('a FOURTH rejection leaves exactly one flag (ON CONFLICT DO NOTHING)', async () => {
    const owner = await createUserClient(user.email, user.password)
    const before = (await flagRows(profileId, slug))[0]

    const fourth = await makeFinding(profileId, slug)
    await owner.rpc('reject_finding', { p_finding_id: fourth, p_reason: 'reason-D' })

    const flags = await flagRows(profileId, slug)
    expect(flags).toHaveLength(1)
    // Untouched: same finding_id and count as when first raised.
    expect(flags[0].finding_id).toBe(before.finding_id)
    expect(flags[0].rejection_count).toBe(before.rejection_count)
    expect(flags[0].reasons).not.toContain('reason-D')
  })

  it('reject_finding still returns true on first rejection and persists the reason', async () => {
    const owner = await createUserClient(user.email, user.password)
    const findingId = await makeFinding(profileId, slug)

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
})
