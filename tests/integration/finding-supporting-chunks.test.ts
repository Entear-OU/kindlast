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
 * ENT-64 — corpus-backed "supporting chunks" for a finding's detail view.
 *
 * public.finding_supporting_chunks(p_finding_id) is a SECURITY DEFINER read
 * model: it reads the public regulatory corpus regardless of caller, but gates
 * on auth.uid() so a user-scoped client only ever sees its own finding's chunks
 * (a missing/foreign id returns zero rows, never an error).
 *
 * Acceptance criteria exercised here:
 *   * Article WITH corpus → chunk 1 is the article (heading + body), chunks 2..N
 *     are its sub-paragraphs in `ordering` order, deep-labelled "GDPR Art. N(...)".
 *   * No corpus for the cited article → a single fallback chunk built from the
 *     finding's denormalised citation (supporting_context / citation_url).
 *   * A foreign caller gets zero rows.
 *
 * Findings are produced through the real pipeline (emit a Watcher signal →
 * analyst_convert_signal) so each row satisfies every Analyst-era constraint.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent64_'
const CELEX = '32016R0679'

// Distinct article numbers per case so the natural-key join is unambiguous and
// the fallback case genuinely has no matching corpus row.
const CORPUS_ARTICLE = 30
const FALLBACK_ARTICLE = 47

const SUMMARY =
  'Fixture obligation for the ENT-64 supporting-chunks test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length for a catalogue row.'

