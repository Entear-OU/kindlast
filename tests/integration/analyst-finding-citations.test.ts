// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-59 — Map every finding to a specific obligation with article citation.
 *
 * ENT-58 produced findings with a baseline `regulatory_obligation` (the
 * obligation title) and a nullable `obligation_id`. ENT-59 makes the citation
 * trustworthy and auditable:
 *
 *   * `obligation_id` is NOT NULL and delete-protected (ON DELETE RESTRICT), so
 *     every finding has a verifiable obligation anchor and the catalogue can't
 *     be deleted out from under a live finding.
 *   * `regulatory_obligation` carries the precise article reference, e.g.
 *     "GDPR Art. 30" / "GDPR Art. 30(1)(b)" / "EU AI Act Annex III".
 *   * `citation_url` carries the resolvable EUR-Lex ELI anchor for the cited
 *     element — the same scheme `lib/corpus/resolve.ts` fetches at runtime.
 *
 * The citation is derived purely from the obligation's own citation fields (no
 * corpus join): the local/test corpus is empty and the catalogue's CELEX +
 * article/recital/annex is the stable natural key. Signals are emitted directly
 * via `emit_watcher_finding` so this profile's signal set stays controlled under
 * parallel test execution. Fixtures carry a `_test_ent59_` prefix.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent59_'
const GDPR = '32016R0679'
const AI_ACT = '32024R1689'

const SUMMARY =
  'Fixture obligation used by the ENT-59 citation integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

interface SeedCitation {
  kind: 'article' | 'recital' | 'annex'
  celex: string
  article?: number
  recital?: number
  annex?: string
  paragraph?: string
}

