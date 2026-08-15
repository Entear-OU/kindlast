-- +goose Up
-- 00006_act_path_audit.sql (ENT-203)
--
-- The act path records the act.
--
-- WHAT WAS WRONG, MEASURED RATHER THAN ASSUMED
--
-- ENT-203 was written believing that approve_finding and reject_finding
-- "already write the audit row, through record_audit_log", and warned that a
-- handler writing one too would produce duplicates. The opposite is true, and
-- it is worth writing down here because the next person will read that issue.
--
-- Neither function contains a call to record_audit_log. Every call site an
-- approval can reach lives inside one of the three executor triggers, and all
-- three WHEN clauses require new.action_type to be create_ropa, create_dsar or
-- create_ai_system. ENT-165 establishes that action_type is always 'review',
-- because analyst_convert_signal's INSERT column list omits it. So the three
-- triggers are unreachable, and approving a finding writes no audit row at all.
--
-- Two things follow, and the second is easy to miss:
--
--   1. The product's central claim, that a human can check what was decided
--      and by whom, is currently unbacked. There is nothing to check.
--   2. approve_finding returns the created record's id by reading target_id
--      from the newest audit_log row for the finding. With no rows, it always
--      returns null. The "take the founder to the new row" behaviour has never
--      worked, and would have read as a UI bug rather than a missing write.
--
-- THE SHAPE OF THE FIX
--
-- The decision and its consequence are two different facts and belong in two
-- different places. Approving a finding is an auditable act whether or not it
-- also creates a ROPA, so the decision is recorded by the act function. The
-- executor triggers keep recording the record they create, with the target_id
-- that points at it. Once action_type is derived (ENT-165) and a trigger does
-- fire, an approval produces two rows: one saying a human decided, one saying
-- a record was created. That is the correct reading of both, not a duplicate.
--
-- Because an AFTER UPDATE ... FOR EACH ROW trigger fires before control returns
-- to the function body, the executor's row already exists by the time the
-- target_id lookup runs. The lookup therefore reads before the decision row is
-- written, and additionally excludes rows whose target is the finding itself,
-- so a decision row can never be mistaken for a created record.
--
-- IDEMPOTENCY
--
-- approve_finding and reject_finding are guarded by `status <> '...'`, so a
-- second call matches no row, writes nothing, and returns the same answer as
-- the first. That guard is what makes the audit write idempotent, and the
-- write sits inside it deliberately.
--
-- snooze_finding is deliberately NOT idempotent, and this is a judgement rather
-- than an oversight. It has no status guard, because deferring a finding that
-- is already deferred is a real second decision with a new date, not a repeat
-- of the first. Recording only the first would leave the trail saying a finding
-- was deferred once when a human deferred it four times, which is exactly the
-- kind of thing an auditor is looking for.
--
-- Nothing here is SECURITY DEFINER. The inserts go through audit_log_insert_org
-- like any other write, which requires the row's user_id to equal the GUC user;
-- record_audit_log is passed app_current_user_id(), so it satisfies that by
-- construction rather than by convention.

-- +goose StatementBegin
create or replace function public.approve_finding(p_finding_id uuid, p_reviewed boolean default false)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_org     uuid := public.app_current_org_id();
  v_updated uuid;
  v_target  uuid;
  v_before  jsonb;
  v_after   jsonb;
begin
  if v_user is null then
    raise exception 'approve_finding: not authenticated';
  end if;

  -- The prior state, for the audit row. Read through the same policies as
  -- everything else, so a finding in another organisation is invisible here
  -- and the update below matches nothing either.
  select to_jsonb(f.*) into v_before
  from public.findings f
  where f.id = p_finding_id and f.org_id = v_org;

  update public.findings
    set status = 'approved',
        approved_by = v_user,
        approval_reviewed = p_reviewed
    where id = p_finding_id
      and org_id = v_org
      and status <> 'approved'
    returning id into v_updated;

  if v_updated is null then
    return null;  -- unknown finding, wrong org, or already approved
  end if;

  -- The created record's id, for "take the founder to the new row". Generic
  -- across every Executor action: the trigger always records target_id.
  select target_id into v_target
  from public.audit_log
  where finding_id = p_finding_id
    and target_id is not null
    and target_table <> 'findings'
  order by occurred_at desc
  limit 1;

  select to_jsonb(f.*) into v_after
  from public.findings f
  where f.id = p_finding_id;

  perform public.record_audit_log(
    v_org, v_user, p_finding_id, 'approve_finding',
    'findings', p_finding_id, v_before, v_after, v_user
  );

  return v_target;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.reject_finding(p_finding_id uuid, p_reason text default null)
returns boolean
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_org     uuid := public.app_current_org_id();
  v_updated uuid;
  v_profile uuid;
  v_slug    text;
  v_count   int;
  v_before  jsonb;
  v_after   jsonb;
  c_threshold constant int := 3;
