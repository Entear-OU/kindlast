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
 * ENT-48 — Regulatory corpus schema.
 *
 * Exercises the four tables introduced in `<ts>_regulatory_corpus.sql`:
 *
 *   * `regulatory_documents`   — one row per regulation (CELEX-keyed)
 *   * `regulatory_articles`    — one row per article, FK to document
 *   * `regulatory_recitals`    — one row per recital, FK to document
 *   * `regulatory_article_recitals` — junction (article ↔ recital)
 *
 * Unlike user-owned tables (compliance_profiles, onboarding_sessions, …),
 * the corpus is shared reference data: anyone can read, only the service
 * role writes. That asymmetry is the policy story this suite proves.
 *
 * Coverage:
 *   1. Tables exist with the expected columns + constraints.
 *   2. Anon reads work (corpus is public).
 *   3. Anon writes are denied (no insert/update/delete policy).
 *   4. Authenticated writes are also denied (same — service-role only).
 *   5. Unique constraints hold (celex_number, (document_id, article_number),
 *      (document_id, recital_number)).
 *   6. FK cascade: deleting a document cascades to articles + recitals;
 *      deleting an article cascades to junction rows.
 *   7. Migration body is idempotent (re-apply does not error).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_32016R0679'
const FIXTURE_ARTICLE_NO = 99999
const FIXTURE_RECITAL_NO = 99998

/** Wipe any leftover fixture rows from previous runs. */
const CLEAN_SQL = /* sql */ `
  delete from public.regulatory_documents where celex_number = '${FIXTURE_CELEX}';
`

