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
 * ENT-70 — manual ROPA create/edit with audit + Free-tier cap.
 *
 * The register's two writes go through the SECURITY DEFINER RPCs
 * create_processing_activity / update_processing_activity. They are called here
 * through an authenticated (anon-key) client so auth.uid() — which the RPCs rely
 * on for the actor — resolves exactly as it does from the app.
 *
 * Acceptance criteria exercised here:
 *   * Manual add inserts a finding-less activity and records an audit entry.
 *   * Inline edit records an audit entry — but a no-op save does not.
 *   * The Free tier caps manual activities at 3.
 *   * A founder cannot edit another founder's activity.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('ROPA manual create/edit (ENT-70)', () => {
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
    await applyFixtureSql(
      `delete from public.processing_activities where user_id = '${user.id}'::uuid;`,
    )
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  const add = (name: string) =>
    client.rpc('create_processing_activity', {
      p_name: name,
      p_purpose: 'A purpose',
      p_legal_basis: 'consent',
      p_data_categories: ['email'],
      p_recipients: ['Stripe'],
      p_retention_period: '24 months',
    })

  it('manual add inserts a finding-less row and records an audit entry', async () => {
    const { data: id, error } = await add('First activity')
    expect(error).toBeNull()
    expect(id).toBeTruthy()

    const [row] = await querySql<{ finding_id: string | null; name: string }>(
      `select finding_id, name from public.processing_activities where id = $1::uuid`,
      [id],
    )
    expect(row.finding_id).toBeNull() // manual
    expect(row.name).toBe('First activity')

    const audits = await querySql(
      `select 1 from public.audit_log
       where target_id = $1::uuid and action_type = 'create_ropa_manual'`,
      [id],
    )
    expect(audits).toHaveLength(1)
  })

  it('records an audit entry on a real edit but not on a no-op save', async () => {
    const { data: id } = await add('Editable activity')

    const edit = (recipients: string[]) =>
      client.rpc('update_processing_activity', {
        p_id: id,
        p_name: 'Editable activity',
        p_purpose: 'A purpose',
        p_legal_basis: 'contract', // changed from consent
        p_data_categories: ['email'],
        p_recipients: recipients,
        p_retention_period: '24 months',
      })

    const first = await edit(['AWS'])
    expect(first.error).toBeNull()

    const auditCount = async () =>
      (
        await querySql(
          `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'update_ropa'`,
          [id],
        )
      ).length

    expect(await auditCount()).toBe(1)

    // Identical save — nothing changed, so no new audit entry.
    await edit(['AWS'])
    expect(await auditCount()).toBe(1)
  })

  it('caps manual activities at 3 on the Free tier', async () => {
    // One activity already exists from the first test; clear to a known baseline.
    await applyFixtureSql(
      `delete from public.processing_activities where user_id = '${user.id}'::uuid;`,
    )

    expect((await add('A')).error).toBeNull()
    expect((await add('B')).error).toBeNull()
    expect((await add('C')).error).toBeNull()

    const fourth = await add('D')
    expect(fourth.error).not.toBeNull()
    expect(fourth.error?.message ?? '').toMatch(/free tier limit/i)

    const rows = await querySql(
      `select 1 from public.processing_activities where user_id = $1::uuid`,
      [user.id],
    )
    expect(rows).toHaveLength(3)
  })

  it('does not cap a Pro user — the limit is enforced server-side per plan (ENT-84)', async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(
      `delete from public.processing_activities where user_id = '${user.id}'::uuid;`,
    )
    // Service role flips the plan (users can't write their own subscription).
    await admin.from('subscriptions').update({ plan: 'pro' }).eq('user_id', user.id)

    try {
      for (const name of ['A', 'B', 'C', 'D', 'E']) {
        expect((await add(name)).error).toBeNull()
      }
      const rows = await querySql(
        `select 1 from public.processing_activities where user_id = $1::uuid`,
        [user.id],
      )
      expect(rows).toHaveLength(5) // past the Free cap of 3
    } finally {
      // Restore the Free plan so later tests stay on a known baseline.
      await admin.from('subscriptions').update({ plan: 'free' }).eq('user_id', user.id)
      await applyFixtureSql(
        `delete from public.processing_activities where user_id = '${user.id}'::uuid;`,
      )
    }
  })

  it('does not let a founder edit another founder\'s activity', async () => {
    const admin = createServiceRoleClient()
    const { data: id } = await add('Mine')

    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const res = await otherClient.rpc('update_processing_activity', {
        p_id: id,
        p_name: 'Hacked',
        p_purpose: null,
        p_legal_basis: null,
        p_data_categories: null,
        p_recipients: null,
        p_retention_period: null,
      })
      expect(res.error).not.toBeNull()
      expect(res.error?.message ?? '').toMatch(/not found or not owned/i)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