begin
  if v_user is null then
    raise exception 'reject_finding: not authenticated';
  end if;

  select to_jsonb(f.*) into v_before
  from public.findings f
  where f.id = p_finding_id and f.org_id = v_org;

  update public.findings
    set status = 'rejected',
        rejection_reason = nullif(btrim(p_reason), ''),
        snoozed_until = null
  where id = p_finding_id
    and org_id = v_org
    and status <> 'rejected'
  returning id, profile_id, obligation_slug
    into v_updated, v_profile, v_slug;

  if v_updated is null then
    return false;  -- unknown finding, wrong org, or already rejected
  end if;

  if v_slug is not null then
    select count(*)
      into v_count
      from public.findings
     where profile_id = v_profile
       and obligation_slug = v_slug
       and status = 'rejected';

    if v_count >= c_threshold then
      insert into public.product_review_flags (
        org_id, created_by, profile_id, obligation_slug, finding_id, rejection_count, reasons
      )
      values (
        v_org,
        v_user,
        v_profile,
        v_slug,
        v_updated,
        v_count,
        (
          select array_remove(array_agg(distinct rejection_reason), null)
            from public.findings
           where profile_id = v_profile
             and obligation_slug = v_slug
             and status = 'rejected'
        )
      )
      on conflict (profile_id, obligation_slug) do nothing;
    end if;
  end if;

  select to_jsonb(f.*) into v_after
  from public.findings f
  where f.id = p_finding_id;

  perform public.record_audit_log(
    v_org, v_user, p_finding_id, 'reject_finding',
    'findings', p_finding_id, v_before, v_after, v_user
  );

  return true;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.snooze_finding(p_finding_id uuid, p_days integer default 7)
returns timestamp with time zone
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user   uuid := public.app_current_user_id();
  v_org    uuid := public.app_current_org_id();
  v_days   integer := greatest(1, least(coalesce(p_days, 7), 365));
  v_until  timestamptz;
  v_before jsonb;
  v_after  jsonb;
begin
  if v_user is null then
    raise exception 'snooze_finding: not authenticated';
  end if;

  select to_jsonb(f.*) into v_before
  from public.findings f
  where f.id = p_finding_id and f.org_id = v_org;

  update public.findings
    set status = 'snoozed',
        snoozed_until = now() + make_interval(days => v_days)
  where id = p_finding_id
    and org_id = v_org
  returning snoozed_until into v_until;

  if v_until is null then
    return null;  -- unknown finding, or wrong org
  end if;

  select to_jsonb(f.*) into v_after
  from public.findings f
  where f.id = p_finding_id;

  -- Every deferral is recorded, including a deferral of an already-deferred
  -- finding. See the header: that is a second decision, not a repeat.
  perform public.record_audit_log(
    v_org, v_user, p_finding_id, 'snooze_finding',
    'findings', p_finding_id, v_before, v_after, v_user
  );

  return v_until;
end;
$function$;
-- +goose StatementEnd

-- +goose Down

-- Restores the 00002 bodies verbatim: no audit write, and the original
-- target_id lookup. Down is a real rollback rather than a comment saying it is
-- unsupported, because this migration only replaces function bodies and there
-- is no data change to reverse.

-- +goose StatementBegin
create or replace function public.approve_finding(p_finding_id uuid, p_reviewed boolean default false)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_updated uuid;
  v_target  uuid;
begin
  if v_user is null then
    raise exception 'approve_finding: not authenticated';
  end if;

  update public.findings
    set status = 'approved',
        approved_by = v_user,
        approval_reviewed = p_reviewed
    where id = p_finding_id
      and org_id = public.app_current_org_id()
      and status <> 'approved'
    returning id into v_updated;

  if v_updated is null then
    return null;
  end if;

  select target_id into v_target
  from public.audit_log
  where finding_id = p_finding_id
  order by occurred_at desc
  limit 1;

  return v_target;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.reject_finding(p_finding_id uuid, p_reason text default null)
returns boolean
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_org     uuid := public.app_current_org_id();
  v_updated uuid;
  v_profile uuid;
  v_slug    text;
  v_count   int;
  c_threshold constant int := 3;
begin
  if v_user is null then
    raise exception 'reject_finding: not authenticated';
  end if;

  update public.findings
    set status = 'rejected',
        rejection_reason = nullif(btrim(p_reason), ''),
        snoozed_until = null
  where id = p_finding_id
    and org_id = v_org
    and status <> 'rejected'
  returning id, profile_id, obligation_slug
    into v_updated, v_profile, v_slug;

  if v_updated is not null and v_slug is not null then
    select count(*)
      into v_count
      from public.findings
     where profile_id = v_profile
       and obligation_slug = v_slug
       and status = 'rejected';

    if v_count >= c_threshold then
      insert into public.product_review_flags (
        org_id, created_by, profile_id, obligation_slug, finding_id, rejection_count, reasons
      )
      values (
        v_org,
        v_user,
        v_profile,
        v_slug,
        v_updated,
        v_count,
        (
          select array_remove(array_agg(distinct rejection_reason), null)
            from public.findings
           where profile_id = v_profile
             and obligation_slug = v_slug
             and status = 'rejected'
        )
      )
      on conflict (profile_id, obligation_slug) do nothing;
    end if;
  end if;

  return v_updated is not null;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.snooze_finding(p_finding_id uuid, p_days integer default 7)
returns timestamp with time zone
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user  uuid := public.app_current_user_id();
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
    and org_id = public.app_current_org_id()
  returning snoozed_until into v_until;

  return v_until;
end;
$function$;
-- +goose StatementEnd
