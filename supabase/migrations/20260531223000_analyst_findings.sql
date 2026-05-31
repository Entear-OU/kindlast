-- The Analyst: signal → structured finding (ENT-58)
--
-- The Analyst is the second agent in the pipeline (epic ENT-34). The Watcher
-- (ENT-53/55/56/57) detects conditions and emits `watcher_findings` rows — the
-- *signals*. The Analyst reads each open signal and produces exactly one
-- `findings` row: the user-facing, actionable item the feed and Comms agent
-- consume. "The Analyst writes only to `findings`" (PRD §5.2) — it never
-- notifies and never touches compliance records.
--
-- Architecture (decided on epic ENT-34; consistent with the SQL-first Watcher):
--
--   * SQL-first. ENT-58 is deterministic structural plumbing — a 1:1 mapping
--     from a signal row to a finding row — so it lives in a migration and is
--     exercised end-to-end against the local stack, exactly like the detectors.
--   * Separate pass, not coupled into the Watcher loop. `run_analyst()` is its
--     own entry point over the shared signal store, on its own cron a few
--     minutes after the Watcher. This keeps the "Watcher triggers the Analyst"
--     agent boundary (PRD §5.1) clean: the Analyst is a pure consumer of open
--     signals and is correct whether it runs standalone or right after a sweep.
--
-- Scope boundary — ENT-58 builds the table + the deterministic conversion and
-- the traceability links, and populates every payload field with a baseline
-- derived from the signal + its obligation. The *quality* of those fields is
-- the rest of the epic and intentionally NOT done here:
--
--   * ENT-59 — precise obligation/article citation; supporting_context cites
--              `regulatory_chunks`; obligation_id tightened to NOT NULL +
--              delete-protected for catalogue-mapped findings.
--   * ENT-60 — plain-language `detected` + a specific, verb-led proposed_action.
--   * ENT-61 — severity adjustment (recency / proximity / sensitivity) and the
--              effort-estimate model.
--
-- Determinism (AC: "deterministic given the same signal + retrieval context,
-- for replay/testing"): the conversion is pure SQL over the signal row and its
-- obligation row, so the same inputs always yield the same finding. The 1:1
-- pivot is a unique index on `watcher_finding_id`; a replay upserts the live
-- finding in place rather than inserting a duplicate.
--
-- Idempotent: every statement uses `if not exists` / `or replace` /
-- `drop … if exists`, so the migration re-applies cleanly in local dev.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Findings store ───────────────────────────────────────────────────────────
--
-- User-owned (one finding belongs to one profile, hence one user) so the
-- dashboard/feed can read it under RLS, but only the Analyst writes — there is
-- no user-facing INSERT path. Status mutations from the feed (approve / reject /
-- snooze, PRD §7) get their own scoped UPDATE policy when that UI lands; the
-- same deferral the Watcher took for its findings.
--
--   * `watcher_finding_id` — the originating signal. NOT NULL + UNIQUE: every
--                            finding has a provenance, and the uniqueness is the
--                            1:1 pivot the conversion upserts on.
--   * `obligation_id`      — catalogue link for traceability. Nullable here:
--                            some signals anchor to a slug that isn't a
--                            catalogue row (e.g. the DSAR data-subject-rights
--                            anchor). ON DELETE SET NULL for now; ENT-59 tightens
--                            this to NOT NULL + delete-protection for the
--                            obligation-mapped findings.
--   * `obligation_slug`    — natural-key carryover from the signal, stable across
--                            corpus re-ingests even when obligation_id is null.
--   * `detected`           — plain-language "what was detected" (baseline: the
--                            signal title; ENT-60 regenerates).
--   * `severity`           — carried from the signal; includes 'critical' for
--                            escalated DSARs. ENT-61 adjusts.
--   * `proposed_action`    — the specific thing to do (baseline by kind; ENT-60
--                            regenerates). One action = one Executor write.
--   * `regulatory_obligation` — human-readable citation (baseline: obligation
--                            title, else the slug; ENT-59 makes it precise).
--   * `supporting_context` — knowledge-base backing (baseline: obligation
--                            summary; ENT-59 cites corpus chunks).
--   * `effort_estimate`    — minutes | hours | days (baseline; ENT-61 owns).
--   * `status`             — pending | approved | rejected | snoozed. New
--                            findings are 'pending'; the conversion never
--                            overwrites a user's decision on replay.
--   * `metadata`           — signal provenance (kind, dedup_key, signal
--                            metadata) for replay/audit.

create table if not exists public.findings (
  id                    uuid        primary key default gen_random_uuid(),
  profile_id            uuid        not null
                          references public.compliance_profiles(id) on delete cascade,
  user_id               uuid        not null
                          references auth.users(id) on delete cascade,
  watcher_finding_id    uuid        not null
                          references public.watcher_findings(id) on delete cascade,
  obligation_id         uuid        references public.obligations(id) on delete set null,
  obligation_slug       text,
  detected              text        not null,
  severity              text        not null default 'medium'
                          check (severity in ('low', 'medium', 'high', 'critical')),
  proposed_action       text        not null,
  regulatory_obligation text,
  supporting_context    text,
  effort_estimate       text        not null default 'hours'
                          check (effort_estimate in ('minutes', 'hours', 'days')),
  status                text        not null default 'pending'
                          check (status in ('pending', 'approved', 'rejected', 'snoozed')),
  metadata              jsonb       not null default '{}'::jsonb,
  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);

-- 1:1 pivot: at most one finding per signal. Doubles as the conflict target the
-- conversion upserts on, so a replay refreshes the live finding in place.
create unique index if not exists findings_watcher_finding_idx
  on public.findings (watcher_finding_id);

create index if not exists findings_user_idx
  on public.findings (user_id);

create index if not exists findings_profile_status_idx
  on public.findings (profile_id, status);

drop trigger if exists set_updated_at on public.findings;
create trigger set_updated_at
  before update on public.findings
  for each row execute function public.set_updated_at();

alter table public.findings enable row level security;

-- Read-only for the owning user (feed surfacing). Writes happen through
-- `analyst_convert_signal()` (SECURITY DEFINER) / the service role.
drop policy if exists "findings_select_own" on public.findings;
create policy "findings_select_own" on public.findings
  for select using (auth.uid() = user_id);

-- 2. analyst_convert_signal() ─────────────────────────────────────────────────
--
-- The single conversion. Reads one `watcher_findings` row, resolves its
-- obligation by natural key (may be absent), and upserts the 1:1 finding.
-- SECURITY DEFINER so it can write while the table stays RLS-locked to readers.
-- Deterministic: every field is a pure function of the signal + obligation row.
-- Returns the finding id.

create or replace function public.analyst_convert_signal(p_signal_id uuid)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_sig    public.watcher_findings;
  v_obl    public.obligations;
  v_action text;
  v_id     uuid;
begin
  select * into v_sig from public.watcher_findings where id = p_signal_id;
  if not found then
    raise exception 'analyst_convert_signal: unknown signal %', p_signal_id;
  end if;

  -- Resolve the obligation by natural key. A null row (no match, or no slug on
  -- the signal) leaves obligation_id null and the baselines fall back to the
  -- signal's own text — DSAR findings anchor to a slug that isn't a catalogue
  -- row, and that path must still produce a finding.
  if v_sig.obligation_slug is not null then
    select * into v_obl from public.obligations where slug = v_sig.obligation_slug;
  end if;

  -- Deterministic baseline action by signal kind. ENT-60 replaces this with a
  -- generated, specific, verb-led action; ENT-58 only guarantees the field is
  -- present, non-empty, and deterministic.
  v_action := case v_sig.kind
    when 'deadline'          then 'Review this obligation and prepare to meet its upcoming deadline.'
    when 'profile_gap'       then 'Put the missing control in place to satisfy this obligation.'
    when 'dsar'              then 'Prepare and log a response to this data-subject request before its deadline.'
    when 'regulatory_update' then 'Review this regulatory update and assess its impact on your obligations.'
    else                          'Review this finding and take the appropriate action.'
  end;

  insert into public.findings (
    profile_id, user_id, watcher_finding_id, obligation_id, obligation_slug,
    detected, severity, proposed_action, regulatory_obligation,
    supporting_context, effort_estimate, metadata
  )
  values (
    v_sig.profile_id,
    v_sig.user_id,
    v_sig.id,
    v_obl.id,                                        -- null when not in the catalogue
    v_sig.obligation_slug,
    v_sig.title,                                     -- detected (baseline)
    v_sig.severity,
    v_action,                                        -- proposed_action (baseline)
    coalesce(v_obl.title, v_sig.obligation_slug),    -- regulatory_obligation (baseline)
    coalesce(v_obl.summary, v_sig.detail),           -- supporting_context (baseline)
    'hours',                                         -- effort_estimate (baseline; ENT-61)
    jsonb_build_object(
      'signal_kind',     v_sig.kind,
      'signal_dedup_key', v_sig.dedup_key,
      'signal_metadata', v_sig.metadata
    )
  )
  on conflict (watcher_finding_id) do update set
    -- Refresh the derived payload from the (possibly updated) signal. `status`
    -- is deliberately NOT reset — a replay must not undo a user's decision.
    obligation_id         = excluded.obligation_id,
    obligation_slug       = excluded.obligation_slug,
    detected              = excluded.detected,
    severity              = excluded.severity,
    proposed_action       = excluded.proposed_action,
    regulatory_obligation = excluded.regulatory_obligation,
    supporting_context    = excluded.supporting_context,
    metadata              = excluded.metadata,
    updated_at            = now()
  returning id into v_id;

  return v_id;
end;
$$;

-- 3. run_analyst_for_profile() ────────────────────────────────────────────────
--
-- One profile's worth of conversion: every OPEN signal becomes a finding.
-- Resolved/dismissed signals are not converted (a closed condition is not an
-- actionable item). Returns the number of signals converted.

create or replace function public.run_analyst_for_profile(p_profile_id uuid)
returns integer
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  s record;
  n integer := 0;
begin
  for s in
    select id from public.watcher_findings
    where profile_id = p_profile_id
      and status = 'open'
    order by created_at, id
  loop
    perform public.analyst_convert_signal(s.id);
    n := n + 1;
  end loop;

  return n;
end;
$$;

-- 4. run_analyst() ────────────────────────────────────────────────────────────
--
-- The daily entry point cron invokes, mirroring `run_watcher()`'s "one
-- invocation per active profile" (the most recent profile per user; a
-- re-interview inserts a new profile, superseding the old one for runs while
-- keeping it queryable for audit). Returns the count of profiles processed.

create or replace function public.run_analyst()
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
    perform public.run_analyst_for_profile(r.id);
    n := n + 1;
  end loop;

  return n;
end;
$$;

-- 5. Daily schedule ───────────────────────────────────────────────────────────
--
-- A separate job from 'watcher-daily' (06:00), running at 06:05 so the day's
-- signals exist before the Analyst sweeps them. `cron.schedule(jobname, ...)`
-- upserts by name, so re-applying this migration updates the job rather than
-- stacking duplicates. The Analyst is idempotent, so an out-of-order or
-- standalone run is harmless — the schedule only optimises latency-to-finding.

create extension if not exists pg_cron;

select cron.schedule('analyst-daily', '5 6 * * *', $$select public.run_analyst();$$);
