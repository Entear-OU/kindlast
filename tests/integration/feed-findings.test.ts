// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { loadFindings } from '@/lib/feed/findings'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-62 — loadFindings against the live stack.
 *
 * Asserts the feed loader returns the user's findings newest-first and is
 * RLS-scoped (owner sees own; another user sees none). Findings are produced
 * through the real pipeline (emit a Watcher signal → analyst_convert_signal)
 * with unique dedup keys, per the hermetic-tests convention. Swept by obligation
 * prefix in afterAll (findings are delete-protected by their obligation FK).
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent62_'
const SUMMARY =
  'Fixture obligation for the ENT-62 feed loader test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

let seq = 0

async function makeFinding(profileId: string, slug: string): Promise<string> {
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, 'A control is missing.', 'high', $4::text, '{}'::jsonb
     ) as id`,
    [profileId, `gap:${slug}:${(seq += 1)}`, `Profile gap: ${slug}`, slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return findingId
}

describe.skipIf(!supabaseRunning)('feed loadFindings (ENT-62)', () => {
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
        ('${PREFIX}ropa', 'Fixture ${PREFIX}ropa', '${SUMMARY}',
         '32016R0679', 'article', 30, '{"role":"controller"}'::jsonb, 'high', null)
      on conflict (slug) do nothing;
    `)

    // Three findings, created oldest→newest so reverse-chron order is testable.
    await makeFinding(profileId, `${PREFIX}ropa`)
    await new Promise((r) => setTimeout(r, 10))
    await makeFinding(profileId, `${PREFIX}ropa`)
    await new Promise((r) => setTimeout(r, 10))
    await makeFinding(profileId, `${PREFIX}ropa`)
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

  it('returns the owner findings newest-first under RLS', async () => {
    const client = await createUserClient(user.email, user.password)
    const findings = await loadFindings(client, user.id)

    expect(findings).toHaveLength(3)
    // Reverse-chronological by created_at.
    const times = findings.map((f) => new Date(f.created_at).getTime())
    expect(times).toEqual([...times].sort((a, b) => b - a))
    // Carries the fields the feed renders.
    expect(findings[0].regulatory_obligation).toBe('GDPR Art. 30')
    expect(findings[0].status).toBe('pending')
    expect(findings[0].severity).toBe('high')
  })

  it('does not leak findings to another user (RLS)', async () => {
    const admin = createServiceRoleClient()
    const other = await signUpTestUser(admin)
    try {
      const otherClient = await createUserClient(other.email, other.password)
      const findings = await loadFindings(otherClient, user.id)
      expect(findings).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
