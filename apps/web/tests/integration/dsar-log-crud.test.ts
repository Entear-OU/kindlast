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
 * ENT-71 — manual DSAR logging + mark-responded with reviewed approval & audit.
 *
 * The log's two writes go through the SECURITY DEFINER RPCs log_dsar /
 * mark_dsar_responded, called here through an authenticated client so auth.uid()
 * resolves as it does from the app.
 *
 * Acceptance criteria exercised here:
 *   * Manual "Log a DSAR" opens a finding-less request with a 30-day deadline and
 *     records an audit entry.
 *   * "Mark as responded" requires a reviewed approval (rejected without it) and,
 *     once confirmed, sets responded + records an audit entry. Idempotent.
 *   * A founder cannot mark another founder's DSAR.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('DSAR Log manual log + mark responded (ENT-71)', () => {
  let user: TestUser
  let client: Awaited<ReturnType<typeof createUserClient>>

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)
    client = await createUserClient(user.email, user.password)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    await applyFixtureSql(`delete from public.dsars where user_id = '${user.id}'::uuid;`)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('logs a manual DSAR with a 30-day deadline and an audit entry', async () => {
    const { data: id, error } = await client.rpc('log_dsar', {
      p_subject_name: 'Jane Roe',
      p_request_type: 'access',
      p_handler: 'Privacy Team',
    })
    expect(error).toBeNull()
    expect(id).toBeTruthy()

    const [row] = await querySql<{
      finding_id: string | null
      status: string
      gap_days: number
    }>(
      `select finding_id, status, (response_due_at::date - received_at::date) as gap_days
       from public.dsars where id = $1::uuid`,
      [id],
    )
    expect(row.finding_id).toBeNull()
    expect(row.status).toBe('open')
    expect(Number(row.gap_days)).toBe(30)

    const audits = await querySql(
      `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'create_dsar_manual'`,
      [id],
    )
    expect(audits).toHaveLength(1)
  })

  it('requires a reviewed approval to mark responded, then records it once', async () => {
    const { data: id } = await client.rpc('log_dsar', {
      p_subject_name: 'John Doe',
      p_request_type: 'erasure',
      p_handler: null,
    })

    // Without review → rejected, nothing changes.
    const plain = await client.rpc('mark_dsar_responded', { p_id: id, p_reviewed: false })
    expect(plain.error).not.toBeNull()
    expect(plain.error?.message ?? '').toMatch(/reviewed approval/i)

    const [stillOpen] = await querySql<{ status: string }>(
      `select status from public.dsars where id = $1::uuid`,
      [id],
    )
    expect(stillOpen.status).toBe('open')

    // Reviewed → responded + audit.
    const reviewed = await client.rpc('mark_dsar_responded', { p_id: id, p_reviewed: true })
    expect(reviewed.error).toBeNull()

    const [done] = await querySql<{ status: string; responded_at: string | null }>(
      `select status, responded_at from public.dsars where id = $1::uuid`,
      [id],
    )
    expect(done.status).toBe('responded')
    expect(done.responded_at).not.toBeNull()

    const auditCount = async () =>
      (
        await querySql(
          `select 1 from public.audit_log where target_id = $1::uuid and action_type = 'mark_dsar_responded'`,
          [id],
        )
      ).length
    expect(await auditCount()).toBe(1)

    // Idempotent — marking an already-responded DSAR records nothing new.
    await client.rpc('mark_dsar_responded', { p_id: id, p_reviewed: true })
    expect(await auditCount()).toBe(1)
  })

  it('does not let a founder mark another founder\'s DSAR', async () => {
    const admin = createServiceRoleClient()
    const { data: id } = await client.rpc('log_dsar', {
      p_subject_name: 'Mine',
      p_request_type: 'access',
      p_handler: null,
    })

    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const res = await otherClient.rpc('mark_dsar_responded', { p_id: id, p_reviewed: true })
      expect(res.error).not.toBeNull()
      expect(res.error?.message ?? '').toMatch(/not found or not owned/i)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
