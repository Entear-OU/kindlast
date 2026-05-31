// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-55 — Flag approaching deadlines within 30 days.
 *
 * The deadline detector that registers into the ENT-53 Watcher loop. Two
 * sources of "approaching deadline":
 *
 *   1. Obligations whose `effective_date` falls within the next 30 days,
 *      filtered to the profile's *applicable* obligations (applies_when).
 *   2. DSAR rows whose `response_due_at` falls within 30 days with no logged
 *      response.
 *
 * Each finding carries `days_remaining` (in metadata) and the obligation
 * reference (`obligation_slug`). Re-emission suppression is inherited from
 * ENT-53's open-finding idempotency, exercised here through the detector.
 *
 * Applicability note: `watcher_obligation_applies()` evaluates the
 * applies_when predicates that map to profile columns; indeterminate
 * predicates (e.g. high_risk) are warn-by-default. ENT-56 refines the
 * gap-specific semantics.
 *
 * Fixture obligations are inserted with a `_test_ent55_` slug prefix and
 * removed in afterAll so we never assert against the real seeded corpus
 * (whose effective dates are all historical or >30 days out).
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent55_'

const SUMMARY =
  'Fixture obligation used by the ENT-55 deadline detector integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

/** Insert a fixture obligation with a controlled effective_date / applies_when. */
async function seedObligation(opts: {
  slug: string
  effectiveDate: string | null
  appliesWhen: Record<string, unknown>
  severity?: string
}): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${opts.slug}', 'Fixture ${opts.slug}', '${SUMMARY}',
       '32016R0679', 'article', 99,
       '${JSON.stringify(opts.appliesWhen)}'::jsonb,
       '${opts.severity ?? 'high'}',
       ${opts.effectiveDate ? `'${opts.effectiveDate}'` : 'null'})
    on conflict (slug) do update set
      effective_date = excluded.effective_date,
      applies_when   = excluded.applies_when,
      severity       = excluded.severity;
  `)
}

describe.skipIf(!supabaseRunning)('watcher deadline detector (ENT-55)', () => {
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
        ai_systems: ['internal ChatGPT'],
        has_dpo: 'no',
        has_ropa: 'no',
        transfers_outside_eu: 'yes',
        staff_count: 40,
        vendor_list: 'Stripe, AWS',
      })
      .select('id')
      .single()
    expect(error).toBeNull()
    profileId = profile!.id as string

    // Obligations spanning the window boundaries (today = 2026-05-31).
    await seedObligation({
      slug: `${PREFIX}near`, // 15 days out → flagged
      effectiveDate: '2026-06-15',
      appliesWhen: { role: 'controller' },
    })
    await seedObligation({
      slug: `${PREFIX}past`, // already in force → not flagged
      effectiveDate: '2018-05-25',
      appliesWhen: { role: 'controller' },
    })
    await seedObligation({
      slug: `${PREFIX}far`, // >30 days out → not flagged
      effectiveDate: '2026-12-01',
      appliesWhen: { role: 'controller' },
    })
    await seedObligation({
      slug: `${PREFIX}notapplicable`, // near, but only applies to 250+ employee orgs
      effectiveDate: '2026-06-10',
      appliesWhen: { thresholds: { employees_min: 250 } },
    })
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`delete from public.obligations where slug like '${PREFIX}%';`)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('creates the user-owned dsars table with RLS', async () => {
    const [rls] = await querySql<{ relrowsecurity: boolean }>(
      `select relrowsecurity from pg_class where oid = 'public.dsars'::regclass`,
    )
    expect(rls.relrowsecurity).toBe(true)

    const cols = (
      await querySql<{ column_name: string }>(
        `select column_name from information_schema.columns
         where table_schema='public' and table_name='dsars'`,
      )
    ).map((c) => c.column_name)
    expect(cols).toEqual(
      expect.arrayContaining(['id', 'user_id', 'status', 'received_at', 'response_due_at', 'responded_at']),
    )
  })

  it('watcher_obligation_applies evaluates the predicates it can map', async () => {
    // role:controller applies to every profile.
    const [ctrl] = await querySql<{ applies: boolean }>(
      `select public.watcher_obligation_applies('{"role":"controller"}'::jsonb, p.*) as applies
       from public.compliance_profiles p where p.id = $1::uuid`,
      [profileId],
    )
    expect(ctrl.applies).toBe(true)

    // cross_border_transfers requires transfers_outside_eu='yes' (profile has 'yes').
    const [xb] = await querySql<{ applies: boolean }>(
      `select public.watcher_obligation_applies('{"thresholds":{"cross_border_transfers":true}}'::jsonb, p.*) as applies
       from public.compliance_profiles p where p.id = $1::uuid`,
      [profileId],
    )
    expect(xb.applies).toBe(true)

    // deployer requires a non-empty ai_systems (profile has one).
    const [dep] = await querySql<{ applies: boolean }>(
      `select public.watcher_obligation_applies('{"role":"deployer"}'::jsonb, p.*) as applies
       from public.compliance_profiles p where p.id = $1::uuid`,
      [profileId],
    )
    expect(dep.applies).toBe(true)
  })

  it('flags only applicable obligations whose effective_date is within 30 days', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const rows = await querySql<{
      obligation_slug: string
      kind: string
      metadata: { days_remaining: number }
    }>(
      `select obligation_slug, kind, metadata from public.watcher_findings
       where profile_id = $1::uuid and kind = 'deadline'
       order by obligation_slug`,
      [profileId],
    )
    const slugs = rows.map((r) => r.obligation_slug)

    expect(slugs).toContain(`${PREFIX}near`)
    expect(slugs).not.toContain(`${PREFIX}past`)
    expect(slugs).not.toContain(`${PREFIX}far`)
    expect(slugs).not.toContain(`${PREFIX}notapplicable`) // applies_when filtered it out

    const near = rows.find((r) => r.obligation_slug === `${PREFIX}near`)!
    expect(near.metadata.days_remaining).toBe(15) // 2026-06-15 − 2026-05-31
  })

  it('is idempotent — re-running the detector does not duplicate the open finding', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const [{ count }] = await querySql<{ count: string }>(
      `select count(*)::text as count from public.watcher_findings
       where profile_id = $1::uuid and obligation_slug = $2::text and status = 'open'`,
      [profileId, `${PREFIX}near`],
    )
    expect(count).toBe('1')
  })

  it('flags DSARs whose response_due_at is within 30 days with no logged response', async () => {
    const client = await createUserClient(user.email, user.password)

    // Due in 12 days, unanswered → flagged.
    const { data: dueSoon, error: e1 } = await client
      .from('dsars')
      .insert({
        user_id: user.id,
        subject_name: 'Alice',
        request_type: 'access',
        status: 'open',
        response_due_at: new Date(Date.now() + 12 * 86400_000).toISOString(),
      })
      .select('id')
      .single()
    expect(e1).toBeNull()

    // Due in 90 days → not flagged.
    await client.from('dsars').insert({
      user_id: user.id,
      subject_name: 'Bob',
      request_type: 'access',
      status: 'open',
      response_due_at: new Date(Date.now() + 90 * 86400_000).toISOString(),
    })

    // Due soon but already responded → not flagged.
    await client.from('dsars').insert({
      user_id: user.id,
      subject_name: 'Carol',
      request_type: 'erasure',
      status: 'in_progress',
      response_due_at: new Date(Date.now() + 5 * 86400_000).toISOString(),
      responded_at: new Date().toISOString(),
    })

    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const rows = await querySql<{ metadata: { dsar_id: string; days_remaining: number } }>(
      `select metadata from public.watcher_findings
       where profile_id = $1::uuid and kind = 'dsar' and status = 'open'`,
      [profileId],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].metadata.dsar_id).toBe(dueSoon!.id)
    expect(rows[0].metadata.days_remaining).toBeLessThanOrEqual(12)
  })

  it('enforces per-user RLS on dsars', async () => {
    const anon = createAnonClient()
    const { data: anonRows } = await anon.from('dsars').select('id')
    expect(anonRows ?? []).toHaveLength(0)
  })
})
