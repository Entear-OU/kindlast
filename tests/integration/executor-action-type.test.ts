// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-165 — the Executor's discriminator actually reaches the finding.
 *
 * `findings.action_type` gates all three Executor triggers, and nothing ever
 * wrote it: every finding carried the `'review'` default, so approving one
 * flipped a status and created nothing. The mapping now lives on the
 * obligations catalogue and `analyst_convert_signal` copies it across.
 *
 * What matters, and what is asserted here:
 *
 *   * A mapped obligation produces a finding with that action_type.
 *   * An unmapped obligation still produces 'review', so nothing starts
 *     creating records by accident.
 *   * Approving the mapped finding actually reaches the Executor: a
 *     processing_activities row appears and an audit entry is written.
 *   * Retagging an obligation takes effect on re-convert without a backfill.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent165_'
const SUMMARY =
  'Fixture obligation for the ENT-165 Executor wiring test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function seedObligation(slug: string, actionType: string): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, action_type)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}',
       '32016R0679', 'article', 30, '{}'::jsonb, 'high', '${actionType}')
    on conflict (slug) do update set action_type = excluded.action_type;
  `)
}

async function convertGap(profileId: string, slug: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, 'Fixture gap detail.', 'high', $4::text,
       '{}'::jsonb
     ) as id`,
    [profileId, `${slug}:${(seq += 1)}`, `Gap: ${slug}`, slug],
  )
  const [{ id }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return id
}

const actionTypeOf = async (findingId: string) =>
  (
    await querySql<{ action_type: string }>(
      `select action_type from public.findings where id = $1::uuid`,
      [findingId],
    )
  )[0].action_type

describe.skipIf(!supabaseRunning)('Executor action_type wiring (ENT-165)', () => {
  let user: TestUser
  let profileId: string
  let mappedFindingId: string

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
        industry: 'E-commerce',
        has_dpo: 'no',
        has_ropa: 'no',
        transfers_outside_eu: 'no',
      })
      .select('id')
      .single()
    profileId = profile!.id as string

    await seedObligation(`${PREFIX}ropa`, 'create_ropa')
    await seedObligation(`${PREFIX}plain`, 'review')

    mappedFindingId = await convertGap(profileId, `${PREFIX}ropa`)
  })

  afterAll(async () => {
    if (user) await deleteTestUser(createServiceRoleClient(), user.id)
    await applyFixtureSql(`delete from public.obligations where slug like '${PREFIX}%';`)
  })

  it('copies a mapped obligation action_type onto the finding', async () => {
    expect(await actionTypeOf(mappedFindingId)).toBe('create_ropa')
  })

  it('leaves an unmapped obligation on review', async () => {
    const id = await convertGap(profileId, `${PREFIX}plain`)
    expect(await actionTypeOf(id)).toBe('review')
  })

  it('reaches the Executor on approval: record created and audit written', async () => {
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [
      mappedFindingId,
      user.id,
    ])

    const activities = await querySql<{ id: string }>(
      `select id from public.processing_activities where finding_id = $1::uuid`,
      [mappedFindingId],
    )
    expect(activities).toHaveLength(1)

    const audit = await querySql<{ action_type: string }>(
      `select action_type from public.audit_log
        where target_id = $1::uuid and target_table = 'processing_activities'`,
      [activities[0].id],
    )
    expect(audit.map((a) => a.action_type)).toContain('create_ropa')
  })

  it('picks up a retagged obligation on the next convert, with no backfill', async () => {
    await seedObligation(`${PREFIX}plain`, 'create_ropa')
    const id = await convertGap(profileId, `${PREFIX}plain`)
    expect(await actionTypeOf(id)).toBe('create_ropa')
  })
})
