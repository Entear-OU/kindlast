-- +goose Up
-- 00010_dsar_clock_from_receipt.sql (ENT-224)
--
-- The statutory clock runs from receipt, not from approval.
--
-- WHAT WAS WRONG
--
-- executor_create_dsar_on_approval hardcoded
--
--   received_at     = now()
--   response_due_at = now() + interval '30 days'
--
-- so a request that arrived a week ago and was approved today got a deadline a
-- week later than the real one. Article 12(3) runs the period from receipt of
-- the request, and nothing in the payload could override it even though the
-- payload already carried requester, request_type and handler.
--
-- The direction of the error is what makes this worth fixing before anything
-- can reach it. The product would have told a customer they were comfortably
-- on time when they were nearly due, or already late. A compliance product that
-- under-reports urgency is worse than one that says nothing, because the
-- customer stops checking for themselves. Same family as ENT-161's green
-- posture before the Watcher had ever run: a confident answer that is wrong.
--
-- THE DECISION THIS MIGRATION MAKES, WHICH ENT-224 LEFT OPEN
--
-- A missing received_at is REFUSED rather than defaulted.
--
-- The alternative is to keep now() as a fallback, and it is tempting because it
-- never fails. But a DSAR whose receipt date is unknown has an unknowable
-- deadline, and now() does not represent that: it asserts a specific deadline
-- that is optimistic by however long the request sat unlogged. Silence would be
-- honest; an optimistic number is not, and it is the number a customer plans
-- around.
--
-- Refusing aborts the approval, which is a real cost and the reason to state it
-- plainly: the human sees an error rather than a created record. That is the
-- correct outcome. "We cannot log this request because we do not know when it
-- arrived" is actionable, where a deadline quietly computed from the wrong date
-- is not.
--
-- The cost is currently zero, which is why this is the moment to be strict.
-- Nothing maps to create_dsar (00009), so no approval reaches this path today,
-- and any future intake will be built against a trigger that already demands
-- the field rather than one that has to be tightened later. Reversible: making
-- the fallback return if this proves too strict is a smaller change than
-- discovering wrong deadlines in production.
--
-- WHAT IS NOT CHANGED
--
-- The 30-day interval stays hardcoded. Article 12(3) allows an extension of two
-- further months for complex requests, so this will eventually need somewhere
-- to record an extension and a reason. That is a schema question rather than a
-- trigger one, and folding it in here would mean designing the extension
-- surface inside a bug fix.

-- +goose StatementBegin
create or replace function public.executor_create_dsar_on_approval()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_dsar_id  uuid;
  v_after    jsonb;
  v_approver uuid := new.approved_by;
  v_received timestamptz;
begin
  -- One DSAR per finding. A second approval transition (or a concurrent fire)
  -- is a no-op rather than a duplicate.
  if exists (select 1 from public.dsars where finding_id = new.id) then
    return new;
  end if;

  -- The receipt date, from the payload. See the header: absent is refused
  -- rather than defaulted, because an unknown receipt date means an unknowable
  -- deadline and now() would assert an optimistic one.
  begin
    v_received := (v_payload ->> 'received_at')::timestamptz;
  exception when others then
    raise exception
      'executor_create_dsar: finding % carries a received_at that is not a '
      'timestamp (%). The statutory deadline runs from receipt, so it cannot '
      'be guessed.', new.id, v_payload ->> 'received_at';
  end;

  if v_received is null then
    raise exception
      'executor_create_dsar: finding % has no received_at in its payload. '
      'The Article 12(3) deadline runs from receipt of the request, and '
      'defaulting to now() would assert a deadline later than the real one.',
      new.id;
  end if;

  if v_received > now() then
    -- A future receipt date is data entry gone wrong, and it moves the deadline
    -- outwards, which is the direction that hides a breach.
    raise exception
      'executor_create_dsar: finding % has a received_at in the future (%).',
      new.id, v_received;
  end if;

  insert into public.dsars (
    org_id, created_by, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    new.org_id,
    v_approver,
    new.id,
    v_payload ->> 'requester',
    v_payload ->> 'request_type',
    v_payload ->> 'handler',
    'open',
    v_received,
    v_received + interval '30 days'
  )
  returning id into v_dsar_id;

  select to_jsonb(d.*) into v_after
  from public.dsars d
  where d.id = v_dsar_id;

  perform public.record_audit_log(
    new.org_id, v_approver, new.id, 'create_dsar',
    'dsars', v_dsar_id, null, v_after, v_approver
  );

  return new;
end;
$function$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
create or replace function public.executor_create_dsar_on_approval()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_dsar_id  uuid;
  v_after    jsonb;
  v_approver uuid := new.approved_by;
begin
  if exists (select 1 from public.dsars where finding_id = new.id) then
    return new;
  end if;

  insert into public.dsars (
    org_id, created_by, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    new.org_id,
    v_approver,
    new.id,
    v_payload ->> 'requester',
    v_payload ->> 'request_type',
    v_payload ->> 'handler',
    'open',
    now(),
    now() + interval '30 days'
  )
  returning id into v_dsar_id;

  select to_jsonb(d.*) into v_after
  from public.dsars d
  where d.id = v_dsar_id;

  perform public.record_audit_log(
    new.org_id, v_approver, new.id, 'create_dsar',
    'dsars', v_dsar_id, null, v_after, v_approver
  );

  return new;
end;
$function$;
-- +goose StatementEnd
