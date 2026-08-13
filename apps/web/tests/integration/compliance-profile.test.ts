// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { type ComplianceProfile } from '@/lib/onboarding/extraction'
import {
  markSessionCompleted,
  persistComplianceProfile,
} from '@/lib/onboarding/persistence'

import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-45 — Compliance profile persistence.
 *
 * Exercises the table introduced in `<ts>_compliance_profiles.sql` and the
 * `persistComplianceProfile` / `markSessionCompleted` helpers end-to-end
 * against the local Supabase stack — all reads/writes flow through an
 * authenticated user client so RLS stays in the loop.
 *
 * Coverage:
 *   1. Maps `ComplianceProfile` (camelCase) onto the snake_case row shape.
 *   2. `(session_id)` unique constraint rejects a second profile per session.
 *   3. `markSessionCompleted` flips status to 'completed'.
 *   4. RLS denies anonymous reads.
 *   5. RLS scopes each user to their own rows.
 *   6. Yes/no/unsure check constraints reject invalid values.
 *
 * Skips when the local Supabase stack is unreachable — same pattern as
 * sibling integration suites.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const SAMPLE_PROFILE: ComplianceProfile = {
  industry: 'SaaS payroll for small businesses',
  euJurisdictions: ['Germany', 'France', 'Estonia'],
  dataCategories: ['customer emails', 'bank details', 'staff records'],
  dataSubjects: ['customers', 'staff', 'prospects'],
  aiSystems: ['ChatGPT (internal)', 'in-house anomaly detection (product)'],
  hasDpo: 'no',
  hasRopa: 'unsure',
  transfersOutsideEu: 'yes',
  transferDestinations: ['United States (Stripe)', 'United States (Amplitude)'],
  vendorList: 'Stripe, Amplitude, AWS (Frankfurt)',
  staffCount: 18,
}

describe.skipIf(!supabaseRunning)('compliance profile persistence (ENT-45)', () => {
  let userA: TestUser
  let userB: TestUser

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (userA?.id) await deleteTestUser(admin, userA.id)
    if (userB?.id) await deleteTestUser(admin, userB.id)
  })

  async function createSessionFor(user: TestUser): Promise<string> {
    const client = await createUserClient(user.email, user.password)
    const { data, error } = await client
      .from('onboarding_sessions')
      .insert({ user_id: user.id })
      .select('id')
      .single()
    expect(error).toBeNull()
    return data!.id as string
  }

  it('persists a profile mapped from camelCase to snake_case columns', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const sessionId = await createSessionFor(userA)

    const row = await persistComplianceProfile(client, {
      sessionId,
      userId: userA.id,
      profile: SAMPLE_PROFILE,
    })

    expect(row.session_id).toBe(sessionId)
    expect(row.user_id).toBe(userA.id)
    expect(row.industry).toBe(SAMPLE_PROFILE.industry)
    expect(row.eu_jurisdictions).toEqual(SAMPLE_PROFILE.euJurisdictions)
    expect(row.data_categories).toEqual(SAMPLE_PROFILE.dataCategories)
    expect(row.data_subjects).toEqual(SAMPLE_PROFILE.dataSubjects)
    expect(row.ai_systems).toEqual(SAMPLE_PROFILE.aiSystems)
    expect(row.has_dpo).toBe(SAMPLE_PROFILE.hasDpo)
    expect(row.has_ropa).toBe(SAMPLE_PROFILE.hasRopa)
    expect(row.transfers_outside_eu).toBe(SAMPLE_PROFILE.transfersOutsideEu)
    expect(row.transfer_destinations).toEqual(SAMPLE_PROFILE.transferDestinations)
    expect(row.vendor_list).toBe(SAMPLE_PROFILE.vendorList)
    expect(row.staff_count).toBe(SAMPLE_PROFILE.staffCount)
  })

  it('rejects a second profile for the same session (unique session_id)', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const sessionId = await createSessionFor(userA)

    await persistComplianceProfile(client, {
      sessionId,
      userId: userA.id,
      profile: SAMPLE_PROFILE,
    })

    await expect(
      persistComplianceProfile(client, {
        sessionId,
        userId: userA.id,
        profile: SAMPLE_PROFILE,
      }),
    ).rejects.toThrow(/duplicate|unique|session_id/i)
  })

  it('persists a profile with null staff_count', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const sessionId = await createSessionFor(userA)

    const row = await persistComplianceProfile(client, {
      sessionId,
      userId: userA.id,
      profile: { ...SAMPLE_PROFILE, staffCount: null },
    })
    expect(row.staff_count).toBeNull()
  })

  it('markSessionCompleted flips the session status to completed', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const sessionId = await createSessionFor(userA)

    await markSessionCompleted(client, sessionId)

    const { data } = await client
      .from('onboarding_sessions')
      .select('status, completed_at')
      .eq('id', sessionId)
      .single()
    expect(data?.status).toBe('completed')
    expect(data?.completed_at).not.toBeNull()
  })

  it('rejects an invalid yes/no/unsure value at the DB layer', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const sessionId = await createSessionFor(userA)

    const { error } = await client.from('compliance_profiles').insert({
      session_id: sessionId,
      user_id: userA.id,
      industry: 'X',
      has_dpo: 'maybe',
      has_ropa: 'no',
      transfers_outside_eu: 'no',
    })
    expect(error).not.toBeNull()
    expect(error?.message.toLowerCase()).toMatch(/has_dpo|check/)
  })

  it('denies anonymous reads of compliance_profiles', async () => {
    const anon = createAnonClient()
    const { data, error } = await anon.from('compliance_profiles').select('*')
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it("denies a user reading another user's profile", async () => {
    const sessionA = await createSessionFor(userA)
    const clientA = await createUserClient(userA.email, userA.password)
    await persistComplianceProfile(clientA, {
      sessionId: sessionA,
      userId: userA.id,
      profile: SAMPLE_PROFILE,
    })

    const clientB = await createUserClient(userB.email, userB.password)
    const { data, error } = await clientB
      .from('compliance_profiles')
      .select('id')
      .eq('session_id', sessionA)
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it("denies a user inserting a profile with another user's user_id", async () => {
    const sessionA = await createSessionFor(userA)
    const clientB = await createUserClient(userB.email, userB.password)

    const { error } = await clientB.from('compliance_profiles').insert({
      session_id: sessionA,
      user_id: userA.id,
      industry: 'X',
      has_dpo: 'no',
      has_ropa: 'no',
      transfers_outside_eu: 'no',
    })
    expect(error).not.toBeNull()
  })
})
