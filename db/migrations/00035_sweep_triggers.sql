-- +goose Up
-- 00035_sweep_triggers.sql (ENT-212 closed by ENT-256, part four)
--
-- The trigger that ENT-212 shipped without: confirming onboarding writes a
-- profile and nothing runs the Watcher over it. A member who finishes the
-- interview lands on a dashboard that has to say "no sweep has run yet",
-- indefinitely, until somebody who holds a service credential calls
-- `SweepService.RunSweep` by hand.
--
-- WHY THIS IS A TABLE AND NOT A CALL FROM INSIDE ConfirmProfile
--
-- `ConfirmProfile` writes the confirmed facts inside the request's own
-- transaction, on `kindlast_app`, and the tenancy interceptor does not commit
-- that transaction until after the handler returns (00008, interceptor/
-- tenancy.go). `RunSweep` runs on a separate connection pool entirely,
-- `kindlast_agent` (00008 again), in its own transaction. A handler that
-- called it directly would be asking a different connection to read facts a
-- still-open transaction on the first connection has not committed yet, and
-- under read committed it would see none of them: the very first sweep for a
-- newly onboarded organisation would silently run over an empty profile and
-- find nothing, which is exactly the failure `sweep.go`'s own comments call
-- the worst available outcome for a trigger.
--
-- So this is `transactional_outbox` (00014) with the send replaced by a
-- sweep. The application writes a marker row in the same transaction that
-- makes the fact true, and a row committed after the request completes is a
-- row that cannot be visible before the facts it depends on are. The Temporal
-- worker's relay (ENT-256, part three and four) lists it once it is actually
-- there and starts one sweep workflow per row, with the row id as the
-- workflow id, so a trigger is run by at most one workflow at a time; the
-- workflow runs the Watcher and then the Analyst on the agent pool through
-- SweepService, and settles the row.
--
-- WHY A SEPARATE TABLE FROM `transactional_outbox` ITSELF
--
-- Same reasoning 00014 already wrote down for keeping `notification_outbox`
-- separate: this is a third shape. A transactional message carries a
-- recipient and a rendered body and is delivered once, to an address that may
-- belong to no user of this system. A sweep trigger carries nothing but which
-- organisation, is delivered by calling a function rather than sending mail,
-- and its `Deliver` is `RunSweep` rather than an SMTP conversation. Forcing
-- them into one table buys a wider `kind` discriminator and a set of check
-- constraints describing which columns a row of each kind is allowed to use,
-- for two shapes that share nothing but "written once, drained later".
--
-- THE GRANT SPLIT, WHICH IS 00014'S AGAIN
--
-- `kindlast_app` enqueues, inside a transaction it does not control the
-- commit of by itself, and never marks a row done: that would let a request
-- handler assert a sweep happened that nothing ran. `kindlast_agent` lists and
-- marks, across every organisation, and cannot create a trigger: the role
-- that runs sweeps does not get to decide which organisations need one.
-- Insert-only and update-only, on opposite sides, same as the table this one
-- is modelled on.
--
-- ONE REASON TODAY, A CHECK CONSTRAINT RATHER THAN A FREE-TEXT COLUMN
--
-- `reason` exists so a second cause (a corrected fact material enough to
-- rerun the Watcher, a corpus update) has somewhere to say why the row exists
-- without a customer-facing surface guessing. Constrained to what is actually
-- caused today, widened in the change that introduces the second caller,
-- exactly as `transactional_outbox.kind` was.

create table public.sweep_triggers (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  reason text not null check (reason in ('onboarding_confirmed')),

  status   text not null default 'pending' check (status in ('pending', 'done')),
  attempts integer not null default 0,
  last_error text,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  done_at    timestamptz,

  -- "Done" is one fact, not two that can disagree, for the same reason
  -- transactional_outbox pairs status with sent_at.
  constraint sweep_triggers_done_at_matches_status
    check ((status = 'done') = (done_at is not null))
);

-- The relay's only query, and it only ever wants pending rows.
create index sweep_triggers_pending_idx
  on public.sweep_triggers (created_at)
  where status = 'pending';

create trigger set_updated_at
  before update on public.sweep_triggers
  for each row execute function public.set_updated_at();

alter table public.sweep_triggers enable row level security;
alter table public.sweep_triggers force row level security;

------------------------------------------------------------------------------
-- kindlast_app: enqueue, within one organisation
------------------------------------------------------------------------------

grant insert on public.sweep_triggers to kindlast_app;

-- The ordinary two-GUC form, as a check on the row being inserted rather than
-- a read filter. Onboarding carries no role gate (any member may confirm the
-- interview they were provisioned into), so unlike transactional_outbox's
-- owner-only insert policy, this one only asks that the row names the caller's
-- own organisation and that the caller belongs to it.
create policy sweep_triggers_insert_member on public.sweep_triggers
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- Deliberately absent: no select, update or delete policy for kindlast_app.
-- Nothing reads this table back from the request path today, and the role
-- that enqueues a sweep does not get to mark one done. See the header.

------------------------------------------------------------------------------
-- kindlast_agent: claim and mark, across every organisation
------------------------------------------------------------------------------

-- No insert. The role that runs sweeps does not get to decide which
-- organisations need one; see the header.
grant select, update on public.sweep_triggers to kindlast_agent;

create policy sweep_triggers_agent on public.sweep_triggers
  to kindlast_agent
  using (true)
  with check (true);

------------------------------------------------------------------------------
-- sweep_targets(): which organisations a scheduled sweep visits
------------------------------------------------------------------------------
--
-- The daily sweep (ENT-256, part four) is "sweep everyone", which sweep.proto
-- insists must be a loop somebody writes deliberately rather than a parameter
-- somebody passes by accident. The loop is the Temporal workflow, one
-- organisation per activity, and it needs the list. `kindlast_agent` cannot
-- produce one: it holds nothing on `organisations` (00008) and its policies on
-- `compliance_profiles` are scoped to the organisation the session GUC names,
-- so with no GUC set it sees no rows at all. That is the structural case for a
-- SECURITY DEFINER function: RLS cannot express "the set of tenants that have
-- something to sweep" for a role that is deliberately not allowed to enumerate
-- tenants, and the function answers exactly that question and nothing adjacent.
--
-- What it returns is organisation ids, with no name, no slug and no member.
-- An organisation with no compliance profile is not returned because a sweep
-- over it does nothing (run_watcher() walks profiles), and returning it would
-- be a wasted activity per tenant per day for every organisation that signed
-- up and never finished onboarding. Ninth definer function; the table in
-- db/README.md is the list.

-- +goose StatementBegin
create or replace function public.sweep_targets()
returns setof uuid
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select distinct p.org_id
    from public.compliance_profiles p
   order by p.org_id;
$function$;
-- +goose StatementEnd

revoke all on function public.sweep_targets() from public;
grant execute on function public.sweep_targets() to kindlast_agent;

-- +goose Down

drop function if exists public.sweep_targets();

drop policy if exists sweep_triggers_agent          on public.sweep_triggers;
drop policy if exists sweep_triggers_insert_member   on public.sweep_triggers;

revoke all on public.sweep_triggers from kindlast_agent;
revoke all on public.sweep_triggers from kindlast_app;

drop table if exists public.sweep_triggers;
