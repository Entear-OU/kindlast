// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  applyFixtureSql,
  dropFixtureSql,
  querySql,
} from './helpers/db-fixture'
import {
  createAnonClient,
  createServiceRoleClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'

/**
 * ENT-95 — `regulatory_article_paragraphs` schema + RLS.
 *
 * Sibling to `regulatory-corpus.test.ts`: same RLS pattern (public read,
 * service-role writes), same `(article_id, paragraph_label)` natural-key
 * idempotency story. Differences worth a dedicated suite:
 *
 *   1. The natural key includes a TEXT column (paragraph_label can be
 *      "1", "1(a)", "1(a)(i)") — needs uniqueness coverage on the
 *      mixed-format key.
 *   2. FK cascade from `regulatory_articles` (not from `regulatory_documents`
 *      directly) — different parent than the corpus tables above.
 *   3. `ordering` is mutable; re-ingest may shift display order without
 *      changing the row identity.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_ENT95_paragraphs_celex'

// Short fixture summary — DB CHECK on legacy tables is 1..2000 (Zod is the
// strict 100-char floor on ingest); short fixture strings keep the SQL terse.
const FIX_SUMMARY = 'fixture summary'

const SEED_SQL = /* sql */ `
  insert into public.regulatory_documents (
    celex_number, title, short_title, version_date, official_url
  ) values (
    '${FIXTURE_CELEX}', 'Paragraph fixture', 'Paragraph fixture', '2024-07-12',
    'https://example.invalid/p'
  ) on conflict (celex_number) do nothing;

  insert into public.regulatory_articles (document_id, article_number, heading, summary)
  select d.id, 1, 'Test article', '${FIX_SUMMARY}'
  from public.regulatory_documents d
  where d.celex_number = '${FIXTURE_CELEX}'
  on conflict (document_id, article_number) do nothing;
`

const CLEAN_SQL = /* sql */ `
  delete from public.regulatory_documents where celex_number = '${FIXTURE_CELEX}';
`

describe.skipIf(!supabaseRunning)('regulatory_article_paragraphs schema (ENT-95)', () => {
  let articleId: string

  beforeAll(async () => {
    await applyFixtureSql(CLEAN_SQL)
    await applyFixtureSql(SEED_SQL)
    const rows = await querySql<{ id: string }>(
      `select a.id
         from public.regulatory_articles a
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and a.article_number = 1`,
      [FIXTURE_CELEX],
    )
    articleId = rows[0]!.id
  })

  afterAll(async () => {
    await dropFixtureSql(CLEAN_SQL)
  })

  describe('schema shape', () => {
    it('has the expected columns + FK to regulatory_articles', async () => {
      const cols = await querySql<{ column_name: string }>(
        `select column_name
           from information_schema.columns
           where table_schema = 'public' and table_name = 'regulatory_article_paragraphs'
           order by ordinal_position`,
      )
      // body dropped by ENT-97; replaced with summary (progressive disclosure).
      expect(cols.map((c) => c.column_name)).toEqual([
        'id',
        'article_id',
        'paragraph_label',
        'ordering',
        'created_at',
        'updated_at',
        'summary',
      ])
    })
  })

  describe('row-level security', () => {
    it('allows anon read of paragraphs', async () => {
      // Seed a paragraph via direct pg (bypasses RLS), then read via anon.
      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_article_paragraphs (article_id, paragraph_label, summary, ordering)
        values ('${articleId}', '1', '${FIX_SUMMARY}', 1)
        on conflict (article_id, paragraph_label) do nothing;
      `)

      const anon = createAnonClient()
      const { data, error } = await anon
        .from('regulatory_article_paragraphs')
        .select('paragraph_label, summary, ordering')
        .eq('article_id', articleId)
        .eq('paragraph_label', '1')
        .single()
      expect(error).toBeNull()
      expect(data?.summary).toBe(FIX_SUMMARY)
    })

    it('denies anon insert', async () => {
      const anon = createAnonClient()
      const { error } = await anon.from('regulatory_article_paragraphs').insert({
        article_id: articleId,
        paragraph_label: 'should-not-write',
        summary: FIX_SUMMARY,
        ordering: 0,
      })
      expect(error).not.toBeNull()
      expect(error?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
    })

    it('allows the service-role client to upsert', async () => {
      const service = createServiceRoleClient()
      const { error } = await service.from('regulatory_article_paragraphs').upsert(
        {
          article_id: articleId,
          paragraph_label: 'service-test',
          summary: 'inserted via service-role',
          ordering: 999,
        },
        { onConflict: 'article_id,paragraph_label' },
      )
      expect(error).toBeNull()
    })
  })

  describe('uniqueness + cascade', () => {
    it('enforces unique (article_id, paragraph_label) with mixed-format labels', async () => {
      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_article_paragraphs (article_id, paragraph_label, summary, ordering)
        values ('${articleId}', '2(a)', '${FIX_SUMMARY}', 1)
        on conflict (article_id, paragraph_label) do nothing;
      `)
      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.regulatory_article_paragraphs (article_id, paragraph_label, summary, ordering)
          values ('${articleId}', '2(a)', '${FIX_SUMMARY}', 2);
        `),
      ).rejects.toThrow(/duplicate|unique/i)
    })

    it('cascades from regulatory_articles', async () => {
      // Seed an isolated article so we can drop it without touching the
      // shared fixture article above.
      const ISO_CELEX = '_TEST_ENT95_paragraphs_cascade'
      await applyFixtureSql(/* sql */ `
        delete from public.regulatory_documents where celex_number = '${ISO_CELEX}';
        insert into public.regulatory_documents (
          celex_number, title, short_title, version_date, official_url
        ) values ('${ISO_CELEX}', 'c', 'c', '2024-07-12', 'https://example.invalid/c');
        insert into public.regulatory_articles (document_id, article_number, heading, summary)
        select d.id, 1, 'h', '${FIX_SUMMARY}' from public.regulatory_documents d
        where d.celex_number = '${ISO_CELEX}';
        insert into public.regulatory_article_paragraphs (article_id, paragraph_label, summary, ordering)
        select a.id, '1', '${FIX_SUMMARY}', 1 from public.regulatory_articles a
        join public.regulatory_documents d on d.id = a.document_id
        where d.celex_number = '${ISO_CELEX}';
      `)

      const articleRows = await querySql<{ id: string }>(
        `select a.id from public.regulatory_articles a
           join public.regulatory_documents d on d.id = a.document_id
           where d.celex_number = $1 and a.article_number = 1`,
        [ISO_CELEX],
      )
      const isoArticleId = articleRows[0]!.id

      const before = await querySql<{ c: number }>(
        `select count(*)::int as c from public.regulatory_article_paragraphs where article_id = $1`,
        [isoArticleId],
      )
      expect(before[0]!.c).toBe(1)

      await applyFixtureSql(`delete from public.regulatory_documents where celex_number = '${ISO_CELEX}';`)

      const after = await querySql<{ c: number }>(
        `select count(*)::int as c from public.regulatory_article_paragraphs where article_id = $1`,
        [isoArticleId],
      )
      expect(after[0]!.c).toBe(0)
    })
  })

  it('migration body is idempotent (re-applying its DDL does not error)', async () => {
    const REAPPLY = /* sql */ `
      create table if not exists public.regulatory_article_paragraphs (
        id              uuid        primary key default gen_random_uuid(),
        article_id      uuid        not null
                          references public.regulatory_articles(id) on delete cascade,
        paragraph_label text        not null,
        summary         text        not null,
        ordering        int         not null,
        created_at      timestamptz not null default now(),
        updated_at      timestamptz not null default now(),
        unique (article_id, paragraph_label)
      );
      alter table public.regulatory_article_paragraphs enable row level security;
      drop policy if exists "regulatory_article_paragraphs_select_public"
        on public.regulatory_article_paragraphs;
      create policy "regulatory_article_paragraphs_select_public"
        on public.regulatory_article_paragraphs
        for select using (true);
    `
    await expect(applyFixtureSql(REAPPLY)).resolves.toBeUndefined()
  })
})
