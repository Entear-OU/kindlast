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
 * ENT-66 — Executor creates a ROPA entry on approval.
 *
 * When a founder approves a finding tagged `action_type='create_ropa'`, the
 * Executor creates exactly one `processing_activities` row, pre-filled from the
 * Analyst's payload, and records the write in the immutable audit log (ENT-69).
 *
 * Acceptance criteria exercised here:
 *   * Fires only when a create_ropa finding transitions to approved — not for
 *     other action_types, and not on a non-approval status change.
 *   * Pre-fills name, purpose, legal basis, data categories, recipients and
 *     retention period from the Analyst payload (findings.metadata->'payload').
 *   * An audit_log row is written carrying action type, finding id, profile id
 *     (in the after-snapshot), approving user, timestamp and before/after.
 *   * Re-approving never creates a second row (idempotent).
 *   * The new row is owner-readable under RLS, and approve_finding returns its id
 *     so the founder can be taken straight to it.
 *
 * Findings are produced through the real pipeline (emit a Watcher signal →
 * analyst_convert_signal) so the row satisfies every Analyst-era constraint
 * (obligation_id NOT NULL, watcher_finding_id unique), then tagged create_ropa
 * with a payload — exactly what the Analyst's classification will do upstream.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent66_'

const SUMMARY =
  'Fixture obligation for the ENT-66 executor ROPA test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

const ROPA_PAYLOAD = {
  name: 'Customer onboarding',
  purpose: 'Create accounts and run KYC checks',
  legal_basis: 'contract',
  data_categories: ['name', 'email', 'company'],
  recipients: ['Stripe', 'AWS'],
  retention_period: '24 months',
}

interface ProcessingActivity {
  id: string
  profile_id: string
  user_id: string
  finding_id: string | null
  name: string
  purpose: string | null
  legal_basis: string | null
  data_categories: string[]
  recipients: string[]
  retention_period: string | null
  [key: string]: unknown
}

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
// (profile_id, dedup_key) and analyst_convert_signal upserts the finding on
// watcher_finding_id, so a constant key would hand every test the same
// already-approved finding and the approval trigger would never re-fire.
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

/** Tag a finding as a create_ropa action carrying the given pre-fill payload. */
async function tagCreateRopa(findingId: string, payload: object): Promise<void> {
  await querySql(
    `update public.findings
       set action_type = 'create_ropa',
           metadata = metadata || jsonb_build_object('payload', $2::jsonb)
     where id = $1::uuid`,
    [findingId, JSON.stringify(payload)],
  )
}

const activitiesFor = (findingId: string) =>
  querySql<ProcessingActivity>(
    `select * from public.processing_activities where finding_id = $1::uuid`,
    [findingId],
  )

