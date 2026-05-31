// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-57 — Detect approaching DSAR deadlines with no logged response.
 *
 * The third detector in the ENT-53 Watcher loop. ENT-55 already opens a
 * `medium` DSAR finding for every unanswered request whose response_due_at is
 * within 30 days (dedup_key 'dsar:<id>'). ENT-57 layers the GDPR Article 12(3)
 * escalation on top: once fewer than 10 days remain (including overdue), the
 * same finding is bumped to `critical` so clients never miss the one-month
 * deadline.
 *
 * Escalation re-emits the *same* dedup key, so emit_watcher_finding() refreshes
 * the existing open finding in place rather than opening a competing one — one
 * finding per DSAR, severity tracking the clock. Re-emission suppression is
 * inherited from ENT-53's open-finding partial unique index.
 *
 * Day arithmetic matches ENT-55: response_due_at::date − current_date, so a
 * deadline N days out (same time of day) yields exactly N regardless of when
 * the run fires. Threshold is strict: <10 days remaining ⇒ critical; exactly
 * 10 days stays medium.
 *
 * DSARs are user-owned operational rows; the suite seeds them with the
 * service-role client and deletes the test user (cascade) in afterAll.
 */

const supabaseRunning = await isLocalSupabaseReachable()

/** Seed a DSAR due `dueInDays` from now (negative = overdue). Returns its id. */
async function seedDsar(opts: {
  userId: string
  subjectName: string
  dueInDays: number
  responded?: boolean
}): Promise<string> {
  const admin = createServiceRoleClient()
  const dueAt = new Date(Date.now() + opts.dueInDays * 86400_000).toISOString()
  const { data, error } = await admin
    .from('dsars')
    .insert({
      user_id: opts.userId,
      subject_name: opts.subjectName,
      request_type: 'access',
      status: opts.responded ? 'in_progress' : 'open',
      response_due_at: dueAt,
      responded_at: opts.responded ? new Date().toISOString() : null,
    })
    .select('id')
    .single()
  if (error || !data) throw new Error(`seedDsar failed: ${error?.message ?? 'no row'}`)
  return data.id as string
}

describe.skipIf(!supabaseRunning)('watcher DSAR escalation (ENT-57)', () => {
  let user: TestUser
  let profileId: string
  let critId: string // due in 5 days  → critical
  let boundaryId: string // due in 10 days → stays medium (strict <10)
  let midId: string // due in 20 days → medium
  let overdueId: string // 2 days overdue → critical
  let respondedId: string // due in 3 days but answered → no finding

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
        has_dpo: 'yes',
        has_ropa: 'yes',
        transfers_outside_eu: 'no',
        staff_count: 40,
      })
      .select('id')
      .single()
    expect(error).toBeNull()
    profileId = profile!.id as string

    critId = await seedDsar({ userId: user.id, subjectName: 'Critical Carol', dueInDays: 5 })
    boundaryId = await seedDsar({ userId: user.id, subjectName: 'Boundary Bob', dueInDays: 10 })
    midId = await seedDsar({ userId: user.id, subjectName: 'Medium Mary', dueInDays: 20 })
    overdueId = await seedDsar({ userId: user.id, subjectName: 'Overdue Otto', dueInDays: -2 })
    respondedId = await seedDsar({
      userId: user.id,
      subjectName: 'Responded Rita',
      dueInDays: 3,
      responded: true,
    })
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  /** Map of dsar_id → finding row, for the user's open DSAR findings. */
  async function openDsarFindings(): Promise<
    Map<string, { severity: string; metadata: { days_remaining: number; escalated?: boolean } }>
  > {
    const rows = await querySql<{
      severity: string
      metadata: { dsar_id: string; days_remaining: number; escalated?: boolean }
    }>(
      `select severity, metadata from public.watcher_findings
       where profile_id = $1::uuid and kind = 'dsar' and status = 'open'`,
      [profileId],
    )
    return new Map(rows.map((r) => [r.metadata.dsar_id, { severity: r.severity, metadata: r.metadata }]))
  }

  it('escalates DSARs with <10 days remaining (incl. overdue) to critical', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    const findings = await openDsarFindings()

    expect(findings.get(critId)?.severity).toBe('critical')
    expect(findings.get(critId)?.metadata.escalated).toBe(true)

    expect(findings.get(overdueId)?.severity).toBe('critical')
    expect(findings.get(overdueId)?.metadata.escalated).toBe(true)
    expect(findings.get(overdueId)?.metadata.days_remaining).toBeLessThan(0)
  })

  it('leaves DSARs with 10+ days remaining at medium (strict <10 threshold)', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    const findings = await openDsarFindings()

    expect(findings.get(boundaryId)?.severity).toBe('medium') // exactly 10 days
    expect(findings.get(boundaryId)?.metadata.escalated).toBeFalsy()
    expect(findings.get(midId)?.severity).toBe('medium') // 20 days
  })

  it('does not flag a DSAR that already has a logged response', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    const findings = await openDsarFindings()
    expect(findings.has(respondedId)).toBe(false)
  })

  it('keeps a single open finding per DSAR across runs (escalation is in place)', async () => {
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])
    await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

    const [{ count }] = await querySql<{ count: string }>(
      `select count(*)::text as count from public.watcher_findings
       where profile_id = $1::uuid and dedup_key = $2::text and status = 'open'`,
      [profileId, `dsar:${critId}`],
    )
    expect(count).toBe('1')
    const findings = await openDsarFindings()
    expect(findings.get(critId)?.severity).toBe('critical') // still critical, not duplicated
  })
})
