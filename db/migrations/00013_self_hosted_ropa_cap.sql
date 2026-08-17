-- +goose Up
-- 00013_self_hosted_ropa_cap.sql (ENT-200 follow-up)
--
-- Stop capping manually added Article 30 entries on a deployment that does not
-- bill anybody.
--
-- THE MISMATCH, WHICH ONLY SHOWS UP ON THE DEFAULT CONFIGURATION
--
-- `KINDLAST_BILLING_ENABLED` is false unless set, which is the self-hosted
-- default and the documented one (§18.1): a deployment that sells nothing
-- should gate nothing. core-api honours it, so `ListProcessingActivities`
-- reports no cap and the console shows no limit.
--
-- `ropa_manual_activity_limit()` never learned. It asks one question, "does this
-- organisation have a `pro` subscription", and on a self-hosted stack the
-- subscriptions table is empty, so every organisation is capped at three.
--
-- The result is the worst of both: nothing warns the customer they are near a
-- limit, because the API says there is none, and then the fourth activity is
-- refused with a message about a plan that does not exist on their deployment
-- and that they cannot buy. Found by reading the two halves against each other
-- while building the write path, and confirmed against a running stack.
--
-- WHY A SESSION GUC RATHER THAN ANYTHING ELSE
--
-- The function cannot read an environment variable, and the alternative shapes
-- are worse. Inferring from "is the subscriptions table empty" breaks the moment
-- one organisation has a row and another legitimately does not. A settings table
-- adds a row that must be kept in step with the environment by hand, which is a
-- second source of truth for a fact the process already holds.
--
-- A GUC set per transaction is the shape this schema already uses for exactly
-- this problem: `app.current_org_id` and `app.current_user_id` are facts the
-- process knows and the database needs, carried the same way. `app.billing_enabled`
-- joins them.
--
-- UNSET MEANS DISABLED, WHICH IS THE SAFE DIRECTION HERE
--
-- `current_setting(name, true)` returns null when nothing set it, and null is
-- read as off. So a deployment that never sets it is uncapped, and so is one
-- whose interceptor fails to set it.
--
-- That is deliberately the opposite of how a security GUC behaves. Tenancy fails
-- closed because an unset org id must never mean "all organisations". This is a
-- billing gate, not a boundary: failing open charges nobody and blocks nobody,
-- while failing closed would refuse a paying customer's work because a
-- configuration line was missing. §18.1 already says the act path is ungated
-- unless configured; this makes the cap agree with it.
--
-- NOT A TENANCY CHANGE
--
-- No policy, grant or column is touched, and the new GUC is never consulted by
-- any policy. An organisation still sees exactly its own rows.

-- +goose StatementBegin
create or replace function public.ropa_manual_activity_limit()
returns integer
language sql
stable
set search_path to 'public', 'pg_temp'
as $function$
  select case
    -- Billing off, or nothing said: no cap. See the header.
    when coalesce(current_setting('app.billing_enabled', true), '') not in ('on', 'true', '1')
      then null::integer
    when exists (
      select 1 from public.subscriptions
      where org_id = public.app_current_org_id() and plan = 'pro'
    ) then null::integer
    else 3
  end
$function$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
create or replace function public.ropa_manual_activity_limit()
returns integer
language sql
stable
set search_path to 'public', 'pg_temp'
as $function$
  select case
    when exists (
      select 1 from public.subscriptions
      where org_id = public.app_current_org_id() and plan = 'pro'
    ) then null::integer
    else 3
  end
$function$;
-- +goose StatementEnd
