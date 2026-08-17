-- +goose Up
-- 00012_log_dsar_receipt_date.sql (ENT-224, the half 00010 could not reach)
--
-- Let a human say when a request actually arrived, instead of stamping the day
-- they got round to logging it.
--
-- WHAT IS WRONG TODAY, AND IT IS THE SAME BUG 00010 FIXED
--
-- `log_dsar` writes `received_at = now()` and computes the deadline from it. So
-- a request that came by post on the 1st and was logged on the 8th is recorded
-- as due one month from the 8th.
--
-- Article 12(3) runs from receipt. A clock started at data entry silently grants
-- the organisation the days it took them to notice, which is exactly backwards:
-- the slower you are to log it, the longer you appear to have. 00010 fixed
-- precisely this on the executor path and could not fix it here, because the
-- fix is in this function's signature rather than in its body.
--
-- Found by building the console form: there was no date field to offer, because
-- the API had nowhere to put one.
--
-- THE SAME THREE REFUSALS AS 00010, DELIBERATELY
--
-- A future date is refused, because a request cannot have arrived tomorrow and
-- the likeliest cause is a typo that would quietly extend a statutory deadline.
-- An unparseable value is refused rather than coerced. Both raise rather than
-- fall back to `now()`, which would be the same silent generosity in a new
-- place.
--
-- The one difference from 00010: null is ALLOWED here and means "today". The
-- executor path reads a payload that must carry the date, so a missing one
-- there is a producer bug. Here a person logging a request that arrived this
-- morning has nothing to type, and forcing them to would produce a date field
-- everybody fills with today's date by reflex.
--
-- The 30-day interval stays hardcoded, and the Article 12(3) two-month
-- extension for complex requests stays unmodelled, both for the reasons 00010
-- set out. This migration changes when the clock starts, not how long it runs.
--
-- BACKWARD COMPATIBLE BY DEFAULT
--
-- `p_received_at` defaults to null, so every existing caller keeps working and
-- keeps getting today. Nothing in the schema needs a backfill: rows already
-- written carry whatever `now()` was at the time, and inventing a better date
-- for them after the fact would be fabricating a compliance record.

-- +goose StatementBegin
create or replace function public.log_dsar(
  p_subject_name text,
  p_request_type text,
  p_handler text,
  p_received_at timestamptz default null
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user     uuid := public.app_current_user_id();
  v_org      uuid := public.app_current_org_id();
  v_received timestamptz;
  v_id       uuid;
  v_after    jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'log_dsar: not authenticated';
  end if;

  -- Null means today, which is the common case and not a missing value.
  v_received := coalesce(p_received_at, now());

  -- A request cannot have arrived in the future. Refused rather than clamped:
  -- clamping would accept a typo and record a deadline nobody chose.
  if v_received > now() then
    raise exception
      'log_dsar: received_at % is in the future; a request cannot have arrived yet',
      v_received
      using errcode = 'check_violation';
  end if;

  insert into public.dsars (
    org_id, created_by, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    v_org, v_user, null,
    nullif(btrim(p_subject_name), ''),
    nullif(btrim(p_request_type), ''),
    nullif(btrim(p_handler), ''),
    'open', v_received, v_received + interval '30 days'
  )
  returning id into v_id;

  select to_jsonb(d.*) into v_after from public.dsars d where d.id = v_id;

  perform public.record_audit_log(
    v_org, v_user, null, 'create_dsar_manual', 'dsars', v_id, null, v_after, v_user
  );

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- The three-argument form is gone, replaced by one with a defaulted fourth.
-- Postgres would otherwise keep both as overloads, and a caller passing three
-- arguments would bind to whichever the resolver preferred: the old one, with
-- the bug still in it.
drop function if exists public.log_dsar(text, text, text);

-- +goose Down

-- +goose StatementBegin
create or replace function public.log_dsar(
  p_subject_name text,
  p_request_type text,
  p_handler text
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user  uuid := public.app_current_user_id();
  v_org   uuid := public.app_current_org_id();
  v_id    uuid;
  v_after jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'log_dsar: not authenticated';
  end if;

  insert into public.dsars (
    org_id, created_by, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    v_org, v_user, null,
    nullif(btrim(p_subject_name), ''),
    nullif(btrim(p_request_type), ''),
    nullif(btrim(p_handler), ''),
    'open', now(), now() + interval '30 days'
  )
  returning id into v_id;

  select to_jsonb(d.*) into v_after from public.dsars d where d.id = v_id;

  perform public.record_audit_log(
    v_org, v_user, null, 'create_dsar_manual', 'dsars', v_id, null, v_after, v_user
  );

  return v_id;
end;
$function$;
-- +goose StatementEnd

drop function if exists public.log_dsar(text, text, text, timestamptz);
