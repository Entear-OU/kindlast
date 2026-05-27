-- Structured obligations catalogue (ENT-52)
--
-- Foundation table for the Watcher agent: a structured catalogue of the
-- normative obligations Kindlast tracks for SMEs. The Watcher cannot
-- re-derive obligations from corpus prose every run — that would be both
-- expensive and non-deterministic — so this table caches the structural
-- mapping from "what triggers an obligation" to "which natural-key
-- citation backs it".
--
-- Architecture (decided on the parent epic ENT-32 comment thread):
--
--   * Catalogue rows reference the corpus by NATURAL KEY, not by surrogate
--     UUID. The Analyst resolves a citation (CELEX + article number, or
--     CELEX + annex label + item label, etc.) to a URL via the corpus
--     metadata at query time, then fetches the verbatim normative text
--     using the websearch tool from ENT-98. Natural-key references stay
--     stable across corpus re-ingests (which DO churn UUIDs).
--   * No mirrored body text. Each row carries a curated `summary`
--     (100..2000 chars) — the same progressive disclosure pattern adopted
--     across `regulatory_articles`, `regulatory_recitals`, etc. under
--     ENT-97. The summary is the LLM routing artifact; the verbatim text
--     is fetched runtime.
--   * Watcher trigger metadata (e.g. `due_within_days`, `recurrence`,
--     `effective_date`) lives on the obligation row itself, NOT on the
--     corpus row. The corpus describes the law; this table describes the
--     operational policy we derive from it.
--
-- Schema notes:
--
--   * `slug` is the human-readable natural key (e.g. "gdpr-art-30-ropa").
--     Stable across re-seeds, used by the Watcher to identify an obligation
--     when emitting alerts.
--   * `citation_kind` is an enum-style discriminator over the corpus shape:
--     - 'article'  → citation_article is required
--     - 'recital'  → citation_recital is required
--     - 'annex'    → citation_annex is required (item_label via citation_paragraph)
--     A composite CHECK enforces "the right column is non-null for the
--     declared kind". This keeps a single flat table without losing type
--     safety — a per-kind subtype table would be over-engineered for the
--     ~10-20 row scale here.
--   * `citation_paragraph` is TEXT to carry the same mixed-format labels
--     `regulatory_article_paragraphs.paragraph_label` and
--     `regulatory_annex_items.item_label` use (e.g. "1", "1(a)", "1(a)(i)").
--     Nullable — the article/annex itself is often the citation grain.
--   * `applies_when` is JSONB. The Watcher evaluates this against an
--     SME's compliance profile to decide whether an obligation is active.
--     Documented shape (see also the seed snapshot):
--       {
--         "role":               "controller" | "processor" | "deployer" | "provider",
--         "processing_includes": "special_categories" | "automated_decisions" | ...,
--         "thresholds": {
--           "employees_min":    250,        // some obligations only apply over thresholds
--           "high_risk":        true,
--           "cross_border":     true
--         }
--       }
--     All keys optional; an empty `{}` means "applies to every SME the
--     product targets". The shape is intentionally loose at the DB level —
--     downstream evaluators own the schema. Migrating to JSON Schema or a
--     Zod-typed column is a follow-up if the Watcher's rule set grows.
--   * `due_within_days` is INT and nullable. 0 means "immediate / on-event"
--     (e.g. Article 33 breach notification — 72h ≈ "due immediately on
--     becoming aware"). NULL means "no scheduled deadline" (e.g. ROPA is
--     maintained continuously, not on a deadline).
--   * `recurrence` is TEXT (annual / ad-hoc / on-event / continuous) and
--     nullable. Free-form rather than enum because the Watcher's
--     interpretation of these is still settling — premature enumeration
--     would force schema migrations for each new pattern.
--   * `effective_date` is nullable. Most GDPR obligations inherit
--     2018-05-25, EU AI Act obligations have per-article staged dates
--     (Article 4 → 2025-02-02, Annex III → 2026-08-02). NULL falls back
--     to the regulation's `version_date` at query time. Snapshotting the
--     date on the obligation row keeps the Watcher independent of corpus
--     re-ingests changing the article-level effective_date.
--   * `severity` is TEXT (low / medium / high) — used by Watcher alert
--     priority, not by trigger logic itself. Same rationale as
--     `recurrence`: free-form for now, enumerable later when the alert
--     prioritisation engine lands.
--   * `topic_tags` mirrors the pattern from `regulatory_guidelines` —
--     `text[]` + GIN index for cheap containment lookups
--     (e.g. obligations involving consent, breach, transfers).
--
-- RLS:
--
--   Obligations are reference data, not user-owned (same as the corpus
--   tables in ENT-48 / ENT-50 / ENT-96). Public SELECT, no write
--   policies — only the service-role seed script writes.
--
-- Idempotent: every statement uses `if not exists` or `drop … if exists`
-- + `create`, so the migration can be re-applied during local
-- development without error.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.obligations (
  id                  uuid        primary key default gen_random_uuid(),
  slug                text        not null unique,
  title               text        not null,
  summary             text        not null,
  citation_celex      text        not null,
  citation_kind       text        not null
                        check (citation_kind in ('article', 'recital', 'annex')),
  citation_article    int,
  citation_recital    int,
  citation_annex      text,
  citation_paragraph  text,
  applies_when        jsonb       not null default '{}'::jsonb,
  severity            text        not null default 'medium'
                        check (severity in ('low', 'medium', 'high')),
  due_within_days     int,
  recurrence          text,
  effective_date      date,
  topic_tags          text[]      not null default '{}',
  created_at          timestamptz not null default now(),
  updated_at          timestamptz not null default now(),
  constraint obligations_summary_length
    check (char_length(summary) between 100 and 2000),
  constraint obligations_citation_matches_kind
    check (
      (citation_kind = 'article' and citation_article is not null
          and citation_recital is null and citation_annex is null)
      or
      (citation_kind = 'recital' and citation_recital is not null
          and citation_article is null and citation_annex is null)
      or
      (citation_kind = 'annex' and citation_annex is not null
          and citation_article is null and citation_recital is null)
    ),
  constraint obligations_due_within_days_nonneg
    check (due_within_days is null or due_within_days >= 0)
);

create index if not exists obligations_topic_tags_idx
  on public.obligations using gin (topic_tags);

create index if not exists obligations_celex_idx
  on public.obligations (citation_celex, citation_kind);

create index if not exists obligations_applies_when_idx
  on public.obligations using gin (applies_when);

drop trigger if exists set_updated_at on public.obligations;
create trigger set_updated_at
  before update on public.obligations
  for each row execute function public.set_updated_at();

alter table public.obligations enable row level security;

drop policy if exists "obligations_select_public" on public.obligations;
create policy "obligations_select_public" on public.obligations
  for select using (true);
