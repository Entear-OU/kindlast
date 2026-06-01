-- ROPA manual-activity cap becomes plan-aware (ENT-84)
--
-- ENT-70 centralised the manual-activity cap in `ropa_manual_activity_limit()` as
-- a flat 3, with a note that billing would later make it plan-aware "without
-- touching the writers". Billing landed (ENT-81), so the limit now reads the
-- caller's subscription: Free stays capped at 3 manual activities, Pro is
-- uncapped (NULL = no limit).
--
-- The cap stays enforced inside `create_processing_activity` (SECURITY DEFINER),
-- so a direct PostgREST/API insert is capped too — not just the "Add activity"
-- button (AC: "direct API hits still cap at 3"). The writer now guards on a NULL
-- limit so the Pro path skips the cap entirely.
--
-- `ropa_manual_activity_limit()` keeps its no-arg signature, so nothing else that
-- references it has to change. It reads `auth.uid()` — which survives the
-- SECURITY DEFINER boundary (it's a request claim, not the function owner) — so
-- the same value is returned whether it's called directly or from the writer.
--
-- Idempotent: `create or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- Plan-aware cap. NULL = unlimited (Pro); Free is capped at 3 manual activities.
-- STABLE (reads a table) + SECURITY DEFINER so it can read `subscriptions`
-- regardless of the caller's RLS view.
create or replace function public.ropa_manual_activity_limit()
returns integer
language sql
stable
security definer
set search_path = public, pg_temp
as $$
  select case
    when exists (
      select 1 from public.subscriptions
      where user_id = auth.uid() and plan = 'pro'
    ) then null::integer
    else 3
  end
$$;

-- create_processing_activity — manual "Add activity".
-- Same as ENT-70 but the Free-tier cap now skips when the limit is NULL (Pro).
create or replace function public.create_processing_activity(
  p_name             text,
  p_purpose          text,
  p_legal_basis      text,
  p_data_categories  text[],
  p_recipients       text[],
  p_retention_period text
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user    uuid := auth.uid();
  v_profile uuid;
  v_limit   integer;
  v_manual  integer;
  v_id      uuid;
  v_after   jsonb;
begin
  if v_user is null then
    raise exception 'create_processing_activity: not authenticated';
  end if;

  -- The caller's current (most recent) compliance profile owns the activity.
  select id into v_profile
  from public.compliance_profiles
  where user_id = v_user
  order by created_at desc
  limit 1;
  if v_profile is null then
    raise exception 'create_processing_activity: no compliance profile for user';
  end if;

  -- Free-tier cap: manual activities only (Executor-ratified rows are unlimited).
  -- A NULL limit means Pro / uncapped — skip the count entirely.
  v_limit := public.ropa_manual_activity_limit();
  if v_limit is not null then
    select count(*) into v_manual
    from public.processing_activities
    where user_id = v_user and finding_id is null;
    if v_manual >= v_limit then
      raise exception 'free tier limit: a manual ROPA is capped at % activities', v_limit
        using errcode = 'check_violation';
    end if;
  end if;

  insert into public.processing_activities (
    profile_id, user_id, finding_id,
    name, purpose, legal_basis, data_categories, recipients, retention_period
  )
  values (
    v_profile, v_user, null,
    coalesce(nullif(btrim(p_name), ''), 'Untitled activity'),
    p_purpose, p_legal_basis,
    coalesce(p_data_categories, '{}'),
    coalesce(p_recipients, '{}'),
    p_retention_period
  )
  returning id into v_id;

  select to_jsonb(pa.*) into v_after from public.processing_activities pa where pa.id = v_id;

  perform public.record_audit_log(
    v_user, null, 'create_ropa_manual', 'processing_activities', v_id, null, v_after, v_user
  );

  return v_id;
end;
$$;
