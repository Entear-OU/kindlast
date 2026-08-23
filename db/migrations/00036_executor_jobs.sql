-- +goose Up
-- 00036_executor_jobs.sql (ENT-271, ENT-225 phase 2)
--
-- The Executor stops being three triggers and becomes a workflow.
--
-- WHAT WAS HERE, AND WHY IT IS GOING
--
-- 00002 created three `after update of status` triggers on `findings`. When a
-- finding whose `action_type` is `create_ropa`, `create_dsar` or
-- `create_ai_system` becomes approved, the matching trigger inserts a
-- `processing_activities`, `dsars` or `ai_systems` row and writes the audit
-- entry, inside the approving transaction, running as `kindlast_app` with the
-- approver's GUCs already set.
--
-- Two things are wrong with that, and only one of them is the layer rule.
--
-- The layer rule first (§14.5, ENT-225): a trigger that decides is a decision
-- in plpgsql. `executor_create_ai_system_on_approval` refuses a High-Risk
-- classification without a reviewed approval by raising `check_violation`,
-- which aborts the whole approving transaction and reaches the caller as
-- whatever the generic error mapping makes of it. §3 asks for
-- `failed_precondition`. That gate moves to Go, where it is checked BEFORE the
-- approval is written, so a refusal leaves nothing behind.
--
-- The second is the design's own sequencing (§3): "ApproveFinding writes the
-- finding, writes audit_log, and publishes finding.approved. It does not
-- synchronously create the ROPA entry, AI system, or DSAR." Execution belongs
-- behind the event boundary, and with Temporal in the stack (ENT-256) there is
-- somewhere for it to go.
--
-- WHY A TABLE AND NOT A WORKFLOW STARTED FROM THE HANDLER
--
-- 00035 wrote this argument out for sweeps and it holds here unchanged: a
-- workflow started after the commit can be lost between the commit and the
-- start, and a workflow started before it can read a transaction that has not
-- committed. A row written inside the approving transaction cannot be either.
-- So this is `transactional_outbox` (00014) again with the send replaced by an
-- execution: the application enqueues in the transaction that makes the
-- approval true, a relay lists what is pending and starts one workflow per
-- row, and the workflow asks core-api to execute it.
--
-- THE GRANT SPLIT, WHICH IS NOT 00035's AND THE DIFFERENCE IS THE POINT
--
-- `sweep_triggers` gives the application insert and the agent select and
-- update, because a sweep runs as the producer. An execution cannot: it writes
-- a customer's compliance record, and `kindlast_agent` holds nothing at all on
-- `processing_activities` or `ai_systems` and only `select` on `dsars`
-- (00008, 00032). Granting it writes would make the role that can invent a
-- finding also able to write the record that finding creates, which is the one
-- separation the producer role exists for.
--
-- So the execution runs on `kindlast_app`, in a transaction whose GUCs name
-- the organisation and the approver read out of the job row: exactly the
-- authority the trigger had, held for exactly as long. The application
-- therefore needs select and update here as well as insert, and the split is
-- drawn differently:
--
--   kindlast_app     insert (enqueue, inside the approving transaction),
--                    select and update SCOPED TO ITS OWN ORGANISATION, for
--                    the execution transaction that claims and settles one job
--   kindlast_agent   select only, across every organisation, because the
--                    relay has to be able to ask "what is pending" without a
--                    tenant. It cannot enqueue and it cannot settle: the role
--                    that lists the work does not get to say the work is done
--
-- WHAT IS NOT IN THIS TABLE
--
-- The payload. The trigger read `new.metadata -> 'payload'` at approval time,
-- and the execution reads it from the finding when it runs. That is the same
-- value: `findings.metadata` is written by the Analyst when the finding is
-- produced and nothing in core-api ever updates it, so it cannot drift between
-- the approval and the execution a second later. Copying it here would be a
-- second copy of a customer's proposed record, in a second table, with its own
-- retention question, to protect against a write path that does not exist.

create table public.executor_jobs (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- The finding whose approval asked for this, and what it asked for. One job
  -- per finding: approving twice is idempotent upstream and the unique
  -- constraint says so here too.
  finding_id  uuid not null references public.findings(id) on delete cascade,
  action_type text not null check (action_type in ('create_ropa', 'create_dsar', 'create_ai_system')),

  -- Who approved it. The execution runs as this person, and the audit row it
  -- writes names them, because the record exists by their decision and not by
  -- the system's.
  approved_by uuid not null,

  status     text not null default 'pending' check (status in ('pending', 'done')),
  attempts   integer not null default 0,
  last_error text,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  done_at    timestamptz,

  constraint executor_jobs_one_per_finding unique (finding_id),

  -- "Done" is one fact, not two that can disagree, for the same reason
  -- transactional_outbox pairs status with sent_at.
  constraint executor_jobs_done_at_matches_status
    check ((status = 'done') = (done_at is not null))
);

