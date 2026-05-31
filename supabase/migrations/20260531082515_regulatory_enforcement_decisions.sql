-- Regulatory enforcement decisions (ENT-99)
--
-- The last unshipped content layer of the regulatory knowledge base (ENT-32,
-- PRD §9): landmark enforcement decisions from the major national DPAs
-- (CNIL, ICO, DPC, BfDI, AEPD) plus binding EDPB decisions. These turn the
-- letter of the regulation into citable precedent — "the CNIL fined Clearview
-- €20M for exactly this" — which makes the Analyst's findings persuasive.
--
-- Shape mirrors `regulatory_guidelines` (ENT-50): a thin catalog row under
-- progressive disclosure. The full decision text is NOT mirrored — the DPA
-- owns the canonical document and the `source_url` is the citable artifact;
-- the Analyst fetches verbatim text at citation time via the websearch tool
-- (ENT-98). See project memory `project-progressive-disclosure-corpus`.
--
-- Design notes:
--
--   * `slug` is the natural key. Format: `<dpa>-<year>-<case>` (e.g.
--     `cnil-2022-clearview-ai`, `dpc-2023-meta-transfers`). Stable across
--     re-ingests so curated edits survive snapshot updates.
--   * `dpa` is free text (not an enum) — the supervisory-authority landscape
--     is wide (27 national DPAs + EDPB + regional German authorities) and a
--     hard enum would force a migration every time the curated set widens.
--     The curated JSON is the gate on what values appear.
--   * `fine_eur` is a nullable bigint — not every decision is a monetary fine
--     (reprimands, processing bans, corrective orders). Stored in EUR cents?
--     No — whole euros: fines are reported in millions and sub-euro precision
--     is meaningless here. UK fines (£) are converted to an approximate EUR
--     figure at curation time; the exact original is in the summary.
--   * `gdpr_articles int[]` records which GDPR articles the decision turned on.
--     GIN-indexed so the Analyst can answer "how has Article 6 been enforced?"
--     with `gdpr_articles @> ARRAY[6]`. Empty array for non-GDPR decisions.
--   * `topic_tags text[]` mirrors the guidelines table — GIN-indexed for
--     `tags @> ARRAY['transfers']` retrieval.
--   * `summary` is the routing artifact (100..2000 chars, CHECK enforced),
--     same as every other progressive-disclosure table.
--
-- RLS mirrors the rest of the corpus (ENT-48): public read, no write
-- policies — only the service-role ingest path writes.
--
-- Idempotent: every statement uses `if not exists` or `drop … if exists`
-- + `create`, so the migration can be re-applied during local development
-- without error.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.regulatory_enforcement_decisions (
  id             uuid        primary key default gen_random_uuid(),
  slug           text        not null unique,
  dpa            text        not null,
  title          text        not null,
  decision_date  date        not null,
  fine_eur       bigint,
  summary        text        not null,
  source_url     text        not null,
  gdpr_articles  int[]       not null default '{}',
  topic_tags     text[]      not null default '{}',
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  constraint regulatory_enforcement_decisions_summary_length
    check (char_length(summary) between 100 and 2000)
);

create index if not exists regulatory_enforcement_decisions_topic_tags_idx
  on public.regulatory_enforcement_decisions using gin (topic_tags);

create index if not exists regulatory_enforcement_decisions_articles_idx
  on public.regulatory_enforcement_decisions using gin (gdpr_articles);

create index if not exists regulatory_enforcement_decisions_dpa_idx
  on public.regulatory_enforcement_decisions (dpa, decision_date desc);

drop trigger if exists set_updated_at on public.regulatory_enforcement_decisions;
create trigger set_updated_at
  before update on public.regulatory_enforcement_decisions
  for each row execute function public.set_updated_at();

alter table public.regulatory_enforcement_decisions enable row level security;

drop policy if exists "regulatory_enforcement_decisions_select_public"
  on public.regulatory_enforcement_decisions;
create policy "regulatory_enforcement_decisions_select_public"
  on public.regulatory_enforcement_decisions
  for select using (true);
