-- +goose Up
-- 00017_billing_webhook_write_path.sql (ENT-210)
--
-- A role that can record what a payment provider says, and reach nothing else.
--
-- THE PROBLEM, WHICH THE SCHEMA ALREADY ANTICIPATED
--
-- Two tables have been waiting for this since 00001, and 00002 wrote down what
-- they were waiting for:
--
--   subscriptions            "writes stay off the app role: the future billing
--                             path runs on a system connection"
--   billing_webhook_events   "RLS on, ZERO policies, deliberately: provider
--                             webhook dedup state is infrastructure, reachable
--                             only by the system path"
--
-- So `kindlast_app` can read a subscription and write neither table, which is
-- correct: a request handler must not be able to grant its own caller a plan.
-- Nothing else could write them either, because FORCE ROW LEVEL SECURITY with
-- no policy means no rows for anybody, including the owner.
--
-- WHY A FIFTH ROLE AND NOT kindlast_agent
--
-- The obvious move is to extend the agent, which is already the system path:
-- it runs the sweeps and, since 00015, delivers notifications. It is the wrong
-- move, and the reason is worth keeping because the same reasoning went the
-- other way one migration ago.
--
-- 00015 declined to give the agent grants on memberships and identities and
-- used a definer function instead, but a *role* would not have helped there: a
-- dispatcher must learn every due row's recipients whichever role it runs as,
-- so a new role would have added a DSN without shrinking anything.
--
-- Here a role shrinks it sharply. 00008 made a point of what the agent is: it
-- can invent a finding, which is a claim about a customer's legal exposure, and
-- it deliberately cannot approve one or read who decided anything. Grant it
-- `insert, update` on `subscriptions` and it becomes a role that can invent a
-- finding AND grant itself a paid plan. That is a new capability, not a wider
-- read, and it is the combination an attacker would want.
--
-- The webhook is also a different trust boundary from the sweeps. It is
-- unauthenticated inbound, authenticated only by a signature over the body,
-- and it writes across tenants with no session. A role that literally cannot
-- reach a finding is the strongest available answer to ENT-210's criterion
-- that the webhook must not use a role which bypasses RLS.
--
-- `kindlast_billing` is NOSUPERUSER, NOBYPASSRLS, owns nothing, and holds
-- grants on exactly two tables. Created by deploy/postgres/init/01-roles.sh as
-- the superuser, because kindlast_migrator is NOCREATEROLE by design and this
-- migration must not be the exception that makes it CREATEROLE. Same
-- arrangement, and same loud failure, as 00008.
--
-- WHY ONE POLICY IS UNSCOPED, AND WHY THE REST ARE NOT
--
-- A provider event says "customer cus_123's subscription changed". Which
-- organisation that is is the answer rather than the question, so the handler
-- cannot set `app.current_org_id` before it has looked. That single lookup, by
-- `provider_customer_id`, is the read that makes scoping possible, and it is
-- the only unscoped policy here.
--
-- Everything after it is org-scoped in the ordinary way: the handler sets the
-- GUC to the organisation it just resolved and writes under org-equality
-- policies. So a webhook that resolves one customer cannot then write another
-- organisation's row, even though it is the same connection and the same
-- transaction. This is §20.1's user-less-actor exception (dedicated role, no
-- membership check, org predicate intact) rather than a per-role bypass.
--
-- WHAT BOUNDS THE BLAST RADIUS BEYOND THAT
--
--   billing_webhook_events   select, insert. No update and no delete, because a
--                            dedup ledger that can be rewritten is not one. The
--                            whole idempotency property is that seeing an event
--                            id twice means the second is a replay, and an actor
--                            able to delete a row can replay anything.
--
--   subscriptions            select, insert, update. No delete: a cancelled
--                            subscription becomes `status = 'canceled'` and
--                            stays, because billing history is part of the
--                            customer's record and a deleted row reads as a
--                            customer who never paid.
--
-- Nothing else in the schema. No organisations, no memberships, no identities,
-- no findings, no audit log.
--
-- WHAT THIS MIGRATION DELIBERATELY DOES NOT DO
--
-- It adds no `provider_subscription_id` column. The webhook resolves a customer
-- through `subscriptions.provider_customer_id`, which 00001 already provides,
-- and inventing schema for a provider integration nobody has configured would
-- be designing against a guess. The column arrives with the code that needs it.

-- +goose StatementBegin
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'kindlast_billing') then
    raise exception
      'kindlast_billing does not exist. It is created by '
      'deploy/postgres/init/01-roles.sh as the superuser, because the migrator '
      'is NOCREATEROLE. Recreate the stack with '
      '`docker compose -f deploy/compose.yaml down -v`, or create the role by '
      'hand (see the header of %).',
      '00017_billing_webhook_write_path.sql';
  end if;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- The dedup ledger
------------------------------------------------------------------------------

-- Revoked from kindlast_app explicitly, rather than left to the absence of a
-- policy. Same reasoning as 00015 gave for `capability_tokens`, and the same
-- trap: 00002 granted the application DML on every table then in the schema, so
-- with FORCE RLS and zero policies its reads return no rows and its writes
-- touch none, silently. That reads exactly like a boundary and is not one, it
-- is a table the application can address and finds empty.
--
-- A missing grant fails closed at parse time; a missing policy fails quietly at
-- run time. For a ledger whose entire purpose is recognising a replay, the loud
-- version is the one worth having.
revoke all on public.billing_webhook_events from kindlast_app;

grant select, insert on public.billing_webhook_events to kindlast_billing;

-- Unconditional, and it carries no org_id to scope by. The ledger records that
-- a provider event id has been seen, which is a fact about this deployment's
-- conversation with a provider rather than about any customer.
create policy billing_webhook_events_billing on public.billing_webhook_events
  to kindlast_billing
  using (true)
  with check (true);

------------------------------------------------------------------------------
-- The subscription itself
------------------------------------------------------------------------------

grant select, insert, update on public.subscriptions to kindlast_billing;

-- The lookup that makes scoping possible. Unscoped by necessity: the handler is
-- resolving which organisation a provider customer id belongs to, and cannot
-- name the organisation before it knows it.
--
-- Select only. A caller holding this can learn which organisation a customer id
-- maps to, and nothing else about them: `subscriptions` carries no personal
-- data, only a plan, a status and the provider's own identifiers.
create policy subscriptions_billing_lookup on public.subscriptions
  for select to kindlast_billing
  using (true);

-- And the writes, org-scoped in the ordinary way. The handler sets
-- `app.current_org_id` to the organisation it resolved above, so a webhook that
-- looked up one customer cannot write another organisation's row on the same
-- connection.
create policy subscriptions_billing_insert on public.subscriptions
  for insert to kindlast_billing
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

create policy subscriptions_billing_update on public.subscriptions
  for update to kindlast_billing
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

-- kindlast_app's `subscriptions_select_org` is untouched, so a member still
-- reads their own organisation's plan through the ordinary two-GUC form and
-- still cannot write it.

-- +goose Down

drop policy if exists subscriptions_billing_update on public.subscriptions;
drop policy if exists subscriptions_billing_insert on public.subscriptions;
drop policy if exists subscriptions_billing_lookup on public.subscriptions;
revoke select, insert, update on public.subscriptions from kindlast_billing;

drop policy if exists billing_webhook_events_billing on public.billing_webhook_events;
revoke select, insert on public.billing_webhook_events from kindlast_billing;
grant select, insert, update, delete on public.billing_webhook_events to kindlast_app;