describe.skipIf(!supabaseRunning)('executor creates a ROPA entry on approval (ENT-66)', () => {
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

    await seedObligation(`${PREFIX}ropa`)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // processing_activities cascade with the profile/user; clear findings that
    // cite the fixture obligation (delete-protected) before dropping it.
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('creates one pre-filled processing_activities row on approval', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    await tagCreateRopa(findingId, ROPA_PAYLOAD)

    const target = await querySql<{ id: string | null }>(
      `select public.approve_finding($1::uuid, $2::uuid) as id`,
      [findingId, user.id],
    ).then((r) => r[0].id)

    const rows = await activitiesFor(findingId)
    expect(rows).toHaveLength(1)
    const pa = rows[0]

    // approve_finding hands back the new row's id (for "take me to the row").
    expect(target).toBe(pa.id)

    // Pre-filled from the Analyst payload.
    expect(pa.name).toBe(ROPA_PAYLOAD.name)
    expect(pa.purpose).toBe(ROPA_PAYLOAD.purpose)
    expect(pa.legal_basis).toBe(ROPA_PAYLOAD.legal_basis)
    expect(pa.data_categories).toEqual(ROPA_PAYLOAD.data_categories)
    expect(pa.recipients).toEqual(ROPA_PAYLOAD.recipients)
    expect(pa.retention_period).toBe(ROPA_PAYLOAD.retention_period)

    // Owned by the founder's profile.
    expect(pa.profile_id).toBe(profileId)
    expect(pa.user_id).toBe(user.id)
  })

  it('writes a matching audit_log entry with before/after and approving user', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    await tagCreateRopa(findingId, { ...ROPA_PAYLOAD, name: 'Marketing emails' })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    const [pa] = await activitiesFor(findingId)
    const [entry] = await querySql<{
      action_type: string
      finding_id: string
      target_table: string
      target_id: string
      approving_user_id: string
      before: unknown
      after: { profile_id: string; name: string } | null
      occurred_at: string
    }>(
      `select action_type, finding_id, target_table, target_id, approving_user_id,
              before, after, occurred_at
       from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )

    expect(entry.action_type).toBe('create_ropa')
    expect(entry.finding_id).toBe(findingId)
    expect(entry.target_table).toBe('processing_activities')
    expect(entry.target_id).toBe(pa.id)
    expect(entry.approving_user_id).toBe(user.id)
    expect(entry.before).toBeNull()
    // The after-snapshot is the whole new row — it carries the profile id.
    expect(entry.after?.profile_id).toBe(profileId)
    expect(entry.after?.name).toBe('Marketing emails')
    // Populated with a valid timestamp (a Date over the direct pg client).
    expect(Number.isNaN(new Date(entry.occurred_at).getTime())).toBe(false)
  })

  it('falls back to the finding text when the payload omits a name', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    // No name in the payload.
    await tagCreateRopa(findingId, { purpose: 'Support tickets', data_categories: [] })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    const [pa] = await activitiesFor(findingId)
    const [finding] = await querySql<{ detected: string }>(
      `select detected from public.findings where id = $1::uuid`,
      [findingId],
    )
    expect(pa.name).toBe(finding.detected)
    expect(pa.data_categories).toEqual([])
    expect(pa.recipients).toEqual([])
  })

  it('is idempotent — re-approving never creates a second row', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    await tagCreateRopa(findingId, ROPA_PAYLOAD)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    // Force the transition again (reset, then approve) — must not duplicate.
    await querySql(`update public.findings set status = 'pending' where id = $1::uuid`, [findingId])
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    expect(await activitiesFor(findingId)).toHaveLength(1)
    const entries = await querySql(
      `select 1 from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )
    expect(entries).toHaveLength(1) // still one audit entry
  })

  it('does not fire for a non-create_ropa finding', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    // Left as the default action_type 'review'.
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    expect(await activitiesFor(findingId)).toHaveLength(0)
    const entries = await querySql(
      `select 1 from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )
    expect(entries).toHaveLength(0)
  })

  it('does not fire on a non-approval status change', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    await tagCreateRopa(findingId, ROPA_PAYLOAD)
    // Snooze, not approve.
    await querySql(`update public.findings set status = 'snoozed' where id = $1::uuid`, [findingId])

    expect(await activitiesFor(findingId)).toHaveLength(0)
  })

  it('exposes the new row to its owner under RLS and hides it from others', async () => {
    const admin = createServiceRoleClient()
    const findingId = await makeFinding(profileId, `${PREFIX}ropa`)
    await tagCreateRopa(findingId, ROPA_PAYLOAD)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    const [pa] = await activitiesFor(findingId)

    const other = await signUpTestUser(admin)
    try {
      const ownerClient = await createUserClient(user.email, user.password)
      const owned = await ownerClient.from('processing_activities').select('id').eq('id', pa.id)
      expect(owned.error).toBeNull()
      expect((owned.data ?? []).map((r) => r.id)).toEqual([pa.id])

      const otherClient = await createUserClient(other.email, other.password)
      const foreign = await otherClient.from('processing_activities').select('id').eq('id', pa.id)
      expect(foreign.error).toBeNull()
      expect(foreign.data ?? []).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
