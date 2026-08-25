-- +goose Up

-------------------------------------------------------------------------------
-- A scheduled fetch deposits evidence (ENT-279, ENT-231, ENT-274)
-------------------------------------------------------------------------------
--
-- WHAT WAS MISSING, AND WHY IT MADE A SHIPPED FEATURE INERT
--
-- ENT-274 gave the Watcher `read_evidence`: it can read observations a fetch
-- has already deposited for one connection and one granted tool. Nothing
-- deposited any on a schedule, so the stored evidence was whatever a person
-- happened to click Fetch for on the Integrations page, and the tool usually
-- found nothing.
--
-- This migration is the database half of the thing that deposits: two
-- questions the Temporal schedule's RPCs have to ask and that row level
-- security structurally cannot answer.
--
-- WHAT THIS DELIBERATELY DOES NOT DO
--
-- It does not widen `kindlast_agent`. 00025 gives the producer role a
-- column-limited select on `integrations` that omits `credential_ciphertext`,
-- and that stays exactly as it is. The shape is that a scheduled fetch
-- deposits and a sweep reads; the role that runs models never holds the
-- material needed to dial a customer, and the second function below is what
-- lets the fetch happen without it, by handing the credential-reading half to
-- the application role acting as the person who consented.
--
-- Whether an agent may cause a fetch is a separate decision and is not taken
-- here.

-------------------------------------------------------------------------------
-- The index the "is this connection's tool due" question needs
-------------------------------------------------------------------------------
--
-- 00025 indexes `(org_id, integration_id, requested_at desc)`, which answers
-- "what did we fetch for this connection, newest first" and is what the
-- console's fetch log reads. The scheduler asks a different question, per tool
-- and across every organisation at once, so the org column at the front is
-- dead weight and `tool` is not there at all.
create index integration_fetches_tool_recency_idx
  on public.integration_fetches (integration_id, tool, requested_at desc);

-------------------------------------------------------------------------------
-- fetch_targets(): which connection and tool is due a scheduled fetch
-------------------------------------------------------------------------------
--
-- The eleventh SECURITY DEFINER function, and the argument is the one
-- `sweep_targets` makes (00035) applied to a second cross-tenant list.
--
-- The producer role cannot enumerate tenants. Its policies on `integrations`
-- and `integration_tools` are scoped to `app.current_org_id`, so a connection
-- with no GUC set sees no rows at all, and there is no organisation to set
-- because working out which organisations have something to fetch is the
-- question being asked. RLS cannot express "the set of tenants with a stale
-- granted tool" for a role that is deliberately not allowed to list tenants.
--
-- WHAT IT RETURNS, AND WHAT IT REFUSES TO RETURN
--
-- An organisation id, a connection id and a tool name. No endpoint, no
-- credential ciphertext, no key id, no consenting user. The caller of this
-- function is core-api on the producer pool, and what it learns is which work
-- exists rather than how to do it; the material a fetch needs is read later,
-- by the application role, under the ordinary two-GUC policy.
--
-- FOUR FILTERS, EACH OF WHICH IS A CUSTOMER DECISION THIS MUST NOT WIDEN
--
--   `status = 'active'`     a revoked connection is never dialled again.
--   `granted`               `integration_tools.granted` is the customer's
--                           decision on the consent screen. A schedule that
--                           fetched an ungranted tool would be the product
--                           overriding the screen it asked them to fill in.
--   `not write_capable`     a schedule may only READ. Nobody is watching, so
--                           nothing could catch a scheduled call that closed a
--                           ticket or deleted a record, and the evidence a
--                           compliance product wants is what a system reports
--                           rather than what it can be made to do. A
--                           write-granted tool stays a thing a person
--                           triggers.
--   the recency `not exists`  bounds how often a customer's system is dialled,
--                           and does so on ATTEMPTS rather than successes. An
--                           endpoint that is down records a `failed` row, and
--                           that row is what stops the next tick trying again
--                           immediately. Keyed on successes, a broken endpoint
--                           would be dialled on every tick forever.
--
-- The staleness interval is a parameter rather than a constant here because it
-- is a decision (how fresh evidence should be), and decisions live in Go. It
-- is NOT a field on the RPC: a caller that could pass zero would dial every
-- customer's systems at once, so core-api holds it as a constant and the
-- Temporal worker cannot influence it.

