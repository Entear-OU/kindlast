-- Feed actions: reject + snooze a finding, with snooze re-emergence (ENT-63)
--
-- The Agent feed (ENT-62) lists findings; ENT-63 makes each one actionable. The
-- approve path already exists (`approve_finding`, ENT-66) — it fires the Executor.
-- This migration adds the other two founder decisions, plus the mechanism that
-- brings a snoozed finding back when its timer runs out.
--
-- Architecture — SQL-first, consistent with approve_finding (ENT-66) and the
-- DSAR/ROPA write RPCs (ENT-71/70). Reject and snooze are plain status changes
-- the founder makes; they create no compliance record, so — unlike an Executor
-- write — they do not touch the audit log (ENT-69 audits record-creating actions).
-- Both go through SECURITY DEFINER functions whose actor is auth.uid() (never a
-- trusted parameter) and whose WHERE clause is scoped to the caller's own rows, so
-- the feed UI is just a caller and a client cannot mutate another user's finding.
-- The findings table keeps no UPDATE policy — every write is one of these RPCs.
--
--   * rejection_reason / snoozed_until — two new columns. rejection_reason is the
--     optional note the founder leaves on reject (persisted now; the Analyst
--     feedback loop that consumes it is ENT-65). snoozed_until is when a snoozed
--     finding is due to re-emerge.
--   * reject_finding   — status → rejected, persist the (trimmed, optional) reason.
--   * snooze_finding   — status → snoozed, snoozed_until = now + N days (default 7).
--   * expire_snoozed_findings — the system sweep: any snooze whose timer has passed
--     goes back to pending. Directly callable (for tests / a manual run) and the
--     body the daily cron runs, mirroring run_watcher()/run_analyst().
--
-- Idempotent: `add column if not exists`, `create or replace`, and a named cron
-- job that is unscheduled-then-scheduled so re-running the migration is safe.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Columns ────────────────────────────────────────────────────────────────────
alter table public.findings
  add column if not exists rejection_reason text;
alter table public.findings
  add column if not exists snoozed_until timestamptz;

-- A snooze that hasn't re-emerged yet, indexed so the sweep is a cheap scan.
create index if not exists findings_snoozed_until_idx
  on public.findings (snoozed_until)
  where status = 'snoozed';

-- 2. reject_finding ──────────────────────────────────────────────────────────────
--
-- Sets the finding to rejected and persists the optional reason (blank → null).
-- Returns whether a row changed: false when the finding is unknown, not owned, or
-- already rejected — letting the caller distinguish a no-op from success.
create or replace function public.reject_finding(
  p_finding_id uuid,
  p_reason     text default null
)
returns boolean
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user    uuid := auth.uid();
  v_updated uuid;
begin
  if v_user is null then
    raise exception 'reject_finding: not authenticated';
  end if;

  update public.findings
    set status = 'rejected',
        rejection_reason = nullif(btrim(p_reason), ''),
        snoozed_until = null
  where id = p_finding_id
    and user_id = v_user
    and status <> 'rejected'
  returning id into v_updated;

  return v_updated is not null;
end;
$$;

-- 3. snooze_finding ──────────────────────────────────────────────────────────────
--
-- Sets the finding to snoozed for p_days (default 7; the AC's configurable
-- duration). Returns the new snoozed_until, or null when the finding is unknown or
-- not owned. The day count is bounded to keep a fat-fingered value sane.
create or replace function public.snooze_finding(
  p_finding_id uuid,
  p_days       integer default 7
)
returns timestamptz
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user  uuid := auth.uid();
  v_days  integer := greatest(1, least(coalesce(p_days, 7), 365));
  v_until timestamptz;
begin
  if v_user is null then
    raise exception 'snooze_finding: not authenticated';
  end if;

  update public.findings
    set status = 'snoozed',
        snoozed_until = now() + make_interval(days => v_days)
  where id = p_finding_id
    and user_id = v_user
  returning snoozed_until into v_until;

  return v_until;  -- null when nothing matched (unknown / not owned)
end;
$$;

-- 4. expire_snoozed_findings ─────────────────────────────────────────────────────
--
-- The re-emergence sweep: every snooze whose timer has passed returns to pending
-- and forgets its timer, so it surfaces in the feed again. A whole-table operation
-- (no auth.uid()): SECURITY DEFINER so the daily cron, running as the table owner,
-- can sweep across all profiles. Returns the number of findings re-emerged.
create or replace function public.expire_snoozed_findings()
returns integer
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_count integer;
begin
  with reemerged as (
    update public.findings
      set status = 'pending',
          snoozed_until = null
    where status = 'snoozed'
      and snoozed_until is not null
      and snoozed_until <= now()
    returning 1
  )
  select count(*) into v_count from reemerged;

  return v_count;
end;
$$;

-- 5. Daily schedule ──────────────────────────────────────────────────────────────
--
-- pg_cron is already preloaded (Watcher/Analyst schedules). Run a little after the
-- Analyst's 06:05 sweep so a finding due today re-emerges before the founder's day.
-- ≤24h latency on a multi-day snooze is immaterial. Idempotent: unschedule the
-- named job if present, then (re)create it.
create extension if not exists pg_cron;

select cron.unschedule('snooze-expiry-daily')
where exists (select 1 from cron.job where jobname = 'snooze-expiry-daily');

select cron.schedule('snooze-expiry-daily', '10 6 * * *', $$select public.expire_snoozed_findings();$$);
