-- Compliance profiles (ENT-45)
--
-- One structured row per completed onboarding session. The conversational
-- intake (ENT-44) produces a free-text transcript; this table is the
-- deterministic projection that the Watcher / Analyst / Executor agents
-- reason over.
--
-- Field shapes are tuned to absorb how founders actually answer:
--
--   * Open-ended categorical fields (`data_categories`, `ai_systems`,
--     `eu_jurisdictions`, `data_subjects`, `transfer_destinations`) are
--     `text[]` because founders volunteer multiple values per turn and the
--     LLM should preserve their phrasing without forcing a closed enum.
--   * Yes/no/unsure trichotomy (`has_dpo`, `has_ropa`, `transfers_outside_eu`)
--     instead of `boolean` so the extraction prompt can faithfully encode
--     "I don't know" without misclassifying an evasive answer as "no".
--   * `vendor_list` and `industry` stay free text — founders use their own
--     vocabulary and we don't want a brittle enum here.
--   * `staff_count` is nullable: when the founder gives a range or skips,
--     leave it null rather than guessing.
--
-- `session_id` is unique: one profile per onboarding session. Re-interviews
-- (ENT-47 model — new session row) produce a new profile row, so historical
-- profiles stay queryable for audit replay.
--
-- RLS follows the convention codified in the baseline migration: `user_id`
-- references `auth.users(id)` and four per-operation policies scope to
-- `auth.uid() = user_id`.
--
-- Idempotent: `if not exists`, `drop policy/trigger if exists` + recreate.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.compliance_profiles (
  id                    uuid        primary key default gen_random_uuid(),
  session_id            uuid        not null unique
                          references public.onboarding_sessions(id) on delete cascade,
  user_id               uuid        not null
                          references auth.users(id) on delete cascade,
  industry              text        not null,
  eu_jurisdictions      text[]      not null default '{}',
  data_categories       text[]      not null default '{}',
  data_subjects         text[]      not null default '{}',
  ai_systems            text[]      not null default '{}',
  has_dpo               text        not null
                          check (has_dpo in ('yes', 'no', 'unsure')),
  has_ropa              text        not null
                          check (has_ropa in ('yes', 'no', 'unsure')),
  transfers_outside_eu  text        not null
                          check (transfers_outside_eu in ('yes', 'no', 'unsure')),
  transfer_destinations text[]      not null default '{}',
  vendor_list           text        not null default '',
  staff_count           int,
  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);

create index if not exists compliance_profiles_user_idx
  on public.compliance_profiles (user_id);

drop trigger if exists set_updated_at on public.compliance_profiles;
create trigger set_updated_at
  before update on public.compliance_profiles
  for each row execute function public.set_updated_at();

alter table public.compliance_profiles enable row level security;

drop policy if exists "compliance_profiles_select_own" on public.compliance_profiles;
create policy "compliance_profiles_select_own" on public.compliance_profiles
  for select using (auth.uid() = user_id);

drop policy if exists "compliance_profiles_insert_own" on public.compliance_profiles;
create policy "compliance_profiles_insert_own" on public.compliance_profiles
  for insert with check (auth.uid() = user_id);

drop policy if exists "compliance_profiles_update_own" on public.compliance_profiles;
create policy "compliance_profiles_update_own" on public.compliance_profiles
  for update using (auth.uid() = user_id)
             with check (auth.uid() = user_id);

drop policy if exists "compliance_profiles_delete_own" on public.compliance_profiles;
create policy "compliance_profiles_delete_own" on public.compliance_profiles
  for delete using (auth.uid() = user_id);