/** Seed an obligation with fully controlled citation columns. */
async function seedObligation(slug: string, c: SeedCitation): Promise<string> {
  const cols = {
    citation_article: c.article ?? null,
    citation_recital: c.recital ?? null,
    citation_annex: c.annex ? `'${c.annex}'` : null,
    citation_paragraph: c.paragraph ? `'${c.paragraph}'` : null,
  }
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind,
       citation_article, citation_recital, citation_annex, citation_paragraph,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}', '${c.celex}', '${c.kind}',
       ${cols.citation_article ?? 'null'}, ${cols.citation_recital ?? 'null'},
       ${cols.citation_annex ?? 'null'}, ${cols.citation_paragraph ?? 'null'},
       '{}'::jsonb, 'medium', null)
    on conflict (slug) do update set
      citation_celex     = excluded.citation_celex,
      citation_kind      = excluded.citation_kind,
      citation_article   = excluded.citation_article,
      citation_recital   = excluded.citation_recital,
      citation_annex     = excluded.citation_annex,
      citation_paragraph = excluded.citation_paragraph;
  `)
  const [{ id }] = await querySql<{ id: string }>(
    `select id from public.obligations where slug = $1`,
    [slug],
  )
  return id
}

describe.skipIf(!supabaseRunning)('analyst finding citations (ENT-59)', () => {
  let user: TestUser
  let profileId: string

  // slug → { signalId, obligationId }
  const emitted: Record<string, { signalId: string; obligationId: string }> = {}

  async function emitFor(slug: string): Promise<string> {
    const [{ id }] = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'deadline', $2::text, $3::text, 'detail', 'medium', $4::text, '{}'::jsonb
       ) as id`,
      [profileId, `cite:${slug}`, `Signal for ${slug}`, slug],
    )
    return id
  }

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
        has_ropa: 'yes',
        transfers_outside_eu: 'no',
      })
      .select('id')
      .single()
    profileId = profile!.id as string

    const cases: Array<[string, SeedCitation]> = [
      [`${PREFIX}article`, { kind: 'article', celex: GDPR, article: 30 }],
      [`${PREFIX}paragraph`, { kind: 'article', celex: GDPR, article: 30, paragraph: '1(b)' }],
      [`${PREFIX}annex`, { kind: 'annex', celex: AI_ACT, annex: 'III' }],
      [`${PREFIX}recital`, { kind: 'recital', celex: GDPR, recital: 47 }],
    ]
    for (const [slug, citation] of cases) {
      const obligationId = await seedObligation(slug, citation)
      const signalId = await emitFor(slug)
      emitted[slug] = { signalId, obligationId }
      // Convert this specific signal (not the profile-wide loop): integration
      // suites run in parallel and the global run_watcher() can inject extra
      // signals into this profile. Per-signal conversion keeps it hermetic.
      await querySql(`select public.analyst_convert_signal($1::uuid)`, [signalId])
    }
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // Findings are delete-protected (ON DELETE RESTRICT) while they cite an
    // obligation. Clear every finding referencing our fixture obligations (any
    // owner, sweeping interrupted prior runs) before dropping the obligations.
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  const findingForSlug = async (slug: string) => {
    const [row] = await querySql<{
      obligation_id: string | null
      regulatory_obligation: string | null
      citation_url: string | null
      supporting_context: string | null
    }>(
      `select obligation_id, regulatory_obligation, citation_url, supporting_context
       from public.findings where watcher_finding_id = $1::uuid`,
      [emitted[slug].signalId],
    )
    return row
  }

  it('links every finding to its obligation (non-null) and carries the precise citation label', async () => {
    const article = await findingForSlug(`${PREFIX}article`)
    expect(article.obligation_id).toBe(emitted[`${PREFIX}article`].obligationId)
    expect(article.regulatory_obligation).toBe('GDPR Art. 30')

    const paragraph = await findingForSlug(`${PREFIX}paragraph`)
    expect(paragraph.regulatory_obligation).toBe('GDPR Art. 30(1)(b)')

    const annex = await findingForSlug(`${PREFIX}annex`)
    expect(annex.regulatory_obligation).toBe('EU AI Act Annex III')

    const recital = await findingForSlug(`${PREFIX}recital`)
    expect(recital.regulatory_obligation).toBe('GDPR Recital 47')
  })

  it('cites the regulatory source by a resolvable ELI URL', async () => {
    const article = await findingForSlug(`${PREFIX}article`)
    expect(article.citation_url).toBe('https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_30')

    const annex = await findingForSlug(`${PREFIX}annex`)
    expect(annex.citation_url).toBe('https://eur-lex.europa.eu/eli/reg/2024/1689/oj#anx_III')

    const recital = await findingForSlug(`${PREFIX}recital`)
    expect(recital.citation_url).toBe('https://eur-lex.europa.eu/eli/reg/2016/679/oj#rct_47')

    // supporting_context (the obligation summary) is still present alongside the cite.
    expect(article.supporting_context).toBe(SUMMARY)
  })

  it('skips a signal whose slug resolves to no catalogue obligation (NOT NULL invariant)', async () => {
    const [{ id: orphanSignal }] = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'deadline', 'cite:orphan', 'Orphan signal', 'detail', 'medium',
         '${PREFIX}does-not-exist', '{}'::jsonb
       ) as id`,
      [profileId],
    )

    // The conversion returns NULL (no resolvable obligation) and writes nothing —
    // a finding can't satisfy the NOT NULL obligation link without one.
    const [{ analyst_convert_signal: produced }] = await querySql<{
      analyst_convert_signal: string | null
    }>(`select public.analyst_convert_signal($1::uuid)`, [orphanSignal])
    expect(produced).toBeNull()

    const orphanFinding = await querySql(
      `select 1 from public.findings where watcher_finding_id = $1::uuid`,
      [orphanSignal],
    )
    expect(orphanFinding).toHaveLength(0)
  })

  it('protects a cited obligation from deletion (ON DELETE RESTRICT)', async () => {
    await expect(
      applyFixtureSql(`delete from public.obligations where slug = '${PREFIX}article';`),
    ).rejects.toThrow(/23503|violates foreign key|still referenced/i)
  })
})
