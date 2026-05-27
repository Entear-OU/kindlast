-- Regulatory annexes + per-article effective dates (ENT-96)
--
-- Final corpus-shape addition under the ENT-49 epic. Three changes:
--
--   1. `regulatory_annexes` — child of `regulatory_documents`. One row per
--      Annex (Annex I, II, III, …). Natural key `(document_id, annex_label)`
--      so re-ingest merges by label.
--   2. `regulatory_annex_items` — child of `regulatory_annexes`. One row per
--      item or sub-item inside an annex (e.g. Annex III categories 1..8 with
--      sub-points (a), (b), (c)). Natural key `(annex_id, item_label)`.
--   3. `regulatory_articles.effective_date` — nullable column. The EU AI Act
--      has a staged effective-date schedule (Article 4: Feb 2025, Annex III:
--      Aug 2026, most others: Aug 2026 or later). Null means "falls back to
--      the document's `version_date`" — the production query helper will
--      normalise. We model dates per-row rather than per-document because
--      the schedule is genuinely sparse and per-article.
--
-- Architecture note — progressive disclosure (ENT-32 update 2026-05-27):
--   These tables follow the catalog shape (no verbatim `body` columns) the
--   rest of the corpus is converging on. Each row carries a curated
--   `summary` (~100-2000 chars) used as the LLM's routing artifact. At
--   citation time the Analyst scans summaries in context, picks the
--   relevant `(annex_label, item_label)` natural key, and fetches verbatim
--   OJ text from `regulatory_documents.official_url` via a Tavily /
--   Firecrawl-backed websearch tool. No local mirror of normative prose.
--   See project memory `project-progressive-disclosure-corpus` for the
--   full rationale, and the parent epic ENT-32 for the audit trail.
--
-- Annex labels are TEXT (e.g. "III", not 3) because the OJ uses Roman
-- numerals — preserving them makes the citation "Annex III" trivial.
-- Item labels are TEXT for the same reason as paragraph labels
-- (ENT-95): real-world labels are "1", "1(a)", "1(a)(i)".
--
-- `regulatory_annex_items.heading` is nullable because in the OJ only the
-- top-level item (e.g. category 1 "Biometrics") has a heading; sub-items
-- (1(a), 1(b), …) only have summary text.
--
-- RLS follows the corpus convention (ENT-48): public read, no write
-- policies — only the service-role ingestion path writes.
--
-- The Annex III deadline (2 August 2026) used in `data/corpus/eu-ai-act.json`
-- assumes no Digital Omnibus deferral, per the PRD §3 instruction not to
-- treat a pending proposal as a current rule. If the Omnibus passes and
-- shifts dates, re-run `pnpm ingest:ai-act` with an updated snapshot —
-- the per-row `effective_date` column is in-place updatable.
--
-- Idempotent: `if not exists`, `drop … if exists` + `create`. Re-applies
-- cleanly during local development.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. regulatory_annexes ──────────────────────────────────────────────────────

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

create index if not exists regulatory_annexes_document_idx
  on public.regulatory_annexes (document_id, annex_label);

drop trigger if exists set_updated_at on public.regulatory_annexes;
create trigger set_updated_at
  before update on public.regulatory_annexes
  for each row execute function public.set_updated_at();

alter table public.regulatory_annexes enable row level security;

drop policy if exists "regulatory_annexes_select_public" on public.regulatory_annexes;
create policy "regulatory_annexes_select_public" on public.regulatory_annexes
  for select using (true);

-- 2. regulatory_annex_items ──────────────────────────────────────────────────

create table if not exists public.regulatory_annex_items (
  id             uuid        primary key default gen_random_uuid(),
  annex_id       uuid        not null
                   references public.regulatory_annexes(id) on delete cascade,
  item_label     text        not null,
  heading        text,
  summary        text        not null,
  effective_date date,
  ordering       int         not null,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  unique (annex_id, item_label),
  constraint regulatory_annex_items_summary_length
    check (char_length(summary) between 100 and 2000)
);

create index if not exists regulatory_annex_items_annex_idx
  on public.regulatory_annex_items (annex_id, ordering);

drop trigger if exists set_updated_at on public.regulatory_annex_items;
create trigger set_updated_at
  before update on public.regulatory_annex_items
  for each row execute function public.set_updated_at();

alter table public.regulatory_annex_items enable row level security;

drop policy if exists "regulatory_annex_items_select_public" on public.regulatory_annex_items;
create policy "regulatory_annex_items_select_public" on public.regulatory_annex_items
  for select using (true);

-- 3. regulatory_articles.effective_date ──────────────────────────────────────

alter table public.regulatory_articles
  add column if not exists effective_date date;