-- The relay's only query, and it only ever wants pending rows.
create index executor_jobs_pending_idx
  on public.executor_jobs (created_at)
  where status = 'pending';

create trigger set_updated_at
  before update on public.executor_jobs
  for each row execute function public.set_updated_at();

alter table public.executor_jobs enable row level security;
alter table public.executor_jobs force row level security;

------------------------------------------------------------------------------
-- kindlast_app: enqueue, and execute, within one organisation
------------------------------------------------------------------------------

grant insert, select, update on public.executor_jobs to kindlast_app;

-- The ordinary two-GUC form. Approving is already gated by the act path's own
-- rules, so this asks what every tenant policy asks: the row names the
-- caller's organisation and the caller belongs to it.
create policy executor_jobs_member on public.executor_jobs
  for all
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- Deliberately absent: no delete grant and no delete policy. A job that ran is
-- a record that a customer's compliance record was created by a decision, and
-- the only thing that removes one is the cascade from `organisations`, which
-- is how erasing an organisation already works.

------------------------------------------------------------------------------
-- executor_job_context(): whose job this is, before there is a tenant
------------------------------------------------------------------------------
--
-- The tenth SECURITY DEFINER function, and the argument for it is a
-- chicken-and-egg the two-GUC form cannot express.
--
-- The execution runs as the approver: it opens a transaction, sets
-- `app.current_org_id` and `app.current_user_id` from the job row, and does
-- everything else under the ordinary policy. But the policy on this table
-- tests those GUCs, so reading the row that says what to set them to is a read
-- no tenant transaction can make: with nothing set, the policy's org equality
-- is null, the row is invisible under FORCE ROW LEVEL SECURITY, and the
-- executor cannot learn whose job it is.
--
-- The alternatives, and why this one:
--
--   A policy permitting select when no organisation is set. Rejected: it opens
--   every job row in the deployment to any application connection that has not
--   resolved tenancy, which is a bug class rather than a state, and 00029's
--   whole point is that a table addressable-but-empty is not a boundary.
--
--   The worker passing the organisation and the approver. Rejected for the
--   approver: a caller that names whose authority executes a job is a caller
--   that can create a customer's compliance record in somebody else's name.
--   (The organisation alone would be safe, because a wrong one makes the job
--   invisible under the policy, but it does not solve the approver.)
--
-- So: a definer function that answers exactly this question about exactly one
-- row addressed by its primary key, and nothing adjacent. It cannot list, it
-- cannot filter, and what it returns is used to SET the tenancy rather than to
-- read anything under it. Same shape and same argument as
-- `notification_recipients` (00015).

-- +goose StatementBegin
create or replace function public.executor_job_context(p_job_id uuid)
returns table (
  org_id      uuid,
  approved_by uuid,
  finding_id  uuid,
  action_type text
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select j.org_id, j.approved_by, j.finding_id, j.action_type
    from public.executor_jobs j
   where j.id = p_job_id;
$function$;
-- +goose StatementEnd

revoke all on function public.executor_job_context(uuid) from public;
grant execute on function public.executor_job_context(uuid) to kindlast_app;

------------------------------------------------------------------------------
-- kindlast_agent: list, across every organisation, and nothing else
------------------------------------------------------------------------------

grant select on public.executor_jobs to kindlast_agent;

create policy executor_jobs_agent_read on public.executor_jobs
  for select
  to kindlast_agent
  using (true);

------------------------------------------------------------------------------
-- The triggers come out
------------------------------------------------------------------------------
--
-- Dropped here rather than left in place beside the workflow, because two
-- things creating the same record is worse than either: the trigger would win
-- the race inside the transaction, the workflow would find its `exists` guard
-- satisfied, and the system would look correct while the code that is supposed
-- to be doing the work never did any. ENT-225's rule is that a function is
-- dropped once its Go path is proven able to fail the same way, and the tests
-- that prove it land in the same change.

drop trigger if exists executor_create_ropa      on public.findings;
drop trigger if exists executor_create_dsar      on public.findings;
drop trigger if exists executor_create_ai_system on public.findings;

drop function if exists public.executor_create_ropa_on_approval();
drop function if exists public.executor_create_dsar_on_approval();
drop function if exists public.executor_create_ai_system_on_approval();

-- +goose Down

drop function if exists public.executor_job_context(uuid);

drop policy if exists executor_jobs_agent_read on public.executor_jobs;
drop policy if exists executor_jobs_member     on public.executor_jobs;

revoke all on public.executor_jobs from kindlast_agent;
revoke all on public.executor_jobs from kindlast_app;

drop table if exists public.executor_jobs;

-- The trigger functions and their triggers are NOT recreated here, and that is
-- deliberate rather than lazy. Restoring them would mean pasting three bodies
-- that this migration exists to retire, into a Down path nobody runs except to
-- get back to a schema whose Go half no longer matches. Rolling back past this
-- migration means checking out the code that went with it.
