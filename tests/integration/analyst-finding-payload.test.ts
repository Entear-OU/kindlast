// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-171 — the Analyst writes the Executor's pre-fill payload.
 *
 * The three Executor triggers build their row from `findings.metadata->'payload'`
 * and fall back to `new.detected` for the name. Nothing ever wrote that payload,
 * so the fallback was the only path and every ratified record came out blank and
 * titled with the gap sentence, e.g. a ROPA activity called "Profile gap:
 * Records of Processing Activities (ROPA)".
 *
 * ENT-66's suite tags a finding with a payload by hand before approving, which
 * is why it stayed green while production never produced one. These assert the
 * payload at its source: `analyst_convert_signal`.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent171_'

const SUMMARY =
  'Fixture obligation for the ENT-171 analyst payload test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

const DATA_CATEGORIES = ['names', 'work emails', 'performance scores']
const DATA_SUBJECTS = ['employees', 'managers']

interface FindingRow {
  id: string
  detected: string
  action_type: string
  metadata: { payload?: Record<string, unknown> }
  [key: string]: unknown
}

async function seedObligation(slug: string, actionType: string, article: number): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date, action_type)
    values
      ('${slug}', 'Records of Processing Activities', '${SUMMARY}',
       '32016R0679', 'article', ${article}, '{"role":"controller"}'::jsonb, 'high', null,
       '${actionType}')
    on conflict (slug) do update set action_type = excluded.action_type;
  `)
}

let findingSeq = 0

/** Emit a Watcher signal and convert it, returning the resulting finding row. */
async function convertSignal(profileId: string, slug: string): Promise<FindingRow> {
  const dedupKey = `gap:${slug}:${(findingSeq += 1)}`
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
     ) as id`,
    [
      profileId,
      dedupKey,
      'Profile gap: Records of Processing Activities (ROPA)',
      'A ROPA entry is missing for this activity.',
      slug,
    ],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  const [finding] = await querySql<FindingRow>(
    `select id, detected, action_type, metadata from public.findings where id = $1::uuid`,
    [findingId],
  )
  return finding
}

describe.skipIf(!supabaseRunning)('analyst writes the executor payload (ENT-171)', () => {
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
        industry: 'HR analytics SaaS',
        data_categories: DATA_CATEGORIES,
        data_subjects: DATA_SUBJECTS,
        vendor_list: 'ChatGPT, GitHub Copilot',
        has_dpo: 'no',
        has_ropa: 'no',
        transfers_outside_eu: 'no',
      })
      .select('id')
      .single()
    expect(error).toBeNull()
    profileId = profile!.id as string

    await seedObligation(`${PREFIX}ropa`, 'create_ropa', 30)
    await seedObligation(`${PREFIX}review`, 'review', 37)
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

  it('names a create_ropa payload after the processing, not the gap sentence', async () => {
    const finding = await convertSignal(profileId, `${PREFIX}ropa`)
    const payload = finding.metadata.payload

    expect(payload).toBeDefined()
    expect(payload!.name).not.toBe(finding.detected)
    expect(String(payload!.name)).not.toMatch(/profile gap/i)
    // Derived from the profile's own data subjects.
    expect(payload!.name).toBe('Processing of employees, managers data')
  })

  it('carries the profile facts it actually knows, and no invented ones', async () => {
    const finding = await convertSignal(profileId, `${PREFIX}ropa`)
    const payload = finding.metadata.payload!

    expect(payload.data_categories).toEqual(DATA_CATEGORIES)
    expect(payload.recipients).toEqual(['ChatGPT', 'GitHub Copilot'])
    // Purpose, legal basis and retention are not derivable from the profile, so
    // the founder fills them in rather than the Analyst guessing.
    expect(payload.purpose ?? null).toBeNull()
    expect(payload.legal_basis ?? null).toBeNull()
    expect(payload.retention_period ?? null).toBeNull()
  })

  it('falls back to the obligation title for a non-ropa action type', async () => {
    const finding = await convertSignal(profileId, `${PREFIX}review`)
    const payload = finding.metadata.payload!

    expect(payload.name).toBe('Records of Processing Activities')
    expect(payload.name).not.toBe(finding.detected)
  })

  it('produces a usable ROPA row end to end, with no hand-tagged payload', async () => {
    const finding = await convertSignal(profileId, `${PREFIX}ropa`)

    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [finding.id, user.id])

    const [activity] = await querySql<{
      name: string
      data_categories: string[]
      recipients: string[]
    }>(
      `select name, data_categories, recipients
         from public.processing_activities where finding_id = $1::uuid`,
      [finding.id],
    )

    expect(activity.name).toBe('Processing of employees, managers data')
    expect(activity.data_categories).toEqual(DATA_CATEGORIES)
    expect(activity.recipients).toEqual(['ChatGPT', 'GitHub Copilot'])
  })

  it('keeps the payload fresh when a signal is re-converted', async () => {
    const dedupKey = `gap:${PREFIX}ropa:stable`
    const [{ id: signalId }] = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
       ) as id`,
      [profileId, dedupKey, 'Profile gap: ROPA', 'Missing.', `${PREFIX}ropa`],
    )

    await querySql(`select public.analyst_convert_signal($1::uuid)`, [signalId])
    const [{ id: findingId }] = await querySql<{ id: string }>(
      `select public.analyst_convert_signal($1::uuid) as id`,
      [signalId],
    )

    const [finding] = await querySql<FindingRow>(
      `select id, detected, action_type, metadata from public.findings where id = $1::uuid`,
      [findingId],
    )
    expect(finding.metadata.payload!.name).toBe('Processing of employees, managers data')
    // The signal context the Analyst already recorded survives alongside it.
    expect(finding.metadata).toHaveProperty('signal_kind', 'profile_gap')
  })
})
