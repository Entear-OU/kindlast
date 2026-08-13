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
 * ENT-68 — Executor creates an AI Systems Register entry on approval.
 *
 * When a founder approves a finding tagged `action_type='create_ai_system'`, the
 * Executor creates one `ai_systems` row carrying the Analyst's proposed EU AI Act
 * risk classification, and records the write in the immutable audit log (ENT-69).
 *
 * Acceptance criteria exercised here:
 *   * Inserts an ai_systems row with name, vendor, purpose, risk classification
 *     and documentation_status.
 *   * A High-Risk classification requires a *reviewed* approval (PRD §10): a plain
 *     approval is rejected, and the row is only created once confirmed.
 *   * An audit_log row is written.
 *
 * Reuses the ENT-66 machinery (create_ai_system action_type, approve_finding,
 * record_audit_log). Findings are produced through the real pipeline.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent68_'

const SUMMARY =
  'Fixture obligation for the ENT-68 executor AI-systems test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

interface AiSystemRow {
  id: string
  profile_id: string
  user_id: string
  finding_id: string | null
  name: string
  vendor: string | null
  purpose: string | null
  risk_classification: string
  documentation_status: string
  last_reviewed_at: string | null
  [key: string]: unknown
}

async function seedObligation(slug: string): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}',
       '32024R1689', 'article', 6, '{"role":"deployer"}'::jsonb, 'high', null)
    on conflict (slug) do nothing;
  `)
}

// Each call needs a distinct signal: emit_watcher_finding dedups on
// (profile_id, dedup_key) and analyst_convert_signal upserts the finding on
// watcher_finding_id, so a constant key would hand every test the same
// already-approved finding and the approval trigger would never re-fire.
let findingSeq = 0

async function makeFinding(profileId: string, slug: string): Promise<string> {
  const dedupKey = `gap:${slug}:${(findingSeq += 1)}`
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
     ) as id`,
    [profileId, dedupKey, `Profile gap: ${slug}`, 'An AI system needs registering.', slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return findingId
}

async function tagCreateAiSystem(findingId: string, payload: object): Promise<void> {
  await querySql(
    `update public.findings
       set action_type = 'create_ai_system',
           metadata = metadata || jsonb_build_object('payload', $2::jsonb)
     where id = $1::uuid`,
    [findingId, JSON.stringify(payload)],
  )
}

const systemsFor = (findingId: string) =>
  querySql<AiSystemRow>(`select * from public.ai_systems where finding_id = $1::uuid`, [findingId])

describe.skipIf(!supabaseRunning)('executor creates an AI Systems Register entry on approval (ENT-68)', () => {
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

    await seedObligation(`${PREFIX}ai`)
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

  it('creates one ai_systems row with the proposed classification on approval', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, {
      name: 'Resume Screener',
      vendor: 'Acme AI',
      purpose: 'Rank job applicants',
      risk_classification: 'limited',
      documentation_status: 'in_progress',
    })

    const target = await querySql<{ id: string | null }>(
      `select public.approve_finding($1::uuid, $2::uuid) as id`,
      [findingId, user.id],
    ).then((r) => r[0].id)

    const rows = await systemsFor(findingId)
    expect(rows).toHaveLength(1)
    const sys = rows[0]
    expect(target).toBe(sys.id)
    expect(sys.name).toBe('Resume Screener')
    expect(sys.vendor).toBe('Acme AI')
    expect(sys.purpose).toBe('Rank job applicants')
    expect(sys.risk_classification).toBe('limited')
    expect(sys.documentation_status).toBe('in_progress')
    expect(sys.last_reviewed_at).not.toBeNull()
    expect(sys.profile_id).toBe(profileId)
  })

  it('writes a matching audit_log entry carrying the classification', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, {
      name: 'Chatbot',
      risk_classification: 'minimal',
    })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    const [sys] = await systemsFor(findingId)
    const [entry] = await querySql<{
      action_type: string
      target_table: string
      target_id: string
      approving_user_id: string
      before: unknown
      after: { risk_classification: string; profile_id: string } | null
    }>(
      `select action_type, target_table, target_id, approving_user_id, before, after
       from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )
    expect(entry.action_type).toBe('create_ai_system')
    expect(entry.target_table).toBe('ai_systems')
    expect(entry.target_id).toBe(sys.id)
    expect(entry.approving_user_id).toBe(user.id)
    expect(entry.before).toBeNull()
    expect(entry.after?.risk_classification).toBe('minimal')
    expect(entry.after?.profile_id).toBe(profileId)
  })

  it('falls back to unclassified + missing docs when the payload omits them', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, { name: 'Mystery model' })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    const [sys] = await systemsFor(findingId)
    expect(sys.risk_classification).toBe('unclassified')
    expect(sys.documentation_status).toBe('missing')
  })

  it('rejects a plain approval of a High-Risk system — reviewed approval required', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, {
      name: 'Biometric ID',
      risk_classification: 'high',
    })

    // Plain (non-reviewed) approval must be rejected, and nothing is created.
    await expect(
      querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id]),
    ).rejects.toThrow(/reviewed approval/i)

    expect(await systemsFor(findingId)).toHaveLength(0)
    // The transition rolled back — the finding is still pending.
    const [{ status }] = await querySql<{ status: string }>(
      `select status from public.findings where id = $1::uuid`,
      [findingId],
    )
    expect(status).toBe('pending')
  })

  it('creates the High-Risk system once a reviewed approval confirms it', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, {
      name: 'Credit Scorer',
      vendor: 'FinML',
      risk_classification: 'high',
    })

    // Reviewed approval (third arg true) ratifies the High-Risk classification.
    const target = await querySql<{ id: string | null }>(
      `select public.approve_finding($1::uuid, $2::uuid, true) as id`,
      [findingId, user.id],
    ).then((r) => r[0].id)

    const rows = await systemsFor(findingId)
    expect(rows).toHaveLength(1)
    expect(rows[0].risk_classification).toBe('high')
    expect(target).toBe(rows[0].id)

    const [entry] = await querySql<{ action_type: string }>(
      `select action_type from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )
    expect(entry.action_type).toBe('create_ai_system')
  })

  it('is idempotent — re-approving never creates a second system', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, { name: 'Recommender', risk_classification: 'minimal' })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    await querySql(`update public.findings set status = 'pending' where id = $1::uuid`, [findingId])
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    expect(await systemsFor(findingId)).toHaveLength(1)
    expect(
      await querySql(`select 1 from public.audit_log where finding_id = $1::uuid`, [findingId]),
    ).toHaveLength(1)
  })

  it('does not fire for a non-create_ai_system finding', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    expect(await systemsFor(findingId)).toHaveLength(0)
  })

  it('exposes the new system to its owner under RLS and hides it from others', async () => {
    const admin = createServiceRoleClient()
    const findingId = await makeFinding(profileId, `${PREFIX}ai`)
    await tagCreateAiSystem(findingId, { name: 'Owned model', risk_classification: 'limited' })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    const [sys] = await systemsFor(findingId)

    const other = await signUpTestUser(admin)
    try {
      const ownerClient = await createUserClient(user.email, user.password)
      const owned = await ownerClient.from('ai_systems').select('id').eq('id', sys.id)
      expect(owned.error).toBeNull()
      expect((owned.data ?? []).map((r) => r.id)).toEqual([sys.id])

      const otherClient = await createUserClient(other.email, other.password)
      const foreign = await otherClient.from('ai_systems').select('id').eq('id', sys.id)
      expect(foreign.error).toBeNull()
      expect(foreign.data ?? []).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
