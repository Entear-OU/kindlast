// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-56 — Detect profile gaps that map to known obligations.
 *
 * The second detector in the ENT-53 Watcher loop. Where ENT-55 watches the
 * calendar, this watches the *profile*: a static gap is an obligation that
 * applies to the org whose corresponding control is not in place — e.g.
 * has_dpo != 'yes' against the Article 37 DPO obligation. These produce
 * day-one findings without waiting on any regulatory change.
 *
 * Rules are data, not code (AC): an obligation opts into gap detection by
 * listing the controls it needs in `applies_when.requires` (a token array).
 * Each token maps to a profile signal via `watcher_gap_satisfied()`:
 *
 *   * 'ropa'                → has_ropa = 'yes'
 *   * 'dpo'                 → has_dpo  = 'yes'
 *   * 'ai_register'         → operates no AI systems (using AI with no
 *                             register entry is the gap)
 *   * 'transfer_safeguards' → at least one transfer destination documented
 *   * unknown token         → ignored (never fabricates a gap), logged
 *
 * Applicability reuses the shared `watcher_obligation_applies()` predicate,
 * so an obligation that doesn't apply to the profile can't raise a gap.
 *
 * Re-surface rule (AC): a gap the user dismissed is raised again — but only
 * while it still exists, and with a different ("recurring") message — by
 * inserting a fresh open finding once the dismissed one frees the dedup key.
 * Open-finding idempotency is inherited from ENT-53.
 *
 * Fixture obligations carry a `_test_ent56_` slug prefix and are removed in
 * afterAll; the test DB has no seeded corpus, so these are the only
 * `requires`-bearing rows in play.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent56_'

const SUMMARY =
  'Fixture obligation used by the ENT-56 gap detector integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

