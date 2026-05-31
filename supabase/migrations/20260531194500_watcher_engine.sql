-- The Watcher engine (ENT-53)
--
-- The Watcher is the product's first differentiator: "it runs without being
-- asked" (PRD §1). This migration is the *harness* — the daily schedule, the
-- shared findings store, and the idempotency model. The individual detectors
-- (deadlines → ENT-55, profile gaps → ENT-56, DSAR deadlines → ENT-57) are
-- added in their own sub-issues; each one calls `emit_watcher_finding()` and
-- gets registered inside `run_watcher_for_profile()`.
--
-- Architecture (decided on epic ENT-33; see also the obligations catalogue
-- ENT-52, which is the reference data the detectors read):
--
--   * SQL-first. The remaining detectors are deterministic DB logic, so the
--     whole engine lives in migrations and is exercised end-to-end against
--     the local stack. The one LLM/semantic detector (ENT-54) is deferred
--     until pgvector embeddings (ENT-51) land — it will attach as a separate
--     async path, not through this synchronous loop.
--   * pg_cron fires `run_watcher()` once a day. pg_cron is already preloaded
--     on the Supabase image (`shared_preload_libraries`), so no infra change.
--
-- Idempotency (AC: "replays don't create duplicate findings") is enforced at
-- the data layer, not by run bookkeeping: a partial unique index on
-- (profile_id, dedup_key) WHERE status = 'open' means a detector can re-emit
-- the same signal every single day and the open finding is refreshed in
-- place. Resolving a finding frees the key so a genuinely new occurrence can
-- open a fresh one later.
--
-- Idempotent: every statement uses `if not exists` / `or replace` /
-- `drop … if exists`, so the migration re-applies cleanly in local dev.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Per-profile last-run timestamp ──────────────────────────────────────────
--
-- AC: "Last-run timestamp recorded per profile (surfaces on dashboard)." A
-- column on the profile is the minimal home for it; a separate run-history
-- table is a follow-up if the dashboard ever needs run-by-run audit.

alter table public.compliance_profiles
  add column if not exists watcher_last_run_at timestamptz;

-- 2. Findings store ───────────────────────────────────────────────────────────
--
-- Shared across every detector. User-owned (one finding belongs to one
-- profile, hence one user) so the dashboard can read it under RLS, but only
-- the Watcher writes — there is no user-facing INSERT path.
--
--   * `kind`           — which detector produced it. 'regulatory_update' is
--                        listed now for the deferred ENT-54 path so its
--                        findings don't need a later check-constraint change.
--   * `obligation_slug`— natural-key reference into `obligations.slug`
--                        (stable across corpus re-ingests). Plain text, not a
--                        FK: DSAR findings reference a DSAR, not an obligation,
--                        so the column is nullable and unconstrained.
--   * `severity`       — includes 'critical' for ENT-57's <10-day DSAR escalation.
--   * `dedup_key`      — detector-computed stable identity of the *signal*
--                        (e.g. 'deadline:gdpr-art-30-ropa', 'dsar:<id>').
--                        Idempotency pivots on this; see the partial index.
--   * `status`         — open | resolved | dismissed. Suppression is scoped
--                        to 'open' so a resolved finding can recur later.

create table if not exists public.watcher_findings (
  id              uuid        primary key default gen_random_uuid(),
  profile_id      uuid        not null
                    references public.compliance_profiles(id) on delete cascade,
  user_id         uuid        not null
                    references auth.users(id) on delete cascade,
  kind            text        not null
                    check (kind in ('deadline', 'profile_gap', 'dsar', 'regulatory_update')),
  obligation_slug text,
  severity        text        not null default 'medium'
                    check (severity in ('low', 'medium', 'high', 'critical')),
  title           text        not null,
  detail          text,
  status          text        not null default 'open'
                    check (status in ('open', 'resolved', 'dismissed')),
  dedup_key       text        not null,
  metadata        jsonb       not null default '{}'::jsonb,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now(),
  resolved_at     timestamptz
);

-- Idempotency pivot: at most one OPEN finding per (profile, dedup_key).
-- A partial unique index leaves resolved/dismissed rows free to accumulate as
-- history while suppressing duplicate live findings.
create unique index if not exists watcher_findings_open_dedup_idx
  on public.watcher_findings (profile_id, dedup_key)
  where status = 'open';

create index if not exists watcher_findings_user_idx
  on public.watcher_findings (user_id);

create index if not exists watcher_findings_profile_status_idx
  on public.watcher_findings (profile_id, status);

