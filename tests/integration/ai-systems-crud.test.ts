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
 * ENT-72 — manual AI-system add + edit with reviewed reclassification & audit.
 *
 * The register's two writes go through the SECURITY DEFINER RPCs
 * create_ai_system_manual / update_ai_system, called through an authenticated
 * client so auth.uid() resolves as from the app.
 *
 * Acceptance criteria exercised here:
 *   * Manual "Add system" inserts a finding-less row + audit; a High-Risk class
 *     needs a reviewed approval.
 *   * Inline edit: a classification change needs a reviewed approval and records
 *     a 'reclassify_ai_system' audit entry; a plain edit records 'update_ai_system'.
 *   * A founder cannot edit another founder's system.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('AI Systems Register manual add + edit (ENT-72)', () => {
  let user: TestUser
  let client: Awaited<ReturnType<typeof createUserClient>>

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)

    const { data: session } = await admin
      .from('onboarding_sessions')
      .insert({ user_id: user.id, status: 'completed' })
      .select('id')
      .single()
    await admin.from('compliance_profiles').insert({
      session_id: session!.id,
      user_id: user.id,
      industry: 'SaaS',
      has_dpo: 'no',
      has_ropa: 'no',
      transfers_outside_eu: 'no',
    })

    client = await createUserClient(user.email, user.password)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`delete from public.ai_systems where user_id = '${user.id}'::uuid;`)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  const add = (name: string, cls: string, reviewed: boolean) =>
    client.rpc('create_ai_system_manual', {
      p_name: name,
      p_vendor: 'Acme',
      p_purpose: 'p',
      p_risk_classification: cls,
      p_documentation_status: 'missing',
      p_reviewed: reviewed,
    })

  it('adds a manual system and records an audit entry', async () => {
    const { data: id, error } = await add('Resume Screener', 'limited', false)
    expect(error).toBeNull()

    const [row] = await querySql<{ finding_id: string | null; last_reviewed_at: string | null }>(
      `select finding_id, last_reviewed_at from public.ai_systems where id = $1::uuid`,
      [id],
    )
    expect(row.finding_id).toBeNull()
    expect(row.last_reviewed_at).not.toBeNull()

    const audits = await querySql(
      `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'create_ai_system_manual'`,
      [id],
    )
    expect(audits).toHaveLength(1)
  })

  it('requires a reviewed approval to add a High-Risk system', async () => {
    const denied = await add('Biometric', 'high', false)
    expect(denied.error).not.toBeNull()
    expect(denied.error?.message ?? '').toMatch(/reviewed approval/i)

    const ok = await add('Biometric', 'high', true)
    expect(ok.error).toBeNull()
  })

  it('records update vs reclassify audits and gates a classification change', async () => {
    const { data: id } = await add('Editable', 'limited', false)

    const edit = (cls: string, reviewed: boolean, vendor = 'Acme') =>
      client.rpc('update_ai_system', {
        p_id: id,
        p_name: 'Editable',
        p_vendor: vendor,
        p_purpose: 'p',
        p_risk_classification: cls,
        p_documentation_status: 'complete',
        p_reviewed: reviewed,
      })

    // Plain field edit → update_ai_system audit.
    expect((await edit('limited', false, 'Acme Corp')).error).toBeNull()
    expect(
      await querySql(
        `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'update_ai_system'`,
        [id],
      ),
    ).toHaveLength(1)

    // Reclassify without review → rejected, class unchanged.
    const denied = await edit('high', false, 'Acme Corp')
    expect(denied.error).not.toBeNull()
    const [stillLimited] = await querySql<{ risk_classification: string }>(
      `select risk_classification from public.ai_systems where id = $1::uuid`,
      [id],
    )
    expect(stillLimited.risk_classification).toBe('limited')

    // Reclassify with review → reclassify_ai_system audit.
    expect((await edit('high', true, 'Acme Corp')).error).toBeNull()
    const [now] = await querySql<{ risk_classification: string }>(
      `select risk_classification from public.ai_systems where id = $1::uuid`,
      [id],
    )
    expect(now.risk_classification).toBe('high')
    expect(
      await querySql(
        `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'reclassify_ai_system'`,
        [id],
      ),
    ).toHaveLength(1)
  })

  it('does not let a founder edit another founder\'s system', async () => {
    const admin = createServiceRoleClient()
    const { data: id } = await add('Mine', 'minimal', false)

    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const res = await otherClient.rpc('update_ai_system', {
        p_id: id,
        p_name: 'Hacked',
        p_vendor: null,
        p_purpose: null,
        p_risk_classification: null,
        p_documentation_status: null,
        p_reviewed: false,
      })
      expect(res.error).not.toBeNull()
      expect(res.error?.message ?? '').toMatch(/not found or not owned/i)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