/** Insert a fixture obligation with a controlled applies_when (incl. requires). */
async function seedObligation(opts: {
  slug: string
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
       '${opts.severity ?? 'medium'}',
       null)
    on conflict (slug) do update set
      applies_when = excluded.applies_when,
      severity     = excluded.severity;
  `)
}

describe.skipIf(!supabaseRunning)('watcher gap detector (ENT-56)', () => {
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

    // Profile is missing a DPO and transfer safeguards, uses AI, but *has* a
    // ROPA — so 'ropa'-gated obligations are satisfied while the rest gap.
    const { data: profile, error } = await admin
      .from('compliance_profiles')
      .insert({
        session_id: session!.id,
        user_id: user.id,
        industry: 'SaaS',
        ai_systems: ['internal ChatGPT'],
        has_dpo: 'no',
        has_ropa: 'yes',
        transfers_outside_eu: 'yes',
        transfer_destinations: [],
        staff_count: 40,
        vendor_list: 'Stripe, AWS',
      })
      .select('id')
      .single()
    expect(error).toBeNull()
    profileId = profile!.id as string

    await seedObligation({ slug: `${PREFIX}ropa`, appliesWhen: { role: 'controller', requires: ['ropa'] } }) // satisfied → no gap
    await seedObligation({ slug: `${PREFIX}dpo`, appliesWhen: { role: 'controller', requires: ['dpo'] } }) // gap
    await seedObligation({ slug: `${PREFIX}aireg`, appliesWhen: { role: 'deployer', requires: ['ai_register'] } }) // gap (uses AI)
    await seedObligation({
      slug: `${PREFIX}transfers`,
      appliesWhen: { role: 'controller', thresholds: { cross_border_transfers: true }, requires: ['transfer_safeguards'] },
    }) // gap (no destinations)
    await seedObligation({
      slug: `${PREFIX}notapplicable`,
      appliesWhen: { thresholds: { employees_min: 250 }, requires: ['dpo'] },
    }) // dpo gap exists, but obligation doesn't apply to a 40-person org
    await seedObligation({
      slug: `${PREFIX}unknown`,
      appliesWhen: { role: 'controller', requires: ['totally_unknown_token'] },
    }) // unknown token → ignored, never a gap
    await seedObligation({ slug: `${PREFIX}resurface`, appliesWhen: { role: 'controller', requires: ['dpo'] } }) // used by the re-surface test only
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`delete from public.obligations where slug like '${PREFIX}%';`)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('watcher_gap_satisfied maps each known token to the right profile signal', async () => {
    const check = async (token: string) => {
      const [row] = await querySql<{ satisfied: boolean }>(
        `select public.watcher_gap_satisfied($2::text, p.*) as satisfied
         from public.compliance_profiles p where p.id = $1::uuid`,
        [profileId, token],
      )
      return row.satisfied
    }

    expect(await check('ropa')).toBe(true) // has_ropa = 'yes'
    expect(await check('dpo')).toBe(false) // has_dpo = 'no'
    expect(await check('ai_register')).toBe(false) // operates an AI system
    expect(await check('transfer_safeguards')).toBe(false) // no destinations documented
    expect(await check('totally_unknown_token')).toBe(true) // unknown ⇒ ignored, never a gap
  })

  it('flags applicable obligations whose required control is missing', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const rows = await querySql<{
      obligation_slug: string
      kind: string
      severity: string
      metadata: { missing: string[]; recurring: boolean }
    }>(
      `select obligation_slug, kind, severity, metadata from public.watcher_findings
       where profile_id = $1::uuid and kind = 'profile_gap'
       order by obligation_slug`,
      [profileId],
    )
    const slugs = rows.map((r) => r.obligation_slug)

    expect(slugs).toContain(`${PREFIX}dpo`)
    expect(slugs).toContain(`${PREFIX}aireg`)
    expect(slugs).toContain(`${PREFIX}transfers`)
    expect(slugs).not.toContain(`${PREFIX}ropa`) // control in place
    expect(slugs).not.toContain(`${PREFIX}notapplicable`) // obligation doesn't apply
    expect(slugs).not.toContain(`${PREFIX}unknown`) // unrecognised token ignored

    const dpo = rows.find((r) => r.obligation_slug === `${PREFIX}dpo`)!
    expect(dpo.kind).toBe('profile_gap')
    expect(dpo.metadata.missing).toEqual(['dpo'])
    expect(dpo.metadata.recurring).toBe(false)
  })

  it('is idempotent — re-running the detector does not duplicate the open finding', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const [{ count }] = await querySql<{ count: string }>(
      `select count(*)::text as count from public.watcher_findings
       where profile_id = $1::uuid and obligation_slug = $2::text and status = 'open'`,
      [profileId, `${PREFIX}dpo`],
    )
    expect(count).toBe('1')
  })

  it('re-surfaces a dismissed gap with a different message while it still exists', async () => {
    const key = `gap:obligation:${PREFIX}resurface`

    // First run: a fresh, first-time finding.
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    const [first] = await querySql<{ id: string; title: string; metadata: { recurring: boolean } }>(
      `select id, title, metadata from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text and status = 'open'`,
      [profileId, key],
    )
    expect(first.metadata.recurring).toBe(false)
    expect(first.title).toMatch(/^Profile gap:/)

    // The user rejects it.
    await querySql(
      `update public.watcher_findings set status = 'dismissed' where id = $1::uuid`,
      [first.id],
    )

    // Next run: the gap is still present, so it re-surfaces as a *new* open
    // finding with a different, recurring message.
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const open = await querySql<{ id: string; title: string; metadata: { recurring: boolean } }>(
      `select id, title, metadata from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text and status = 'open'`,
      [profileId, key],
    )
    expect(open).toHaveLength(1)
    expect(open[0].id).not.toBe(first.id) // a fresh finding, not the dismissed one
    expect(open[0].metadata.recurring).toBe(true)
    expect(open[0].title).toMatch(/^Recurring gap:/)
    expect(open[0].title).not.toBe(first.title)

    // The dismissed finding stays as history → two rows for this gap total.
    const [{ count }] = await querySql<{ count: string }>(
      `select count(*)::text as count from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text`,
      [profileId, key],
    )
    expect(count).toBe('2')
  })
})