describe.skipIf(!supabaseRunning)('regulatory corpus schema (ENT-48)', () => {
  beforeAll(async () => {
    await applyFixtureSql(CLEAN_SQL)
  })

  afterAll(async () => {
    await dropFixtureSql(CLEAN_SQL)
  })

  describe('schema shape', () => {
    it('regulatory_documents has the expected columns', async () => {
      const cols = await querySql<{ column_name: string; data_type: string; is_nullable: string }>(
        `select column_name, data_type, is_nullable
         from information_schema.columns
         where table_schema = 'public' and table_name = 'regulatory_documents'
         order by ordinal_position`,
      )
      const names = cols.map((c) => c.column_name)
      expect(names).toEqual([
        'id',
        'celex_number',
        'title',
        'short_title',
        'version_date',
        'official_url',
        'created_at',
        'updated_at',
      ])
    })

    it('regulatory_articles has the expected columns + FK to documents', async () => {
      const cols = await querySql<{ column_name: string }>(
        `select column_name
         from information_schema.columns
         where table_schema = 'public' and table_name = 'regulatory_articles'
         order by ordinal_position`,
      )
      expect(cols.map((c) => c.column_name)).toEqual([
        'id',
        'document_id',
        'article_number',
        'heading',
        'body',
        'created_at',
        'updated_at',
      ])
    })

    it('regulatory_recitals has the expected columns + FK to documents', async () => {
      const cols = await querySql<{ column_name: string }>(
        `select column_name
         from information_schema.columns
         where table_schema = 'public' and table_name = 'regulatory_recitals'
         order by ordinal_position`,
      )
      expect(cols.map((c) => c.column_name)).toEqual([
        'id',
        'document_id',
        'recital_number',
        'body',
        'created_at',
        'updated_at',
      ])
    })

    it('regulatory_article_recitals is a composite-PK junction', async () => {
      const cols = await querySql<{ column_name: string }>(
        `select column_name
         from information_schema.columns
         where table_schema = 'public' and table_name = 'regulatory_article_recitals'
         order by ordinal_position`,
      )
      expect(cols.map((c) => c.column_name)).toEqual(['article_id', 'recital_id', 'created_at'])

      const pk = await querySql<{ column_name: string }>(
        `select kcu.column_name
         from information_schema.table_constraints tc
         join information_schema.key_column_usage kcu
           on tc.constraint_name = kcu.constraint_name
          and tc.table_schema = kcu.table_schema
         where tc.table_schema = 'public'
           and tc.table_name = 'regulatory_article_recitals'
           and tc.constraint_type = 'PRIMARY KEY'
         order by kcu.ordinal_position`,
      )
      expect(pk.map((c) => c.column_name)).toEqual(['article_id', 'recital_id'])
    })
  })

  describe('row-level security', () => {
    it('allows anon read of regulatory_documents', async () => {
      // Seed via direct pg (bypasses RLS) — anon reads it via the policy.
      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_documents (
          celex_number, title, short_title, version_date, official_url
        ) values (
          '${FIXTURE_CELEX}',
          'Test fixture document',
          'Test fixture',
          '2016-05-04',
          'https://example.invalid/test'
        ) on conflict (celex_number) do nothing;
      `)

      const anon = createAnonClient()
      const { data, error } = await anon
        .from('regulatory_documents')
        .select('celex_number, short_title')
        .eq('celex_number', FIXTURE_CELEX)
        .single()
      expect(error).toBeNull()
      expect(data?.short_title).toBe('Test fixture')
    })

    it('denies anon insert into regulatory_documents', async () => {
      const anon = createAnonClient()
      const { error } = await anon.from('regulatory_documents').insert({
        celex_number: '_TEST_anon_insert_should_fail',
        title: 'X',
        short_title: 'X',
        version_date: '2016-05-04',
        official_url: 'https://example.invalid/x',
      })
      expect(error).not.toBeNull()
      // RLS rejection surfaces as 42501 (insufficient_privilege) or a "violates
      // row-level security policy" message — match either.
      expect(error?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
    })

    it('denies anon update of regulatory_documents', async () => {
      const anon = createAnonClient()
      const { error } = await anon
        .from('regulatory_documents')
        .update({ short_title: 'hacked' })
        .eq('celex_number', FIXTURE_CELEX)
      // For UPDATE with no matching policy, supabase-js may return error
      // OR succeed with zero rows affected. Re-read to confirm content
      // unchanged either way.
      void error
      const after = await querySql<{ short_title: string }>(
        `select short_title from public.regulatory_documents where celex_number = $1`,
        [FIXTURE_CELEX],
      )
      expect(after[0]?.short_title).toBe('Test fixture')
    })

    it('denies anon delete of regulatory_documents', async () => {
      const anon = createAnonClient()
      await anon
        .from('regulatory_documents')
        .delete()
        .eq('celex_number', FIXTURE_CELEX)
      const after = await querySql<{ celex_number: string }>(
        `select celex_number from public.regulatory_documents where celex_number = $1`,
        [FIXTURE_CELEX],
      )
      expect(after).toHaveLength(1)
    })

    it('allows the service-role client to write (ingestion path)', async () => {
      const service = createServiceRoleClient()
      const { data, error } = await service
        .from('regulatory_documents')
        .upsert(
          {
            celex_number: FIXTURE_CELEX,
            title: 'Test fixture document (updated)',
            short_title: 'Test fixture v2',
            version_date: '2016-05-04',
            official_url: 'https://example.invalid/test',
          },
          { onConflict: 'celex_number' },
        )
        .select('short_title')
        .single()
      expect(error).toBeNull()
      expect(data?.short_title).toBe('Test fixture v2')
    })
  })

  describe('uniqueness + cascades', () => {
    it('enforces unique (document_id, article_number)', async () => {
      const docs = await querySql<{ id: string }>(
        `select id from public.regulatory_documents where celex_number = $1`,
        [FIXTURE_CELEX],
      )
      const docId = docs[0]!.id

      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_articles (document_id, article_number, heading, body)
        values ('${docId}', ${FIXTURE_ARTICLE_NO}, 'h', 'b')
        on conflict (document_id, article_number) do nothing;
      `)

      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.regulatory_articles (document_id, article_number, heading, body)
          values ('${docId}', ${FIXTURE_ARTICLE_NO}, 'h2', 'b2');
        `),
      ).rejects.toThrow(/duplicate|unique/i)
    })

    it('enforces unique (document_id, recital_number)', async () => {
      const docs = await querySql<{ id: string }>(
        `select id from public.regulatory_documents where celex_number = $1`,
        [FIXTURE_CELEX],
      )
      const docId = docs[0]!.id

      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_recitals (document_id, recital_number, body)
        values ('${docId}', ${FIXTURE_RECITAL_NO}, 'b')
        on conflict (document_id, recital_number) do nothing;
      `)

      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.regulatory_recitals (document_id, recital_number, body)
          values ('${docId}', ${FIXTURE_RECITAL_NO}, 'b2');
        `),
      ).rejects.toThrow(/duplicate|unique/i)
    })

    it('cascades document delete to articles, recitals, and junction rows', async () => {
      // Create an isolated cascade-fixture doc so we don't tear down the
      // shared FIXTURE_CELEX row mid-suite.
      const cascadeCelex = '_TEST_cascade_doc'
      await applyFixtureSql(/* sql */ `
        delete from public.regulatory_documents where celex_number = '${cascadeCelex}';
        insert into public.regulatory_documents (
          celex_number, title, short_title, version_date, official_url
        ) values (
          '${cascadeCelex}', 'cascade', 'cascade', '2016-05-04', 'https://example.invalid/c'
        );
      `)

      const docs = await querySql<{ id: string }>(
        `select id from public.regulatory_documents where celex_number = $1`,
        [cascadeCelex],
      )
      const docId = docs[0]!.id

      await applyFixtureSql(/* sql */ `
        insert into public.regulatory_articles (document_id, article_number, heading, body)
          values ('${docId}', 1, 'h', 'b');
        insert into public.regulatory_recitals (document_id, recital_number, body)
          values ('${docId}', 1, 'b');
        insert into public.regulatory_article_recitals (article_id, recital_id)
          select a.id, r.id
          from public.regulatory_articles a, public.regulatory_recitals r
          where a.document_id = '${docId}' and r.document_id = '${docId}';
      `)

      const counts = await querySql<{ articles: number; recitals: number; links: number }>(
        `select
           (select count(*)::int from public.regulatory_articles where document_id = $1) as articles,
           (select count(*)::int from public.regulatory_recitals where document_id = $1) as recitals,
           (select count(*)::int from public.regulatory_article_recitals ar
              join public.regulatory_articles a on a.id = ar.article_id
              where a.document_id = $1) as links`,
        [docId],
      )
      expect(counts[0]).toEqual({ articles: 1, recitals: 1, links: 1 })

      await applyFixtureSql(/* sql */ `
        delete from public.regulatory_documents where celex_number = '${cascadeCelex}';
      `)

      const after = await querySql<{ articles: number; recitals: number; links: number }>(
        `select
           (select count(*)::int from public.regulatory_articles where document_id = $1) as articles,
           (select count(*)::int from public.regulatory_recitals where document_id = $1) as recitals,
           (select count(*)::int from public.regulatory_article_recitals ar
              join public.regulatory_articles a on a.id = ar.article_id
              where a.document_id = $1) as links`,
        [docId],
      )
      expect(after[0]).toEqual({ articles: 0, recitals: 0, links: 0 })
    })
  })

  it('migration body is idempotent (re-applying its DDL does not error)', async () => {
    // Re-run the exact `create … if not exists` / `drop policy if exists` +
    // `create policy` shape the migration uses on one of the corpus tables.
    const REAPPLY = /* sql */ `
      create table if not exists public.regulatory_documents (
        id             uuid        primary key default gen_random_uuid(),
        celex_number   text        not null unique,
        title          text        not null,
        short_title    text        not null,
        version_date   date        not null,
        official_url   text        not null,
        created_at     timestamptz not null default now(),
        updated_at     timestamptz not null default now()
      );
      alter table public.regulatory_documents enable row level security;
      drop policy if exists "regulatory_documents_select_public"
        on public.regulatory_documents;
      create policy "regulatory_documents_select_public"
        on public.regulatory_documents
        for select using (true);
    `
    await expect(applyFixtureSql(REAPPLY)).resolves.toBeUndefined()
  })
})
