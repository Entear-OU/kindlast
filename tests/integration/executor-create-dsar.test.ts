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
 * ENT-67 — Executor creates a DSAR tracking task on approval.
 *
 * When a founder approves a finding tagged `action_type='create_dsar'`, the
 * Executor opens one `dsars` row with a 30-day Article 12(3) countdown, pre-filled
 * from the Analyst payload, and records the write in the immutable audit log.
 *
 * Acceptance criteria exercised here:
 *   * Inserts a dsars row: received_at = now(), response_due_at = received_at +
 *     30 days, status = 'open'.
 *   * Pre-fills request type, requester and handler from the Analyst payload.
 *   * An audit_log row is written.
 *   * The Watcher picks the row up on its next run — the DSAR-escalation detector
 *     emits a deadline signal for it once the deadline nears.
 *
 * The DSAR write reuses the ENT-66 machinery: the create_dsar action_type, the
 * approve_finding() entry point, and record_audit_log(). Findings are produced
 * through the real pipeline so every Analyst-era constraint is satisfied.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent67_'

const SUMMARY =
  'Fixture obligation for the ENT-67 executor DSAR test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

const DSAR_PAYLOAD = {
  request_type: 'access',
  requester: 'Jane Roe',
  handler: 'Privacy Team',
}

interface DsarRow {
  id: string
  user_id: string
  finding_id: string | null
  subject_name: string | null
  request_type: string | null
  handler: string | null
  status: string
  received_at: string
  response_due_at: string
  responded_at: string | null
  [key: string]: unknown
}

async function seedObligation(slug: string): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}',
       '32016R0679', 'article', 12, '{"role":"controller"}'::jsonb, 'high', null)
    on conflict (slug) do nothing;
  `)
}

/** Emit a signal and convert it to a finding, returning the finding id. */
async function makeFinding(profileId: string, slug: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
     ) as id`,
    [profileId, `gap:${slug}`, `Profile gap: ${slug}`, 'A DSAR needs logging.', slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return findingId
}

async function tagCreateDsar(findingId: string, payload: object): Promise<void> {
  await querySql(
    `update public.findings
       set action_type = 'create_dsar',
           metadata = metadata || jsonb_build_object('payload', $2::jsonb)
     where id = $1::uuid`,
    [findingId, JSON.stringify(payload)],
  )
}

const dsarsFor = (findingId: string) =>
  querySql<DsarRow>(`select * from public.dsars where finding_id = $1::uuid`, [findingId])

