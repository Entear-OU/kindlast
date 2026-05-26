-- Regulatory article paragraphs (ENT-95)
--
-- Sub-paragraph granularity for the MVP-critical EU AI Act articles
-- (Art 4, 6, 9–17, 26, 50). The Analyst needs to cite "Article 6(1)(a)"
-- specifically, not dump the whole 2,000-word article body. This table
-- is the addressable-citation layer beneath `regulatory_articles`.
--
-- Shape mirrors `regulatory_articles`:
--
--   * `(article_id, paragraph_label)` is the natural key. Re-ingest with
--     the same source text merges by label, never duplicates.
--   * `paragraph_label` is TEXT (not int) because real-world labels are
--     "1", "1(a)", "1(a)(i)", "3 second subparagraph" — letters, parens,
--     phrases. An int column would lose this directly.
--   * `ordering` is the stable display index — independent of label format
--     so a reader can render rows in source order without parsing the label.
--   * Body is required (the row IS a sub-paragraph; an empty body is a
--     defect in the source data, not a valid row).
--
-- RLS follows the corpus convention from ENT-48: public read, no write
-- policies — only the service-role ingestion path writes.
--
-- Idempotent: every statement uses `if not exists` or `drop … if exists`
-- + `create`, so the migration can be re-applied during local development.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.regulatory_article_paragraphs (
  id              uuid        primary key default gen_random_uuid(),
  article_id      uuid        not null
                    references public.regulatory_articles(id) on delete cascade,
  paragraph_label text        not null,
  body            text        not null,
  ordering        int         not null,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now(),
  unique (article_id, paragraph_label)
);

create index if not exists regulatory_article_paragraphs_article_idx
  on public.regulatory_article_paragraphs (article_id, ordering);

drop trigger if exists set_updated_at on public.regulatory_article_paragraphs;
create trigger set_updated_at
  before update on public.regulatory_article_paragraphs
  for each row execute function public.set_updated_at();

alter table public.regulatory_article_paragraphs enable row level security;

drop policy if exists "regulatory_article_paragraphs_select_public"
  on public.regulatory_article_paragraphs;
create policy "regulatory_article_paragraphs_select_public"
  on public.regulatory_article_paragraphs
  for select using (true);
