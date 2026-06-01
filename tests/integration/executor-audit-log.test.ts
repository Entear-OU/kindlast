// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-69 — Every Executor action writes an immutable audit log entry.
 *
 * The audit log is the product's compliance evidence: it must be *append-only*.
 * The Executor (the only agent that writes compliance records, ENT-66/67/68)
 * records one entry per approved action through the canonical
 * `record_audit_log()` writer. This suite pins the immutability guarantees the
 * rest of the epic leans on.
 *
 * Acceptance criteria exercised here:
 *   * `audit_log` is INSERT-only by RLS — the owner role can SELECT and INSERT
 *     its own rows but has no UPDATE / DELETE path.
 *   * Schema carries id, user_id, finding_id, action_type, target_table,
 *     target_id, before, after, approving_user_id, occurred_at.
 *   * Existing rows are immutable even to the service role / SECURITY DEFINER —
 *     a BEFORE UPDATE guard rejects every update, so retention/cleanup can only
 *     delete whole rows, never silently mutate one.
 *   * A `(user_id, occurred_at desc)` index backs the dashboard "recent actions"
 *     query so it never falls back to a sequential scan.
 */

const supabaseRunning = await isLocalSupabaseReachable()

interface AuditRow {
  id: string
  user_id: string
  finding_id: string | null
  action_type: string
  target_table: string
  target_id: string | null
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  approving_user_id: string
  occurred_at: string
  [key: string]: unknown
}

const record = (args: {
  userId: string
  findingId: string | null
  actionType: string
  targetTable: string
  targetId: string | null
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  approvingUserId: string
}) =>
  querySql<{ id: string }>(
    `select public.record_audit_log(
       $1::uuid, $2::uuid, $3::text, $4::text, $5::uuid, $6::jsonb, $7::jsonb, $8::uuid
     ) as id`,
    [
      args.userId,
      args.findingId,
      args.actionType,
      args.targetTable,
      args.targetId,
      args.before === null ? null : JSON.stringify(args.before),
      args.after === null ? null : JSON.stringify(args.after),
      args.approvingUserId,
    ],
  ).then((rows) => rows[0].id)