-- +goose StatementBegin
create or replace function public.fetch_targets(
  p_stale_after interval,
  p_limit       integer
)
returns table (
  org_id         uuid,
  integration_id uuid,
  tool           text
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select i.org_id, i.id, t.name
    from public.integrations i
    join public.integration_tools t
      on t.integration_id = i.id
   where i.status = 'active'
     and t.granted
     and not t.write_capable
     and not exists (
           select 1
             from public.integration_fetches f
            where f.integration_id = i.id
              and f.tool = t.name
              and f.requested_at > now() - p_stale_after
         )
   order by i.org_id, i.id, t.name
   limit p_limit;
$function$;
-- +goose StatementEnd

revoke all on function public.fetch_targets(interval, integer) from public;
grant execute on function public.fetch_targets(interval, integer) to kindlast_agent;

-------------------------------------------------------------------------------
-- integration_fetch_context(): whose consent a scheduled fetch runs on
-------------------------------------------------------------------------------
--
-- The twelfth, and the same chicken-and-egg `executor_job_context` (00036)
-- has, for the same reason and with the same shape of answer.
--
-- Reading a connection's endpoint and sealed credential needs the application
-- role: 00025 gives `kindlast_app` the columns and withholds
-- `credential_ciphertext` from `kindlast_agent` on purpose, and this migration
-- does not touch that. But `integrations_select_org` tests both GUCs, so the
-- transaction has to know which organisation and which person to be before it
-- can read anything, and the row that says so is behind the policy those GUCs
-- drive.
--
-- WHY A PERSON AT ALL, WHEN NOBODY ASKED FOR THIS FETCH
--
-- Because a connection is a standing consent rather than a click. Somebody
-- named the endpoint, ticked the tools and pressed connect, and a scheduled
-- fetch is that consent being exercised rather than a new authority. Running
-- it as the most recent consenting person keeps the two-GUC form intact end to
-- end, and gives the property a compliance product should want: when that
-- person is no longer a member of the organisation, the policy's membership
-- `exists` fails, the connection is invisible, and the scheduled fetch stops.
-- Consent that outlives everyone who gave it is exactly what a customer would
-- object to on reading their own audit log.
--
-- The alternatives, and why this one:
--
--   The worker naming the organisation and the user. Rejected for the same
--   reason 00036 rejected it: a caller that names whose authority a fetch runs
--   under is a caller that can reach a customer's systems in somebody else's
--   name.
--
--   Widening `kindlast_agent` to `credential_ciphertext` so the producer role
--   could do the whole thing. That is ENT-279's other half and a security
--   decision in its own right, deliberately not taken here.
--
--   A policy on `integrations` permitting select with no user set. Rejected:
--   it would open every connection in the deployment, credential column
--   included, to any application connection that has not resolved tenancy.
--
-- So: a definer function answering exactly this about exactly one row
-- addressed by its primary key. It cannot list and it cannot filter, and what
-- it returns is used to SET the tenancy rather than to read anything under it.
--
-- It answers for a revoked connection too, and that is deliberate. The refusal
-- has to be recorded against an organisation, and a function that went silent
-- on revoked rows would leave the scheduler with a connection it may not fetch
-- and nowhere to write down that it did not.

-- +goose StatementBegin
create or replace function public.integration_fetch_context(p_integration_id uuid)
returns table (
  org_id       uuid,
  consented_by uuid
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select i.org_id,
         coalesce(
           (select c.consented_by
              from public.integration_consents c
             where c.integration_id = i.id
             order by c.consented_at desc, c.id desc
             limit 1),
           i.created_by)
    from public.integrations i
   where i.id = p_integration_id;
$function$;
-- +goose StatementEnd

revoke all on function public.integration_fetch_context(uuid) from public;
grant execute on function public.integration_fetch_context(uuid) to kindlast_app;

-- +goose Down

drop function if exists public.integration_fetch_context(uuid);
drop function if exists public.fetch_targets(interval, integer);

drop index if exists public.integration_fetches_tool_recency_idx;
