-- The Executor: create a DSAR tracking task on approval (ENT-67)
--
-- The Executor's second write path (epic ENT-35; PRD §5.4, §7.4, §10). When a
-- founder approves a DSAR-typed finding, a new `dsars` row is opened with a
-- 30-day countdown so the GDPR Article 12(3) deadline is tracked automatically,
-- and the act is recorded in the immutable audit log (ENT-69).
--
-- This reuses the machinery ENT-66 established: the `findings.action_type`
-- discriminator (the 'create_dsar' value was already declared in its check
-- constraint), `approve_finding()` as the explicit-approval entry point, and
-- `record_audit_log()` as the audit writer. The shape mirrors the ROPA path —
-- an AFTER UPDATE trigger gated on the pending→approved transition — so the same
-- "one approval → one row → one audit entry" guarantee holds.
--
-- The `dsars` table itself already exists: it was introduced with the Watcher's
-- DSAR deadline detectors (ENT-57, watcher_detect_dsar_escalation), which scan
-- the owner's open/in_progress DSARs and escalate as the deadline nears. Because
-- the Executor writes into that very table with status='open' and a future
-- response_due_at, the Watcher picks the new row up on its next run for free —
-- no detector change needed. This migration only adds the two columns the
-- Executor/Analyst flow needs on top of the existing schema:
--
--   * `handler`    — who owns the response. Surfaced in the DSAR Log (ENT-71)
--                    and pre-filled here from the Analyst payload.
--   * `finding_id` — provenance back to the approved finding, and the
--                    idempotency pivot (unique) so re-approval never logs a
--                    second DSAR for the same finding.
--
-- Scope boundary — ENT-71 builds the DSAR Log view/edit UI, the manual "Log a
-- DSAR" form, and the "Mark as responded" approval on top of this table.
--
-- Idempotent: `add column if not exists` / `create or replace` / `drop … if
-- exists`, and re-approving a finding never opens a second DSAR.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. DSAR columns the Executor flow adds ──────────────────────────────────────

alter table public.dsars
  add column if not exists handler text;

-- Soft reference (no FK): provenance that outlives a purged finding, mirroring
-- processing_activities.finding_id. Unique when present so one finding opens at
-- most one DSAR — the idempotency pivot for re-approval.
alter table public.dsars
  add column if not exists finding_id uuid;

create unique index if not exists dsars_finding_idx
  on public.dsars (finding_id)
  where finding_id is not null;

-- 2. Executor reaction ─────────────────────────────────────────────────────────
--
-- Fires on the pending→approved transition of a create_dsar finding. Opens a
-- dsars row with received_at = now() and a 30-day response deadline, pre-filled
-- from the Analyst payload (findings.metadata->'payload'), then records the
-- write in the audit log. SECURITY DEFINER so it can write while the tables stay
-- RLS-locked to direct callers.

create or replace function public.executor_create_dsar_on_approval()
returns trigger
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_dsar_id  uuid;
  v_after    jsonb;
  v_approver uuid := coalesce(new.approved_by, new.user_id);
begin
  -- One DSAR per finding. A repeat approval transition is a no-op.
  if exists (select 1 from public.dsars where finding_id = new.id) then
    return new;
  end if;

  insert into public.dsars (
    user_id, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    new.user_id,
    new.id,
    v_payload ->> 'requester',     -- the data subject who made the request
    v_payload ->> 'request_type',
    v_payload ->> 'handler',
    'open',
    now(),
    now() + interval '30 days'     -- response_due_at = received_at + 30 days
  )
  returning id into v_dsar_id;

  select to_jsonb(d.*) into v_after
  from public.dsars d
  where d.id = v_dsar_id;

  perform public.record_audit_log(
    new.user_id,    -- owner the entry belongs to
    new.id,         -- finding id
    'create_dsar',  -- action type
    'dsars',        -- target table
    v_dsar_id,      -- target id
    null,           -- before (a create has no prior state)
    v_after,        -- after (the whole new row)
    v_approver      -- approving user
  );

  return new;
end;
$$;

drop trigger if exists executor_create_dsar on public.findings;
create trigger executor_create_dsar
  after update of status on public.findings
  for each row
  when (
    new.status = 'approved'
    and old.status is distinct from 'approved'
    and new.action_type = 'create_dsar'
  )
  execute function public.executor_create_dsar_on_approval();
