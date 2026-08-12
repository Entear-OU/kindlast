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
 * ENT-96 — `regulatory_annexes` + `regulatory_annex_items` + per-article
 * `effective_date` schema.
 *
 * Sibling to `regulatory-corpus.test.ts` and `regulatory-article-paragraphs.test.ts`:
 * same RLS pattern (public read, service-role writes), same natural-key
 * idempotency. Differences worth a dedicated suite:
 *
 *   1. Annexes are children of documents, items are grand-children — verify
 *      FK cascade walks both levels.
 *   2. `annex_label` is TEXT containing Roman numerals; `item_label` is TEXT
 *      with mixed-format strings ("1", "1(a)"). Uniqueness coverage on both.
 *   3. `regulatory_articles.effective_date` is a new nullable column — null
 *      writable, non-null writable, and pre-existing rows survive the
 *      `alter table … add column` (idempotent migration).
 *   4. `regulatory_annex_items.heading` is nullable (top-level items have it,
 *      sub-items don't).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_ENT96_annex_celex'

const SEED_SQL = /* sql */ `
  insert into public.regulatory_documents (
    celex_number, title, short_title, version_date, official_url
  ) values (
    '${FIXTURE_CELEX}', 'Annex fixture', 'Annex fixture', '2024-07-12',
    'https://example.invalid/a'
  ) on conflict (celex_number) do nothing;
`

const CLEAN_SQL = /* sql */ `
  delete from public.regulatory_documents where celex_number = '${FIXTURE_CELEX}';
`

describe.skipIf(!supabaseRunning)('regulatory_annexes + items + effective_date (ENT-96)', () => {
  let documentId: string

  beforeAll(async () => {
    await applyFixtureSql(CLEAN_SQL)
    await applyFixtureSql(SEED_SQL)
    const rows = await querySql<{ id: string }>(
      `select id from public.regulatory_documents where celex_number = $1`,
      [FIXTURE_CELEX],
    )
    documentId = rows[0]!.id
  })

  afterAll(async () => {
    await dropFixtureSql(CLEAN_SQL)
  })

  describe('regulatory_annexes — schema shape', () => {
    it('has the expected columns including effective_date and summary (not body)', async () => {
      const cols = await querySql<{ column_name: string; is_nullable: string }>(
        `select column_name, is_nullable
           from information_schema.columns
           where table_schema = 'public' and table_name = 'regulatory_annexes'
           order by ordinal_position`,
      )
      expect(cols.map((c) => c.column_name)).toEqual([
        'id',
        'document_id',
        'annex_label',
        'heading',
        'summary',
        'effective_date',
        'created_at',
        'updated_at',
      ])
      const effective = cols.find((c) => c.column_name === 'effective_date')
      expect(effective?.is_nullable).toBe('YES')
    })
  })

  describe('regulatory_annex_items — schema shape', () => {
    it('has the expected columns; heading is nullable; summary not body', async () => {
      const cols = await querySql<{ column_name: string; is_nullable: string }>(
        `select column_name, is_nullable
           from information_schema.columns
           where table_schema = 'public' and table_name = 'regulatory_annex_items'
           order by ordinal_position`,
      )
      expect(cols.map((c) => c.column_name)).toEqual([
        'id',
        'annex_id',
        'item_label',
        'heading',
        'summary',
        'effective_date',
        'ordering',
        'created_at',
        'updated_at',
      ])
      const heading = cols.find((c) => c.column_name === 'heading')
      expect(heading?.is_nullable).toBe('YES')
    })
  })

  describe('regulatory_articles.effective_date — added by this migration', () => {
    it('exists as a nullable date column', async () => {
      const cols = await querySql<{ data_type: string; is_nullable: string }>(
        `select data_type, is_nullable
           from information_schema.columns
           where table_schema = 'public'
             and table_name = 'regulatory_articles'
             and column_name = 'effective_date'`,
      )
      expect(cols).toHaveLength(1)
      expect(cols[0]!.data_type).toBe('date')
      expect(cols[0]!.is_nullable).toBe('YES')
    })

    it('accepts both null and non-null values', async () => {
      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_articles (document_id, article_number, heading, summary, effective_date)
        values ('${documentId}', 1, 'h', 'fixture summary', '2025-02-02')
        on conflict (document_id, article_number) do nothing;
        insert into public.regulatory_articles (document_id, article_number, heading, summary)
        values ('${documentId}', 2, 'h2', 'fixture summary')
        on conflict (document_id, article_number) do nothing;
      `)
      const rows = await querySql<{ article_number: number; effective_date: string | null }>(
        `select article_number, effective_date::text
           from public.regulatory_articles
           where document_id = $1
           order by article_number`,
        [documentId],
      )
      expect(rows[0]!.effective_date).toBe('2025-02-02')
      expect(rows[1]!.effective_date).toBeNull()
    })
  })

  describe('row-level security', () => {
    // Fixture summaries are ≥100 chars so they satisfy the CHECK constraint.
    const ANNEX_SUMMARY =
      'Annex III enumerates AI use cases the Regulation designates high-risk under Article 6(2). Listed systems trigger Articles 9–17 obligations.'
    const ITEM_SUMMARY =
      'Category 1 — Biometrics: AI systems used to identify, categorise, or recognise the emotional state of natural persons, subject to applicable Union or Member State law.'

    it('allows anon read of annexes + items', async () => {
      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_annexes (document_id, annex_label, heading, summary, effective_date)
        values ('${documentId}', 'III', 'High-risk AI systems', '${ANNEX_SUMMARY}', '2026-08-02')
        on conflict (document_id, annex_label) do nothing;
        insert into public.regulatory_annex_items (annex_id, item_label, heading, summary, ordering)
        select a.id, '1', 'Biometrics', '${ITEM_SUMMARY}', 1
          from public.regulatory_annexes a
          where a.document_id = '${documentId}' and a.annex_label = 'III'
        on conflict (annex_id, item_label) do nothing;
      `)

      const anon = createAnonClient()
      const { data, error } = await anon
        .from('regulatory_annexes')
        .select('annex_label, heading, effective_date')
        .eq('document_id', documentId)
        .single()
      expect(error).toBeNull()
      expect(data?.annex_label).toBe('III')
      expect(data?.effective_date).toBe('2026-08-02')
    })

    it('denies anon insert to annexes', async () => {
      const anon = createAnonClient()
      const { error } = await anon.from('regulatory_annexes').insert({
        document_id: documentId,
        annex_label: 'should-not-write',
        heading: 'x',
        summary: ANNEX_SUMMARY,
      })
      expect(error).not.toBeNull()
      expect(error?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
    })

    it('denies anon insert to annex items', async () => {
      const anon = createAnonClient()
      const { error } = await anon.from('regulatory_annex_items').insert({
        annex_id: documentId, // any UUID — the rejection should be RLS, not FK
        item_label: 'x',
        summary: ITEM_SUMMARY,
        ordering: 0,
      })
      expect(error).not.toBeNull()
      expect(error?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
    })

    it('allows the service-role client to upsert annexes', async () => {
      const service = createServiceRoleClient()
      const { error } = await service.from('regulatory_annexes').upsert(
        {
          document_id: documentId,
          annex_label: 'III',
          heading: 'High-risk AI systems (updated)',
          summary: ANNEX_SUMMARY,
          effective_date: '2026-08-02',
        },
        { onConflict: 'document_id,annex_label' },
      )
      expect(error).toBeNull()
    })
  })

  describe('uniqueness + cascade', () => {
    // Local fixture summaries — long enough to clear the CHECK constraint.
    const FIX_SUMMARY =
      'Fixture summary covering the high-risk use case category for test purposes; not a verbatim OJ extract — used only to satisfy the CHECK length floor.'

    it('enforces unique (document_id, annex_label)', async () => {
      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.regulatory_annexes (document_id, annex_label, heading, summary)
          values ('${documentId}', 'III', 'duplicate', '${FIX_SUMMARY}');
        `),
      ).rejects.toThrow(/duplicate|unique/i)
    })

    it('enforces unique (annex_id, item_label) with mixed-format labels', async () => {
      const annexRows = await querySql<{ id: string }>(
        `select id from public.regulatory_annexes where document_id = $1 and annex_label = 'III'`,
        [documentId],
      )
      const annexId = annexRows[0]!.id

      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_annex_items (annex_id, item_label, summary, ordering)
        values ('${annexId}', '1(a)', '${FIX_SUMMARY}', 99)
        on conflict (annex_id, item_label) do nothing;
      `)
      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.regulatory_annex_items (annex_id, item_label, summary, ordering)
          values ('${annexId}', '1(a)', '${FIX_SUMMARY}', 100);
        `),
      ).rejects.toThrow(/duplicate|unique/i)
    })

    it('cascades document delete → annex → annex_items', async () => {
      const ISO_CELEX = '_TEST_ENT96_annex_cascade'
      await applyFixtureSql(/* sql */ `
        delete from public.regulatory_documents where celex_number = '${ISO_CELEX}';
        insert into public.regulatory_documents (celex_number, title, short_title, version_date, official_url)
        values ('${ISO_CELEX}', 'c', 'c', '2024-07-12', 'https://example.invalid/c');
        insert into public.regulatory_annexes (document_id, annex_label, heading, summary)
        select d.id, 'X', 'h', '${FIX_SUMMARY}' from public.regulatory_documents d
        where d.celex_number = '${ISO_CELEX}';
        insert into public.regulatory_annex_items (annex_id, item_label, summary, ordering)
        select a.id, '1', '${FIX_SUMMARY}', 1 from public.regulatory_annexes a
        join public.regulatory_documents d on d.id = a.document_id
        where d.celex_number = '${ISO_CELEX}';
      `)

      const annexRows = await querySql<{ id: string }>(
        `select a.id from public.regulatory_annexes a
           join public.regulatory_documents d on d.id = a.document_id
           where d.celex_number = $1`,
        [ISO_CELEX],
      )
      const isoAnnexId = annexRows[0]!.id

      const before = await querySql<{ c: number }>(
        `select count(*)::int as c from public.regulatory_annex_items where annex_id = $1`,
        [isoAnnexId],
      )
      expect(before[0]!.c).toBe(1)

      await applyFixtureSql(`delete from public.regulatory_documents where celex_number = '${ISO_CELEX}';`)

      const afterAnnex = await querySql<{ c: number }>(
        `select count(*)::int as c from public.regulatory_annexes where id = $1`,
        [isoAnnexId],
      )
      const afterItems = await querySql<{ c: number }>(
        `select count(*)::int as c from public.regulatory_annex_items where annex_id = $1`,
        [isoAnnexId],
      )
      expect(afterAnnex[0]!.c).toBe(0)
      expect(afterItems[0]!.c).toBe(0)
    })
  })

  it('migration body is idempotent (re-applying its DDL does not error)', async () => {
    const REAPPLY = /* sql */ `
      create table if not exists public.regulatory_annexes (
        id             uuid        primary key default gen_random_uuid(),
        document_id    uuid        not null
                         references public.regulatory_documents(id) on delete cascade,
        annex_label    text        not null,
        heading        text        not null,
        summary        text        not null,
        effective_date date,
        created_at     timestamptz not null default now(),
        updated_at     timestamptz not null default now(),
        unique (document_id, annex_label),
        constraint regulatory_annexes_summary_length
          check (char_length(summary) between 100 and 2000)
      );
      alter table public.regulatory_annexes enable row level security;
      drop policy if exists "regulatory_annexes_select_public" on public.regulatory_annexes;
      create policy "regulatory_annexes_select_public" on public.regulatory_annexes
        for select using (true);
      alter table public.regulatory_articles
        add column if not exists effective_date date;
    `
    await expect(applyFixtureSql(REAPPLY)).resolves.toBeUndefined()
  })
})
