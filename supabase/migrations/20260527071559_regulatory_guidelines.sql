-- Regulatory guidelines (ENT-50)
--
-- Secondary regulatory sources — EDPB guidelines and WP29 guidelines endorsed
-- by the EDPB. Lives alongside the primary-source corpus from ENT-48 /
-- ENT-94 / ENT-95 but is shaped differently because guidelines are
-- standalone prose documents, not article+recital regulations.
--
-- Design notes:
--
--   * The acceptance criterion (PRD §9, ENT-50) calls for "title, publication
--     date, source URL, full text" per guideline. The full prose is NOT
--     mirrored into this table — EDPB owns the canonical text and guidelines
--     get revised (v1.0 → v2.0), so mirroring would create a sync trap. The
--     `source_url` is the citable artifact; if on-demand text fetch is
--     needed later, this table grows a nullable `full_text` column then.
--   * `slug` is the natural key. Format: `<publisher>-<series>-<year>-<topic>`
--     (e.g. `edpb-05-2020-consent`, `wp29-wp243-dpo`). Stable across re-ingests
--     so curated topic-tag edits survive snapshot updates.
--   * `publisher` defaults to `EDPB` (the curated set is primarily EDPB
--     guidelines). WP29 entries — endorsed by the EDPB on its first
--     plenary in May 2018 — explicitly set `publisher = 'WP29'` so the
--     legacy origin is preserved.
--   * `topic_tags` is `text[]` rather than a junction table. The corpus
--     here is small (~20 rows for MVP, low-double-digits at most for the
--     foreseeable future); a tags table would be over-engineered. GIN-index
--     the column so `tags @> ARRAY['consent']` lookups stay cheap if
--     retrieval surfaces multiply.
--   * `version` is nullable — most guidelines have a single published
--     version; a handful (e.g. EDPB 3/2018 territorial scope v2.0) ship
--     revisions. When present, store as the publisher's own version string
--     ("1.0", "2.0", "rev.01") rather than normalising — citations
--     reference the publisher's notation, not ours.
--
-- RLS mirrors the rest of the corpus (ENT-48):
--   * RLS enabled, default-deny for writes.
--   * Public-select policy — guideline metadata is publicly published.
--   * No INSERT/UPDATE/DELETE policies — only the service-role ingest
--     script writes (bypasses RLS by design).
--
-- Idempotent: every statement uses `if not exists` or `drop … if exists`
-- + `create`, so the migration can be re-applied during local development
-- without error.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.regulatory_guidelines (
  id            uuid        primary key default gen_random_uuid(),
  slug          text        not null unique,
  publisher     text        not null default 'EDPB',
  title         text        not null,
  adopted_date  date        not null,
  version       text,
  source_url    text        not null,
  topic_tags    text[]      not null default '{}',
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

create index if not exists regulatory_guidelines_topic_tags_idx
  on public.regulatory_guidelines using gin (topic_tags);

create index if not exists regulatory_guidelines_publisher_idx
  on public.regulatory_guidelines (publisher, adopted_date desc);

drop trigger if exists set_updated_at on public.regulatory_guidelines;
create trigger set_updated_at
  before update on public.regulatory_guidelines
  for each row execute function public.set_updated_at();

alter table public.regulatory_guidelines enable row level security;

drop policy if exists "regulatory_guidelines_select_public" on public.regulatory_guidelines;
create policy "regulatory_guidelines_select_public" on public.regulatory_guidelines
  for select using (true);