async function seedObligation(slug: string, article: number): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${slug}', 'Fixture ${slug}', '${SUMMARY}',
       '${CELEX}', 'article', ${article}, '{"role":"controller"}'::jsonb, 'high', null)
    on conflict (slug) do nothing;
  `)
}

// Each call needs a distinct signal: emit_watcher_finding dedups on
// (profile_id, dedup_key) and analyst_convert_signal upserts on
// watcher_finding_id, so a constant key would hand every test the same finding.
let findingSeq = 0

/** Emit a signal and convert it to a finding, returning the finding id. */
async function makeFinding(profileId: string, slug: string): Promise<string> {
  const dedupKey = `gap:${slug}:${(findingSeq += 1)}`
  const [{ id: signalId }] = await querySql<{ id: string }>(
    `select public.emit_watcher_finding(
       $1::uuid, 'profile_gap', $2::text, $3::text, $4::text, 'high', $5::text, '{}'::jsonb
     ) as id`,
    [profileId, dedupKey, `Profile gap: ${slug}`, 'A ROPA entry is missing for this activity.', slug],
  )
  const [{ id: findingId }] = await querySql<{ id: string }>(
    `select public.analyst_convert_signal($1::uuid) as id`,
    [signalId],
  )
  return findingId
}

interface Chunk {
  ordinal: number
  label: string
  quoted_text: string
  source_url: string | null
  [key: string]: unknown
}

interface FindingCitation {
  regulatory_obligation: string | null
  supporting_context: string | null
  citation_url: string | null
  [key: string]: unknown
}

// ENT-97 rotated the corpus to a curated `summary` (the routing artifact the
// chunk quotes), not mirrored verbatim prose.
const ARTICLE_HEADING = 'Records of processing activities'
const ARTICLE_SUMMARY =
  'Each controller shall maintain a record of processing activities under its responsibility.'
const PARA_1_SUMMARY = 'That record shall contain the name and contact details of the controller.'
const PARA_2_SUMMARY = 'The records shall be in writing, including in electronic form.'

describe.skipIf(!supabaseRunning)('finding supporting chunks (ENT-64)', () => {
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
    const { data: profile, error } = await admin
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
    expect(error).toBeNull()
    profileId = profile!.id as string

    await seedObligation(`${PREFIX}article`, CORPUS_ARTICLE)
    await seedObligation(`${PREFIX}fallback`, FALLBACK_ARTICLE)

    // Seed the corpus for the article case only (CORPUS_ARTICLE), leaving the
    // fallback obligation's article (FALLBACK_ARTICLE) with no corpus row.
    await applyFixtureSql(`
      insert into public.regulatory_documents
        (celex_number, title, short_title, version_date, official_url)
      values
        ('${CELEX}', 'Fixture GDPR ${PREFIX}', 'GDPR', '2016-04-27',
         'https://eur-lex.europa.eu/eli/reg/2016/679/oj')
      on conflict (celex_number) do nothing;

      insert into public.regulatory_articles
        (document_id, article_number, heading, summary)
      select d.id, ${CORPUS_ARTICLE}, '${ARTICLE_HEADING}', '${ARTICLE_SUMMARY}'
        from public.regulatory_documents d
       where d.celex_number = '${CELEX}'
      on conflict (document_id, article_number) do nothing;

      insert into public.regulatory_article_paragraphs
        (article_id, paragraph_label, summary, ordering)
      select a.id, '1(a)', '${PARA_1_SUMMARY}', 1
        from public.regulatory_articles a
        join public.regulatory_documents d on d.id = a.document_id
       where d.celex_number = '${CELEX}' and a.article_number = ${CORPUS_ARTICLE}
      on conflict (article_id, paragraph_label) do nothing;

      insert into public.regulatory_article_paragraphs
        (article_id, paragraph_label, summary, ordering)
      select a.id, '2(b)', '${PARA_2_SUMMARY}', 2
        from public.regulatory_articles a
        join public.regulatory_documents d on d.id = a.document_id
       where d.celex_number = '${CELEX}' and a.article_number = ${CORPUS_ARTICLE}
      on conflict (article_id, paragraph_label) do nothing;
    `)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // Findings cite the (delete-protected) fixture obligations — clear them
    // first, then the obligations, then the corpus rows we inserted by CELEX.
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
      delete from public.regulatory_documents where celex_number = '${CELEX}'
        and title = 'Fixture GDPR ${PREFIX}';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('article with corpus: article chunk then its paragraphs in order', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}article`)
    const owner = await createUserClient(user.email, user.password)

    const { data, error } = await owner.rpc('finding_supporting_chunks', {
      p_finding_id: findingId,
    })
    expect(error).toBeNull()
    const chunks = (data as Chunk[]).sort((a, b) => a.ordinal - b.ordinal)
    expect(chunks).toHaveLength(3)

    // Chunk 1: the article (heading + body).
    expect(chunks[0].ordinal).toBe(1)
    expect(chunks[0].label).toBe(`GDPR Art. ${CORPUS_ARTICLE}`)
    expect(chunks[0].quoted_text).toContain(ARTICLE_HEADING)
    expect(chunks[0].quoted_text).toContain(ARTICLE_SUMMARY)
    expect(chunks[0].source_url).toContain(`#art_${CORPUS_ARTICLE}`)

    // Chunks 2,3: the paragraphs in `ordering` order, deep-labelled.
    expect(chunks[1].ordinal).toBe(2)
    expect(chunks[1].label).toBe(`GDPR Art. ${CORPUS_ARTICLE}(1)(a)`)
    expect(chunks[1].quoted_text).toBe(PARA_1_SUMMARY)

    expect(chunks[2].ordinal).toBe(3)
    expect(chunks[2].label).toBe(`GDPR Art. ${CORPUS_ARTICLE}(2)(b)`)
    expect(chunks[2].quoted_text).toBe(PARA_2_SUMMARY)
  })

  it('no corpus: a single fallback chunk from the finding citation', async () => {
    const findingId = await makeFinding(profileId, `${PREFIX}fallback`)
    const owner = await createUserClient(user.email, user.password)

    const { data, error } = await owner.rpc('finding_supporting_chunks', {
      p_finding_id: findingId,
    })
    expect(error).toBeNull()
    const chunks = data as Chunk[]
    expect(chunks).toHaveLength(1)
    expect(chunks[0].ordinal).toBe(1)

    const [finding] = await querySql<FindingCitation>(
      `select regulatory_obligation, supporting_context, citation_url
         from public.findings where id = $1::uuid`,
      [findingId],
    )
    expect(chunks[0].label).toBe(finding.regulatory_obligation)
    expect(chunks[0].quoted_text).toBe(finding.supporting_context)
    expect(chunks[0].source_url).toBe(finding.citation_url)
  })

  it('a foreign caller gets zero rows', async () => {
    const admin = createServiceRoleClient()
    const findingId = await makeFinding(profileId, `${PREFIX}article`)
    const other = await signUpTestUser(admin)
    try {
      const intruder = await createUserClient(other.email, other.password)
      const { data, error } = await intruder.rpc('finding_supporting_chunks', {
        p_finding_id: findingId,
      })
      expect(error).toBeNull()
      expect(data as Chunk[]).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
