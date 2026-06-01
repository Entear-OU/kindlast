// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { signActionToken } from '@/lib/notifications/action-token'
import { performFindingAction } from '@/lib/notifications/act'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-73 — one-tap finding action handler against the live stack.
 *
 * The email's signed links act on a finding without a session (service role,
 * acting as the owner): reject and snooze flip the status for any tier; approve
 * is Pro-gated (Free → upgrade, Pro → approved); tampered / expired tokens are
 * refused. Mirrors the in-app feed actions, which is why the migration
 * parameterized reject/snooze with an explicit acting user.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent73_act_'
const SECRET = 'act-route-secret'
const NOW = 1_700_000_000
const SUMMARY =
  'Fixture obligation for the ENT-73 act-route test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeFinding(profileId: string, slug: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, 'A control is missing.', 'high', $4::text, '{}'::jsonb
     ) as id`,
    [profileId, `gap:${slug}:${(seq += 1)}`, `Profile gap: ${slug}`, slug],
  )
  const [{ id }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return id
}

async function statusOf(findingId: string): Promise<string> {
  const [{ status }] = await querySql<{ status: string }>(
    `select status from public.findings where id = $1`,
    [findingId],
  )
  return status
}

function token(findingId: string, action: 'approve' | 'reject' | 'snooze', opts?: { ttlSeconds?: number }) {
  return signActionToken({ findingId, action, nowSeconds: NOW, ...opts }, SECRET)
}

describe.skipIf(!supabaseRunning)('one-tap finding action (ENT-73)', () => {
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
    const { data: profile } = await admin
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
    profileId = profile!.id as string

    await applyFixtureSql(`
      insert into public.obligations
        (slug, title, summary, citation_celex, citation_kind, citation_article,
         applies_when, severity, effective_date)
      values
        ('${PREFIX}ob', 'Fixture ${PREFIX}ob', '${SUMMARY}',
         '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
      on conflict (slug) do nothing;
    `)
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

  async function setPlan(plan: 'free' | 'pro') {
    await querySql(`update public.subscriptions set plan = $2 where user_id = $1`, [user.id, plan])
  }

  it('rejects a finding from a one-tap link (any tier)', async () => {
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token(id, 'reject'),
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('ok')
    expect(await statusOf(id)).toBe('rejected')
  })

  it('snoozes a finding and sets snoozed_until', async () => {
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token(id, 'snooze'),
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('ok')
    expect(await statusOf(id)).toBe('snoozed')
  })

  it('blocks one-tap approve for a Free owner (upgrade), leaving the finding pending', async () => {
    await setPlan('free')
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token(id, 'approve'),
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('upgrade')
    expect(await statusOf(id)).toBe('pending')
  })

  it('approves a finding for a Pro owner', async () => {
    await setPlan('pro')
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token(id, 'approve'),
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('ok')
    expect(await statusOf(id)).toBe('approved')
    await setPlan('free')
  })

  it('refuses an expired token', async () => {
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token(id, 'reject', { ttlSeconds: 60 }),
      secret: SECRET,
      nowSeconds: NOW + 120,
    })
    expect(result.kind).toBe('expired')
    expect(await statusOf(id)).toBe('pending')
  })

  it('refuses a tampered token', async () => {
    const id = await makeFinding(profileId, `${PREFIX}ob`)
    const good = token(id, 'reject')
    const [payload] = good.split('.')
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: `${payload}.deadbeef`,
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('invalid')
    expect(await statusOf(id)).toBe('pending')
  })

  it('returns not_found for an unknown finding', async () => {
    const result = await performFindingAction({
      supabase: createServiceRoleClient(),
      token: token('00000000-0000-0000-0000-000000000000', 'reject'),
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(result.kind).toBe('not_found')
  })
})