drop trigger if exists set_updated_at on public.watcher_findings;
create trigger set_updated_at
  before update on public.watcher_findings
  for each row execute function public.set_updated_at();

alter table public.watcher_findings enable row level security;

-- Read-only for the owning user (dashboard surfacing). Writes happen through
-- `emit_watcher_finding()` (SECURITY DEFINER) / the service role, so no
-- user-facing INSERT/UPDATE/DELETE policy exists. Status mutations (dismiss /
-- resolve from the UI) get their own scoped policy when that UI lands.
drop policy if exists "watcher_findings_select_own" on public.watcher_findings;
create policy "watcher_findings_select_own" on public.watcher_findings
  for select using (auth.uid() = user_id);

-- 3. emit_watcher_finding() ───────────────────────────────────────────────────
--
-- The single write path detectors call. SECURITY DEFINER so detector logic
-- (and the cron job, which runs as the table owner) can write findings while
-- the table stays RLS-locked to end users. Derives user_id from the profile
-- so callers can't mis-scope a finding. Returns the finding id.
--
-- Idempotent on (profile_id, dedup_key) while open: a same-day replay updates
-- the live finding's mutable fields in place instead of inserting a duplicate.

create or replace function public.emit_watcher_finding(
  p_profile_id      uuid,
  p_kind            text,
  p_dedup_key       text,
  p_title           text,
  p_detail          text  default null,
  p_severity        text  default 'medium',
  p_obligation_slug text  default null,
  p_metadata        jsonb default '{}'::jsonb
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user_id uuid;
  v_id      uuid;
begin
  select user_id into v_user_id
  from public.compliance_profiles
  where id = p_profile_id;

  if v_user_id is null then
    raise exception 'emit_watcher_finding: unknown profile %', p_profile_id;
  end if;

  insert into public.watcher_findings (
    profile_id, user_id, kind, obligation_slug, severity, title, detail, dedup_key, metadata
  )
  values (
    p_profile_id, v_user_id, p_kind, p_obligation_slug, p_severity,
    p_title, p_detail, p_dedup_key, p_metadata
  )
  on conflict (profile_id, dedup_key) where status = 'open'
  do update set
    kind            = excluded.kind,
    obligation_slug = excluded.obligation_slug,
    severity        = excluded.severity,
    title           = excluded.title,
    detail          = excluded.detail,
    metadata        = excluded.metadata,
    updated_at      = now()
  returning id into v_id;

  return v_id;
end;
$$;

-- 4. run_watcher_for_profile() ────────────────────────────────────────────────
--
-- One profile's worth of work. Today it only stamps the last-run timestamp;
-- detectors register their `emit_watcher_finding()` calls here as their
-- sub-issues land (ENT-55/56/57). Keeping this the single per-profile entry
-- point means the daily loop and any ad-hoc/manual trigger share one code path.

create or replace function public.run_watcher_for_profile(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
begin
  -- ── Detectors register here (ENT-55 deadlines, ENT-56 profile gaps,
  --    ENT-57 DSAR deadlines). Each calls public.emit_watcher_finding(...). ──

  update public.compliance_profiles
  set watcher_last_run_at = now()
  where id = p_profile_id;
end;
$$;

-- 5. run_watcher() ────────────────────────────────────────────────────────────
--
-- The daily entry point cron invokes. "One invocation per active profile"
-- (AC): a re-interview (ENT-47) inserts a *new* compliance_profiles row for
-- the same user, so "active" = the most recent profile per user. Superseded
-- profiles stay queryable for audit but are not run. Returns the count of
-- profiles processed (handy for logging / the cron run record).

create or replace function public.run_watcher()
returns integer
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  r record;
  n integer := 0;
begin
  for r in
    select distinct on (user_id) id
    from public.compliance_profiles
    order by user_id, created_at desc
  loop
    perform public.run_watcher_for_profile(r.id);
    n := n + 1;
  end loop;

  return n;
end;
$$;

-- 6. Daily schedule ───────────────────────────────────────────────────────────
--
-- pg_cron is preloaded on the Supabase image. `cron.schedule(jobname, ...)`
-- upserts by name, so re-applying this migration updates the existing job
-- rather than stacking duplicates. 06:00 UTC keeps the run off the busy
-- midnight cron slot while still surfacing findings within 24h of a trigger.

create extension if not exists pg_cron;

select cron.schedule('watcher-daily', '0 6 * * *', $$select public.run_watcher();$$);