describe.skipIf(!supabaseRunning)('executor creates a DSAR tracking task on approval (ENT-67)', () => {
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

    await seedObligation(`${PREFIX}dsar`)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // DSARs cascade with the user; clear findings citing the fixture obligation
    // (delete-protected) before dropping it.
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('opens one DSAR with a 30-day countdown, pre-filled from the payload', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    await tagCreateDsar(findingId, DSAR_PAYLOAD)

    const target = await querySql<{ id: string | null }>(
      `select public.approve_finding($1::uuid, $2::uuid) as id`,
      [findingId, user.id],
    ).then((r) => r[0].id)

    const rows = await dsarsFor(findingId)
    expect(rows).toHaveLength(1)
    const dsar = rows[0]
    expect(target).toBe(dsar.id) // approve_finding returns the new row's id

    // Pre-filled from the Analyst payload.
    expect(dsar.request_type).toBe(DSAR_PAYLOAD.request_type)
    expect(dsar.subject_name).toBe(DSAR_PAYLOAD.requester)
    expect(dsar.handler).toBe(DSAR_PAYLOAD.handler)

    // received_at = now(), status open, response_due_at = received_at + 30 days.
    expect(dsar.status).toBe('open')
    expect(dsar.responded_at).toBeNull()
    expect(dsar.user_id).toBe(user.id)
    const [{ gap_days }] = await querySql<{ gap_days: number }>(
      `select (response_due_at::date - received_at::date) as gap_days
       from public.dsars where id = $1::uuid`,
      [dsar.id],
    )
    expect(Number(gap_days)).toBe(30)
  })

  it('writes a matching audit_log entry', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    await tagCreateDsar(findingId, { ...DSAR_PAYLOAD, requester: 'John Doe' })
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    const [dsar] = await dsarsFor(findingId)
    const [entry] = await querySql<{
      action_type: string
      target_table: string
      target_id: string
      approving_user_id: string
      before: unknown
      after: { subject_name: string; status: string } | null
    }>(
      `select action_type, target_table, target_id, approving_user_id, before, after
       from public.audit_log where finding_id = $1::uuid`,
      [findingId],
    )
    expect(entry.action_type).toBe('create_dsar')
    expect(entry.target_table).toBe('dsars')
    expect(entry.target_id).toBe(dsar.id)
    expect(entry.approving_user_id).toBe(user.id)
    expect(entry.before).toBeNull()
    expect(entry.after?.subject_name).toBe('John Doe')
    expect(entry.after?.status).toBe('open')
  })

  it('is idempotent — re-approving never opens a second DSAR', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    await tagCreateDsar(findingId, DSAR_PAYLOAD)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    await querySql(`update public.findings set status = 'pending' where id = $1::uuid`, [findingId])
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])

    expect(await dsarsFor(findingId)).toHaveLength(1)
    expect(
      await querySql(`select 1 from public.audit_log where finding_id = $1::uuid`, [findingId]),
    ).toHaveLength(1)
  })

  it('does not fire for a non-create_dsar finding', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    // Left as the default action_type 'review'.
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    expect(await dsarsFor(findingId)).toHaveLength(0)
  })

  it('is picked up by the Watcher — the escalation detector signals it near deadline', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    await tagCreateDsar(findingId, DSAR_PAYLOAD)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    const [dsar] = await dsarsFor(findingId)

    // The fresh DSAR sits in the detector's scan set (open, unanswered) but 30
    // days out, so it is tracked, not yet escalated.
    const beforeAging = await querySql(
      `select 1 from public.watcher_findings where dedup_key = $1`,
      [`dsar:${dsar.id}`],
    )
    expect(beforeAging).toHaveLength(0)

    // Simulate time passing toward the deadline, then run the Watcher's DSAR
    // detector: it must surface this Executor-created row.
    await querySql(
      `update public.dsars set response_due_at = now() + interval '5 days' where id = $1::uuid`,
      [dsar.id],
    )
    await querySql(`select public.watcher_detect_dsar_escalation($1::uuid)`, [profileId])

    const signal = await querySql<{ severity: string; metadata: { dsar_id: string } }>(
      `select severity, metadata from public.watcher_findings where dedup_key = $1`,
      [`dsar:${dsar.id}`],
    )
    expect(signal).toHaveLength(1)
    expect(signal[0].severity).toBe('critical')
    expect(signal[0].metadata.dsar_id).toBe(dsar.id)
  })

  it('exposes the new DSAR to its owner under RLS and hides it from others', async () => {
    const admin = createServiceRoleClient()
    const findingId = await makeFinding(profileId, `${PREFIX}dsar`)
    await tagCreateDsar(findingId, DSAR_PAYLOAD)
    await querySql(`select public.approve_finding($1::uuid, $2::uuid)`, [findingId, user.id])
    const [dsar] = await dsarsFor(findingId)

    const other = await signUpTestUser(admin)
    try {
      const ownerClient = await createUserClient(user.email, user.password)
      const owned = await ownerClient.from('dsars').select('id').eq('id', dsar.id)
      expect(owned.error).toBeNull()
      expect((owned.data ?? []).map((r) => r.id)).toEqual([dsar.id])

      const otherClient = await createUserClient(other.email, other.password)
      const foreign = await otherClient.from('dsars').select('id').eq('id', dsar.id)
      expect(foreign.error).toBeNull()
      expect(foreign.data ?? []).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
