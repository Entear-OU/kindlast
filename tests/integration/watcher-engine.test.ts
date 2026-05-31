// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { querySql } from './helpers/db-fixture'
import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-53 — The Watcher's daily-run engine.
 *
 * This is the harness every detector (ENT-55/56/57) plugs into, so the
 * properties under test here are the *shared infrastructure*, not any one
 * detector's logic (detectors land in their own issues):
 *
 *   1. `watcher_findings` exists with RLS enabled — findings are user-owned.
 *   2. `emit_watcher_finding()` inserts an open finding and is idempotent:
 *      re-emitting the same (profile, dedup_key) while a finding is open
 *      refreshes it in place rather than duplicating (AC: "replays don't
 *      create duplicate findings").
 *   3. Once a finding is resolved, the same dedup_key may open a fresh one —
 *      the suppression is scoped to *open* findings only.
 *   4. `run_watcher_for_profile()` records the per-profile last-run timestamp
 *      (AC: "Last-run timestamp recorded per profile").
 *   5. `run_watcher()` processes exactly one profile per active client — the
 *      latest profile per user — so re-interviews (ENT-47) don't double-run.
 *   6. RLS: a user reads only their own findings; anon reads none.
 *   7. `pg_cron` schedules the Watcher daily (AC: "pg_cron fires daily").
 *
 * Service-role / direct SQL is used for fixture setup because findings are
 * written by the Watcher (a SECURITY DEFINER function invoked by cron), never
 * by an end user — there is no user-facing INSERT path to exercise.
 *
 * Skips when the local Supabase stack is unreachable — same pattern as
 * sibling integration suites.
 */

const supabaseRunning = await isLocalSupabaseReachable()

async function createProfileFor(
  admin: ReturnType<typeof createServiceRoleClient>,
  user: TestUser,
): Promise<string> {
  const { data: session, error: sErr } = await admin
    .from('onboarding_sessions')
    .insert({ user_id: user.id, status: 'completed' })
    .select('id')
    .single()
  expect(sErr).toBeNull()

  const { data: profile, error: pErr } = await admin
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
  expect(pErr).toBeNull()
  return profile!.id as string
}

describe.skipIf(!supabaseRunning)('watcher engine (ENT-53)', () => {
  let userA: TestUser
  let userB: TestUser
  let profileA: string
  let profileB: string

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)
    profileA = await createProfileFor(admin, userA)
    profileB = await createProfileFor(admin, userB)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (userA?.id) await deleteTestUser(admin, userA.id)
    if (userB?.id) await deleteTestUser(admin, userB.id)
  })

  it('exposes watcher_findings with RLS enabled', async () => {
    const cols = await querySql<{ column_name: string }>(
      `select column_name from information_schema.columns
       where table_schema = 'public' and table_name = 'watcher_findings'`,
    )
    const names = cols.map((c) => c.column_name)
    expect(names).toEqual(
      expect.arrayContaining([
        'id',
        'profile_id',
        'user_id',
        'kind',
        'obligation_slug',
        'severity',
        'title',
        'detail',
        'status',
        'dedup_key',
        'metadata',
        'created_at',
        'updated_at',
        'resolved_at',
      ]),
    )

    const [rls] = await querySql<{ relrowsecurity: boolean }>(
      `select relrowsecurity from pg_class
       where oid = 'public.watcher_findings'::regclass`,
    )
    expect(rls.relrowsecurity).toBe(true)
  })

  it('emit_watcher_finding inserts an open finding and is idempotent while open', async () => {
    const dedupKey = `_test_ent53_dedup_${profileA}`

    const [first] = await querySql<{ emit_watcher_finding: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'profile_gap', $2::text, 'No DPO appointed', 'detail v1', 'high', 'gdpr-art-37-dpo'
       )`,
      [profileA, dedupKey],
    )
    expect(first.emit_watcher_finding).toBeTruthy()

    // Re-emit the same key while still open → no duplicate, fields refreshed.
    const [second] = await querySql<{ emit_watcher_finding: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'profile_gap', $2::text, 'No DPO appointed', 'detail v2', 'high', 'gdpr-art-37-dpo'
       )`,
      [profileA, dedupKey],
    )
    expect(second.emit_watcher_finding).toBe(first.emit_watcher_finding)

    const rows = await querySql<{ id: string; detail: string; status: string }>(
      `select id, detail, status from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text`,
      [profileA, dedupKey],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].detail).toBe('detail v2')
    expect(rows[0].status).toBe('open')
  })

  it('allows a fresh open finding once the prior one is resolved', async () => {
    const dedupKey = `_test_ent53_resolve_${profileA}`

    const [first] = await querySql<{ emit_watcher_finding: string }>(
      `select public.emit_watcher_finding($1::uuid, 'deadline', $2::text, 'Deadline near')`,
      [profileA, dedupKey],
    )
    await querySql(
      `update public.watcher_findings set status = 'resolved', resolved_at = now() where id = $1::uuid`,
      [first.emit_watcher_finding],
    )

    const [second] = await querySql<{ emit_watcher_finding: string }>(
      `select public.emit_watcher_finding($1::uuid, 'deadline', $2::text, 'Deadline near again')`,
      [profileA, dedupKey],
    )
    expect(second.emit_watcher_finding).not.toBe(first.emit_watcher_finding)

    const rows = await querySql<{ status: string }>(
      `select status from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text order by created_at`,
      [profileA, dedupKey],
    )
    expect(rows.map((r) => r.status)).toEqual(['resolved', 'open'])
  })

  it('run_watcher_for_profile records the per-profile last-run timestamp', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileB])
    const [row] = await querySql<{ watcher_last_run_at: string | null }>(
      `select watcher_last_run_at from public.compliance_profiles where id = $1::uuid`,
      [profileB],
    )
    expect(row.watcher_last_run_at).not.toBeNull()
  })

  it('run_watcher runs exactly the latest profile per user', async () => {
    const admin = createServiceRoleClient()

    // A second, newer profile for user A simulates a re-interview (ENT-47).
    const newerProfileA = await createProfileFor(admin, userA)

    // Reset both A profiles' last-run so we can observe which one runs.
    await querySql(
      `update public.compliance_profiles set watcher_last_run_at = null where id = any($1::uuid[])`,
      [[profileA, newerProfileA]],
    )

    const [{ run_watcher: processed }] = await querySql<{ run_watcher: number }>(
      `select public.run_watcher()`,
    )
    expect(processed).toBeGreaterThanOrEqual(1)

    const rows = await querySql<{ id: string; watcher_last_run_at: string | null }>(
      `select id, watcher_last_run_at from public.compliance_profiles
       where id = any($1::uuid[])`,
      [[profileA, newerProfileA]],
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r.watcher_last_run_at]))
    expect(byId[newerProfileA]).not.toBeNull() // latest profile ran
    expect(byId[profileA]).toBeNull() // superseded profile did not
  })

  it('enforces per-user RLS on findings', async () => {
    const dedupKey = `_test_ent53_rls_${profileA}`
    await querySql(
      `select public.emit_watcher_finding($1::uuid, 'profile_gap', $2::text, 'RLS check')`,
      [profileA, dedupKey],
    )

    const clientA = await createUserClient(userA.email, userA.password)
    const { data: own } = await clientA
      .from('watcher_findings')
      .select('id')
      .eq('dedup_key', dedupKey)
    expect(own).toHaveLength(1)

    const clientB = await createUserClient(userB.email, userB.password)
    const { data: other } = await clientB
      .from('watcher_findings')
      .select('id')
      .eq('dedup_key', dedupKey)
    expect(other).toHaveLength(0)

    const anon = createAnonClient()
    const { data: anonRows } = await anon
      .from('watcher_findings')
      .select('id')
      .eq('dedup_key', dedupKey)
    expect(anonRows ?? []).toHaveLength(0)
  })

  it('schedules the Watcher daily via pg_cron', async () => {
    const rows = await querySql<{ schedule: string; command: string; active: boolean }>(
      `select schedule, command, active from cron.job where jobname = 'watcher-daily'`,
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].schedule).toBe('0 6 * * *')
    expect(rows[0].command).toMatch(/run_watcher\(\)/)
    expect(rows[0].active).toBe(true)
  })
})
