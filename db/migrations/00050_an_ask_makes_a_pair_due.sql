-- +goose Up
-- 00050_an_ask_makes_a_pair_due.sql (ENT-279)
--
-- The relay's union arm: a pending fetch request makes its pair due now,
-- rather than when the daily staleness window happens to reach it.
--
-- 00049 created the ask as a row and said, in as many words, that the relay's
-- due-listing would arrive "as an arm of the scheduled fetch's own definer
-- listing". Without this, `RequestFetch` acknowledges an ask that then waits
-- for the schedule like everything else, which makes the RPC a polite way of
-- doing nothing: the one thing an agent's ask means is "sooner than the
-- schedule", and only this listing can grant that.
--
-- WHAT AN ASK DOES NOT GET TO SKIP
--
-- Every customer-decision filter from 00048 applies to a requested pair
-- unchanged: a revoked connection is not dialled, an ungranted tool is not
-- fetched, a write-capable tool is never touched by the relay. An ask can
-- change WHEN a permitted fetch happens; it cannot make an impermissible one
-- permitted. The Go ask-path refuses those asks up front, and this listing
-- refuses them again at fetch time, because a grant withdrawn between the ask
-- and the relay tick must stop the fetch (00049 makes that argument for why
-- the checks are not insert-time constraints).
--
-- SERVED IS DERIVED, NOT WRITTEN BACK
--
-- A request row is never updated (00049: no status, no settle). A request
-- counts as pending while it is younger than `p_request_window` and no
-- attempt on its pair has been made since it was created. Any attempt after
-- the ask, failed included, is the ask served: attempts suppressing dials is
-- 00048's rule, and an ask that could force a redial of a down endpoint every
-- tick would be the bound it removed.
--
-- THE OLD ARITY IS DROPPED FIRST, WHICH IS NOT TIDYING UP
--
-- `create or replace function` matches on the argument list, so the
-- three-parameter version below would OVERLOAD the two-parameter 00048
-- version rather than replace it, and every caller would fail with "function
-- fetch_targets(...) is not unique". 00039 shipped that bug and its test
-- caught it; the drop is the fix it wrote down.

drop function if exists public.fetch_targets(interval, integer);

-- +goose StatementBegin
create or replace function public.fetch_targets(
  p_stale_after     interval,
  p_request_window  interval,
  p_limit           integer
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
     and (
       -- The schedule's arm, exactly as 00048 wrote it: stale, and no attempt
       -- inside the staleness window.
       not exists (
             select 1
               from public.integration_fetches f
              where f.integration_id = i.id
                and f.tool = t.name
                and f.requested_at > now() - p_stale_after
           )
       -- The ask's arm: a pending request, unserved by any attempt since.
       or exists (
             select 1
               from public.fetch_requests r
              where r.integration_id = i.id
                and r.tool = t.name
                and r.created_at > now() - p_request_window
                and not exists (
                      select 1
                        from public.integration_fetches f
                       where f.integration_id = i.id
                         and f.tool = t.name
                         and f.requested_at >= r.created_at
                    )
           )
     )
   order by i.org_id, i.id, t.name
   limit p_limit;
$function$;
-- +goose StatementEnd

revoke all on function public.fetch_targets(interval, interval, integer) from public;
grant execute on function public.fetch_targets(interval, interval, integer) to kindlast_agent;

-- +goose Down

drop function if exists public.fetch_targets(interval, interval, integer);

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