describe.skipIf(!supabaseRunning)('executor audit log immutability (ENT-69)', () => {
  let user: TestUser
  let seededId: string

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)

    // A representative create-action entry written through the canonical writer.
    seededId = await record({
      userId: user.id,
      findingId: null,
      actionType: 'create_ropa',
      targetTable: 'processing_activities',
      targetId: '00000000-0000-0000-0000-0000000000aa',
      before: null,
      after: { name: 'Customer onboarding', legal_basis: 'contract' },
      approvingUserId: user.id,
    })
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // Retention/cleanup path: the service role deletes rows directly.
    await admin.from('audit_log').delete().eq('user_id', user.id)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('persists the full evidence schema for an Executor action', async () => {
    const [row] = await querySql<AuditRow>(
      `select * from public.audit_log where id = $1::uuid`,
      [seededId],
    )
    expect(row.user_id).toBe(user.id)
    expect(row.action_type).toBe('create_ropa')
    expect(row.target_table).toBe('processing_activities')
    expect(row.target_id).toBe('00000000-0000-0000-0000-0000000000aa')
    expect(row.before).toBeNull()
    expect(row.after).toMatchObject({ name: 'Customer onboarding', legal_basis: 'contract' })
    expect(row.approving_user_id).toBe(user.id)
    expect(row.finding_id).toBeNull()
    expect(typeof row.occurred_at).toBe('string')
  })

  it('exposes a row only to its owner under RLS', async () => {
    const admin = createServiceRoleClient()
    const other = await signUpTestUser(admin)
    try {
      const ownerClient = await createUserClient(user.email, user.password)
      const owned = await ownerClient.from('audit_log').select('id').eq('id', seededId)
      expect(owned.error).toBeNull()
      expect((owned.data ?? []).map((r) => r.id)).toEqual([seededId])

      const otherClient = await createUserClient(other.email, other.password)
      const foreign = await otherClient.from('audit_log').select('id').eq('id', seededId)
      expect(foreign.error).toBeNull()
      expect(foreign.data ?? []).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })

  it('lets the owner INSERT its own entry but rejects forging another user', async () => {
    const admin = createServiceRoleClient()
    const other = await signUpTestUser(admin)
    try {
      const ownerClient = await createUserClient(user.email, user.password)

      const ok = await ownerClient
        .from('audit_log')
        .insert({
          user_id: user.id,
          action_type: 'mark_dsar_responded',
          target_table: 'dsars',
          approving_user_id: user.id,
        })
        .select('id')
        .single()
      expect(ok.error).toBeNull()
      expect(ok.data?.id).toBeTruthy()

      // Owner cannot write a row attributed to a different user.
      const forged = await ownerClient.from('audit_log').insert({
        user_id: other.id,
        action_type: 'create_ropa',
        target_table: 'processing_activities',
        approving_user_id: other.id,
      })
      expect(forged.error).not.toBeNull()
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })

  it('is INSERT-only by RLS — the owner has no UPDATE or DELETE path', async () => {
    const ownerClient = await createUserClient(user.email, user.password)

    // No UPDATE policy → the row is invisible to update; nothing changes.
    const upd = await ownerClient
      .from('audit_log')
      .update({ action_type: 'tampered' })
      .eq('id', seededId)
      .select('id')
    expect(upd.error).toBeNull()
    expect(upd.data ?? []).toHaveLength(0)

    // No DELETE policy → the row is invisible to delete; nothing is removed.
    const del = await ownerClient.from('audit_log').delete().eq('id', seededId).select('id')
    expect(del.error).toBeNull()
    expect(del.data ?? []).toHaveLength(0)

    const [still] = await querySql<{ action_type: string }>(
      `select action_type from public.audit_log where id = $1::uuid`,
      [seededId],
    )
    expect(still.action_type).toBe('create_ropa') // untouched
  })

  it('rejects every UPDATE — even from the service role / SECURITY DEFINER', async () => {
    // The BEFORE UPDATE guard fires regardless of role, so an existing entry can
    // never be silently mutated. A direct (superuser) update must raise.
    await expect(
      querySql(`update public.audit_log set action_type = 'tampered' where id = $1::uuid`, [
        seededId,
      ]),
    ).rejects.toThrow()

    const [row] = await querySql<{ action_type: string }>(
      `select action_type from public.audit_log where id = $1::uuid`,
      [seededId],
    )
    expect(row.action_type).toBe('create_ropa')
  })

  it('allows the service role to delete rows for retention/cleanup', async () => {
    const admin = createServiceRoleClient()
    const disposableId = await record({
      userId: user.id,
      findingId: null,
      actionType: 'create_ai_system',
      targetTable: 'ai_systems',
      targetId: null,
      before: null,
      after: { name: 'Vendor model' },
      approvingUserId: user.id,
    })

    const del = await admin.from('audit_log').delete().eq('id', disposableId).select('id')
    expect(del.error).toBeNull()
    expect((del.data ?? []).map((r) => r.id)).toEqual([disposableId])

    const remaining = await querySql(
      `select 1 from public.audit_log where id = $1::uuid`,
      [disposableId],
    )
    expect(remaining).toHaveLength(0)
  })

  it('backs the recent-actions query with a (user_id, occurred_at desc) index', async () => {
    // The dashboard reads "most recent actions for this user": equality on
    // user_id, ordered by occurred_at desc. A composite index leading with
    // user_id and descending on occurred_at lets that query scale without a
    // table scan. We assert the index *definition* rather than an EXPLAIN plan:
    // on a near-empty table the planner rightly prefers a seq scan, so a plan
    // assertion would be flaky — the index's presence is what the AC requires.
    const indexes = await querySql<{ indexdef: string }>(
      `select indexdef from pg_indexes
       where schemaname = 'public' and tablename = 'audit_log'`,
    )
    const defs = indexes.map((i) => i.indexdef.toLowerCase())
    expect(defs.some((d) => d.includes('(user_id, occurred_at desc)'))).toBe(true)
  })
})
