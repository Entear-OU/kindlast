-- Drop mirrored body columns; add curated summary on legacy corpus tables (ENT-97)
--
-- Completes the progressive disclosure migration adopted on ENT-32. Before this
-- migration the three "legacy" corpus tables (ENT-48 / ENT-94 / ENT-95) still
-- stored verbatim OJ prose in `body` columns. Under the catalog model the
-- Analyst pulls verbatim text from the document's `official_url` at citation
-- time via a Tavily/Firecrawl-backed websearch tool, so the local mirror is
-- pure deadweight — and a staleness/maintenance trap when the EU amends
-- GDPR or the AI Act.
--
-- Three tables are rotated to the same shape the annex tables already use
-- (ENT-96): structural natural keys + a curated `summary` (100..2000 chars).
--
--   1. regulatory_articles
--   2. regulatory_recitals
--   3. regulatory_article_paragraphs
--
-- Each summary is a routing artifact — short curated prose that helps an LLM
-- decide whether to fetch this article/recital/paragraph's source URL when
-- answering a compliance question. The DB CHECK enforces the same length
-- bounds the Zod validator enforces in `lib/corpus/ingest.ts`, so curators
-- get a clear error before the upsert.
--
-- The `source_url` for each row is derived at runtime from
-- `regulatory_documents.official_url` plus the natural key (article_number,
-- recital_number, etc.) — EUR-Lex's ELI anchor pattern is deterministic per
-- regulation, so no per-row URL column is needed.
--
-- This migration is destructive on the `body` columns. The current snapshot
-- files in `data/corpus/*.json` are re-authored in the same PR to populate
-- `summary` for every row, so re-running `pnpm ingest:gdpr` and
-- `pnpm ingest:ai-act` after the migration produces a fully populated
-- corpus. The natural-key uniqueness + idempotent upsert pattern from
-- ENT-48/94/95 is preserved.
--
-- Idempotent: every statement uses `if exists` / `if not exists` so the
-- migration can be re-applied during local development without error.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. regulatory_articles ─────────────────────────────────────────────────────

alter table public.regulatory_articles
  drop column if exists body;

alter table public.regulatory_articles
  add column if not exists summary text;

-- Backfill any pre-existing rows (defensive — local DBs may have rows from
-- earlier ingests; remote starts clean since the migration runs before the
-- post-PR re-ingest). The placeholder is intentionally short and obviously
-- a placeholder, so any row that survives to query time is visible.
update public.regulatory_articles
  set summary = '__pending_summary__ — populated by post-migration ingest of data/corpus/*.json'
  where summary is null;

alter table public.regulatory_articles
  alter column summary set not null;

alter table public.regulatory_articles
  drop constraint if exists regulatory_articles_summary_length;
alter table public.regulatory_articles
  add constraint regulatory_articles_summary_length
    check (char_length(summary) between 1 and 2000);
-- Floor relaxed to 1 here so the placeholder backfill above doesn't violate
-- the CHECK. Curators are blocked at the Zod validator (100..2000) before
-- any row reaches the DB through the ingest path; the DB CHECK exists as
-- defence-in-depth, not as the primary length guarantee.

-- 2. regulatory_recitals ─────────────────────────────────────────────────────

alter table public.regulatory_recitals
  drop column if exists body;

alter table public.regulatory_recitals
  add column if not exists summary text;

update public.regulatory_recitals
  set summary = '__pending_summary__ — populated by post-migration ingest of data/corpus/*.json'
  where summary is null;

alter table public.regulatory_recitals
  alter column summary set not null;

alter table public.regulatory_recitals
  drop constraint if exists regulatory_recitals_summary_length;
alter table public.regulatory_recitals
  add constraint regulatory_recitals_summary_length
    check (char_length(summary) between 1 and 2000);

-- 3. regulatory_article_paragraphs ───────────────────────────────────────────

alter table public.regulatory_article_paragraphs
  drop column if exists body;

alter table public.regulatory_article_paragraphs
  add column if not exists summary text;

update public.regulatory_article_paragraphs
  set summary = '__pending_summary__ — populated by post-migration ingest of data/corpus/*.json'
  where summary is null;

alter table public.regulatory_article_paragraphs
  alter column summary set not null;

alter table public.regulatory_article_paragraphs
  drop constraint if exists regulatory_article_paragraphs_summary_length;
alter table public.regulatory_article_paragraphs
  add constraint regulatory_article_paragraphs_summary_length
    check (char_length(summary) between 1 and 2000);
