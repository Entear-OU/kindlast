// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { loadFindingContext, persistFindingNarrative } from '@/lib/analyst/persistence'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-60 — narrative persistence + replay preservation.
 *
 * The LLM generation itself is unit-tested with a mocked model
 * (__tests__/lib/analyst/narrative.test.ts). This integration suite covers the
 * DB-facing half against the real stack:
 *
 *   * loadFindingContext assembles the generator's context from the stored
 *     finding + obligation + profile (signal kind and metadata ride on
 *     findings.metadata from ENT-58; the citation label from ENT-59).
 *   * persistFindingNarrative writes detected / proposed_action /
 *     narrative_generated_at.
 *   * Re-running analyst_convert_signal PRESERVES the generated narrative (the
 *     migration's on-conflict change) — the daily Watcher loop must not revert a
 *     finding to its baseline once the Analyst has written the founder-facing
 *     text. Signal-derived fields (severity, citation) still refresh.
 *
 * Hermetic per the project convention: convert only our own signal, and sweep
 * findings by obligation prefix in afterAll (findings are delete-protected).
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent60_'

const SUMMARY =
  'Fixture obligation used by the ENT-60 narrative integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

const NARRATIVE = {
  description:
    'You have no Record of Processing Activities, which EU rules require for a payroll business handling staff data.',
  proposedAction: 'Publish a Record of Processing Activities.',
}

describe.skipIf(!supabaseRunning)('analyst finding narrative (ENT-60)', () => {
  let user: TestUser
  let profileId: string
  let signalId: string
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
        industry: 'SaaS payroll',
        has_dpo: 'no',
        has_ropa: 'no',
        transfers_outside_eu: 'no',
        vendor_list: 'Stripe, AWS',
      })
      .select('id')
      .single()
    profileId = profile!.id as string

    await applyFixtureSql(`
      insert into public.obligations
        (slug, title, summary, citation_celex, citation_kind, citation_article,
         applies_when, severity, effective_date)
      values
        ('${PREFIX}ropa', 'Records of Processing Activities', '${SUMMARY}',
         '32016R0679', 'article', 30, '{}'::jsonb, 'high', null)
      on conflict (slug) do update set title = excluded.title;
    `)

    const [{ id }] = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'profile_gap', 'gap:obligation:${PREFIX}ropa',
         'Profile gap: Records of Processing Activities', 'baseline detail', 'high',
         '${PREFIX}ropa', jsonb_build_object('missing', to_jsonb(array['ropa']), 'recurring', false)
       ) as id`,
      [profileId],
    )
    signalId = id
    await querySql(`select public.analyst_convert_signal($1::uuid)`, [signalId])

    const [{ id: fid }] = await querySql<{ id: string }>(
      `select id from public.findings where watcher_finding_id = $1::uuid`,
      [signalId],
    )
    findingId = fid
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

  it('assembles the generator context from the stored finding', async () => {
    const ctx = await loadFindingContext(createServiceRoleClient(), findingId)
    expect(ctx.signalKind).toBe('profile_gap')
    expect(ctx.obligationTitle).toBe('Records of Processing Activities')
    expect(ctx.citationLabel).toBe('GDPR Art. 30')
    expect(ctx.industry).toBe('SaaS payroll')
    expect(ctx.vendors).toBe('Stripe, AWS')
    expect(ctx.missingControls).toEqual(['ropa'])
    expect(ctx.obligationSummary).toBe(SUMMARY)
  })

  it('persists the generated narrative onto the finding', async () => {
    await persistFindingNarrative(createServiceRoleClient(), findingId, NARRATIVE)

    const [row] = await querySql<{
      detected: string
      proposed_action: string
      narrative_generated_at: string | null
    }>(
      `select detected, proposed_action, narrative_generated_at
       from public.findings where id = $1::uuid`,
      [findingId],
    )
    expect(row.detected).toBe(NARRATIVE.description)
    expect(row.proposed_action).toBe(NARRATIVE.proposedAction)
    expect(row.narrative_generated_at).not.toBeNull()
  })

  it('preserves the narrative when the signal is re-converted (daily loop)', async () => {
    // Re-run the conversion — as the Watcher's daily loop would.
    await querySql(`select public.analyst_convert_signal($1::uuid)`, [signalId])

    const [row] = await querySql<{
      detected: string
      proposed_action: string
      narrative_generated_at: string | null
      severity: string
      regulatory_obligation: string
    }>(
      `select detected, proposed_action, narrative_generated_at, severity, regulatory_obligation
       from public.findings where id = $1::uuid`,
      [findingId],
    )
    // Narrative preserved, not reverted to the baseline.
    expect(row.detected).toBe(NARRATIVE.description)
    expect(row.proposed_action).toBe(NARRATIVE.proposedAction)
    expect(row.narrative_generated_at).not.toBeNull()
    // Signal-derived fields still refresh.
    expect(row.severity).toBe('high')
    expect(row.regulatory_obligation).toBe('GDPR Art. 30')
  })
})
