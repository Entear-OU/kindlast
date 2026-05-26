-- Regulatory corpus (ENT-48)
--
-- Foundation tables for the primary-source regulatory knowledge base
-- (PRD §9). The MVP corpus is full-text GDPR and EU AI Act; this migration
-- defines the shape so any regulation that decomposes into Articles +
-- Recitals can be ingested with the same schema.
--
-- Design notes:
--
--   * `regulatory_documents.celex_number` is the natural key (EU CELEX is the
--     canonical EU-level identifier; one regulation = one CELEX). Idempotent
--     ingest leans on this uniqueness — the same document re-ingested becomes
--     an upsert, not a duplicate row.
--   * `regulatory_articles` / `regulatory_recitals` use a composite natural
--     key `(document_id, <number>)` for the same reason: re-running the
--     ingest script merges by article/recital number, never duplicates.
--   * `article_number` / `recital_number` are `int` not `text` because every
--     regulation in the MVP corpus (GDPR, AI Act) uses sequential integers.
--     If a future regulation introduces letter-suffixed articles (e.g. "6a"
--     from a later amendment), revisit then — premature flexibility here
--     would lose natural ordering for no current gain.
--   * `regulatory_article_recitals` is a many-to-many junction. The
--     acceptance criterion is "recitals … linkable from articles", not
--     "links populated"; the junction is what makes that linkage
--     expressible. Recital→article curation is a separate concern.
--
-- RLS model:
--
--   Corpus tables are NOT user-owned — they are reference data shared across
--   all tenants. The convention codified in the baseline migration
--   (`user_id` + per-user policies) does not apply. Instead:
--
--     * RLS is enabled so the default-deny stance remains for writes.
--     * A single permissive `for select using (true)` policy makes corpus
--       text readable to any role (anon + authenticated). The text is public
--       law; there is no confidentiality concern.
--     * No INSERT/UPDATE/DELETE policies. Only the service-role client
--       (which bypasses RLS by design) can write — that is the ingestion
--       script's role.
--
-- Idempotent: every statement uses `if not exists` or `drop … if exists` +
-- `create`, so the migration can be re-applied during local development
-- without error.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. regulatory_documents ────────────────────────────────────────────────────

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

drop trigger if exists set_updated_at on public.regulatory_documents;
create trigger set_updated_at
  before update on public.regulatory_documents
  for each row execute function public.set_updated_at();

alter table public.regulatory_documents enable row level security;

drop policy if exists "regulatory_documents_select_public" on public.regulatory_documents;
create policy "regulatory_documents_select_public" on public.regulatory_documents
  for select using (true);

-- 2. regulatory_articles ─────────────────────────────────────────────────────

create table if not exists public.regulatory_articles (
  id             uuid        primary key default gen_random_uuid(),
  document_id    uuid        not null
                   references public.regulatory_documents(id) on delete cascade,
  article_number int         not null,
  heading        text        not null,
  body           text        not null,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  unique (document_id, article_number)
);

create index if not exists regulatory_articles_document_idx
  on public.regulatory_articles (document_id, article_number);

drop trigger if exists set_updated_at on public.regulatory_articles;
create trigger set_updated_at
  before update on public.regulatory_articles
  for each row execute function public.set_updated_at();

alter table public.regulatory_articles enable row level security;

drop policy if exists "regulatory_articles_select_public" on public.regulatory_articles;
create policy "regulatory_articles_select_public" on public.regulatory_articles
  for select using (true);

-- 3. regulatory_recitals ─────────────────────────────────────────────────────

create table if not exists public.regulatory_recitals (
  id             uuid        primary key default gen_random_uuid(),
  document_id    uuid        not null
                   references public.regulatory_documents(id) on delete cascade,
  recital_number int         not null,
  body           text        not null,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  unique (document_id, recital_number)
);

create index if not exists regulatory_recitals_document_idx
  on public.regulatory_recitals (document_id, recital_number);

drop trigger if exists set_updated_at on public.regulatory_recitals;
create trigger set_updated_at
  before update on public.regulatory_recitals
  for each row execute function public.set_updated_at();

alter table public.regulatory_recitals enable row level security;

drop policy if exists "regulatory_recitals_select_public" on public.regulatory_recitals;
create policy "regulatory_recitals_select_public" on public.regulatory_recitals
  for select using (true);

-- 4. regulatory_article_recitals (junction) ──────────────────────────────────
--
-- Composite-PK junction. No surrogate id — the natural key (article, recital)
-- is the only identity that matters and a UUID would just waste space.

create table if not exists public.regulatory_article_recitals (
  article_id uuid not null
               references public.regulatory_articles(id) on delete cascade,
  recital_id uuid not null
               references public.regulatory_recitals(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (article_id, recital_id)
);

create index if not exists regulatory_article_recitals_recital_idx
  on public.regulatory_article_recitals (recital_id);

alter table public.regulatory_article_recitals enable row level security;

drop policy if exists "regulatory_article_recitals_select_public"
  on public.regulatory_article_recitals;
create policy "regulatory_article_recitals_select_public"
  on public.regulatory_article_recitals
  for select using (true);
