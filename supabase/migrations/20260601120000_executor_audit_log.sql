-- The Executor: immutable audit log (ENT-69)
--
-- The Executor is the only agent that writes to compliance records, and it acts
-- only on explicit human approval (epic ENT-35; PRD §5.4, §7.4, §10). Every one
-- of those writes leaves a trace here. The audit log is the product's compliance
-- evidence — what a founder shows a supervisory authority to demonstrate
-- defensible human-in-the-loop control — so its single defining property is that
-- it is *append-only*: entries are written once and never silently change.
--
-- Scope (ENT-69): the evidence store itself — its schema, its immutability
-- guarantees, the recent-actions index, and the canonical writer the Executor
-- sub-issues call. The actions that produce entries land in their own issues:
--
--   * ENT-66 — create a ROPA (processing_activities) row on approval.
--   * ENT-67 — create a DSAR (dsars) tracking row on approval.
--   * ENT-68 — create an AI Systems Register (ai_systems) row on approval.
--
-- Each of those calls `record_audit_log()` once per approved write. This
-- migration deliberately does NOT constrain action_type / target_table to an
-- enum: the concrete vocabulary is owned by those sub-issues, and pinning it
-- here would couple this migration to records that don't exist yet.
--
-- Immutability model (two independent layers, defence in depth):
--
--   1. RLS — the owner (authenticated) role gets SELECT + INSERT on its own
--      rows and nothing else. With no UPDATE or DELETE policy, RLS denies both
--      by default, so a founder can append evidence and read it back but has no
--      path to alter or erase it. This is the literal AC: "INSERT-only by RLS —
--      no UPDATE / DELETE for the owner role."
--   2. A BEFORE UPDATE trigger that rejects *every* update, regardless of role.
--      The service role and SECURITY DEFINER functions bypass RLS, so RLS alone
--      cannot stop a backend mutation — the trigger does. The result: the only
--      privileged mutation left to the service role is DELETE, used for
--      retention/cleanup of whole rows. It can prune old evidence but can never
--      silently rewrite an existing entry. (AC: "Service role can write
--      retention/cleanup operations, but never silently mutate existing rows.")
--
-- There is intentionally no `updated_at` column or `set_updated_at` trigger: an
-- audit entry has no lifecycle to track. `occurred_at` is the only timestamp,
-- and it is set once at insert.
--
-- Idempotent: every statement uses `if not exists` / `or replace` /
-- `drop … if exists`, so the migration re-applies cleanly in local dev.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Evidence store ───────────────────────────────────────────────────────────
--
-- User-owned (RLS convention), one row per Executor action.
--
--   * `finding_id`        — the approved finding that triggered the write. A
--                           soft reference (plain uuid, no FK) on purpose: the
--                           evidence must outlive the operational finding, which
--                           may later be resolved or purged. An FK with ON DELETE
--                           SET NULL would also fight the immutability trigger
--                           (the cascade is an UPDATE). Nullable for the rare
--                           action with no originating finding.
--   * `action_type`       — what the Executor did (e.g. 'create_ropa',
--                           'mark_dsar_responded'). Free text; vocabulary owned
--                           by ENT-66/67/68. Non-empty.
--   * `target_table`      — the compliance record table written
--                           (e.g. 'processing_activities'). Non-empty.
--   * `target_id`         — the affected row's id. Nullable: known only after an
--                           insert, and some actions have no single target.
--   * `before` / `after`  — JSON snapshots framing the change. `before` is null
--                           for a create; `after` is null for a delete.
--   * `approving_user_id` — the human who approved the action (human-in-the-loop
--                           evidence). In the MVP this is the owner; modelled
--                           separately so a future team/seat can attribute it.
--   * `occurred_at`       — when the action happened. Set once; never updated.

create table if not exists public.audit_log (
  id                uuid        primary key default gen_random_uuid(),
  user_id           uuid        not null references auth.users(id) on delete cascade,
  finding_id        uuid,
  action_type       text        not null check (length(btrim(action_type)) > 0),
  target_table      text        not null check (length(btrim(target_table)) > 0),
  target_id         uuid,
  before            jsonb,
  after             jsonb,
  approving_user_id uuid        not null references auth.users(id) on delete cascade,
  occurred_at       timestamptz not null default now()
);

-- Dashboard "recent actions": most recent entries for one user. Leading on
-- user_id (the RLS/equality predicate) and ordered by occurred_at desc lets the
-- planner satisfy the query from the index alone — no sequential scan, no sort.
create index if not exists audit_log_user_recent_idx
  on public.audit_log (user_id, occurred_at desc);

-- Finding-detail view: every action recorded for a given finding. Partial — the
-- soft reference is null for actions with no originating finding.
create index if not exists audit_log_finding_idx
  on public.audit_log (finding_id)
  where finding_id is not null;

-- 2. Immutability guard ───────────────────────────────────────────────────────
--
-- Rejects every UPDATE on the table, for every role. RLS already denies UPDATE
-- to the owner role, but the service role and SECURITY DEFINER functions bypass
-- RLS — this trigger is what makes the rows immutable to *them* too. DELETE is
-- deliberately not guarded: retention/cleanup prunes whole rows, which is not a
-- silent mutation of an entry's content.

create or replace function public.audit_log_forbid_update()
returns trigger
language plpgsql
as $$
begin
  raise exception 'audit_log is append-only: UPDATE on row % is not permitted', old.id
    using errcode = 'check_violation';
end;
$$;

drop trigger if exists audit_log_no_update on public.audit_log;
create trigger audit_log_no_update
  before update on public.audit_log
  for each row execute function public.audit_log_forbid_update();

-- 3. Row-level security ────────────────────────────────────────────────────────
--
-- Owner role: SELECT + INSERT of its own rows, nothing else. The absence of
-- UPDATE / DELETE policies is the enforcement — RLS denies by default — so the
-- table is INSERT-only to a founder. INSERT additionally pins approving_user_id
-- to the actor so a row can't be attributed to someone who didn't approve it.

alter table public.audit_log enable row level security;

drop policy if exists "audit_log_select_own" on public.audit_log;
create policy "audit_log_select_own" on public.audit_log
  for select using (auth.uid() = user_id);

drop policy if exists "audit_log_insert_own" on public.audit_log;
create policy "audit_log_insert_own" on public.audit_log
  for insert with check (
    auth.uid() = user_id and auth.uid() = approving_user_id
  );

-- 4. record_audit_log() ────────────────────────────────────────────────────────
--
-- The canonical writer. The Executor sub-issues (ENT-66/67/68) call this exactly
-- once per approved action so the snapshots and attribution are recorded by the
-- system, consistently, on the same write path. SECURITY DEFINER so a backend
-- (service-role / agent) caller can append regardless of session role, while the
-- table stays RLS-locked to direct readers. Returns the new entry's id.

create or replace function public.record_audit_log(
  p_user_id           uuid,
  p_finding_id        uuid,
  p_action_type       text,
  p_target_table      text,
  p_target_id         uuid,
  p_before            jsonb,
  p_after             jsonb,
  p_approving_user_id uuid
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_id uuid;
begin
  insert into public.audit_log (
    user_id, finding_id, action_type, target_table, target_id,
    before, after, approving_user_id
  )
  values (
    p_user_id, p_finding_id, p_action_type, p_target_table, p_target_id,
    p_before, p_after, p_approving_user_id
  )
  returning id into v_id;

  return v_id;
end;
$$;
