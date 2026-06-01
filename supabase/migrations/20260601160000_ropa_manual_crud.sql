-- ROPA register: manual create + edit with audit (ENT-70)
--
-- The founder's view/edit surface for the Record of Processing Activities (epic
-- ENT-35; PRD §7.4, §10). The register reads `processing_activities` directly
-- under RLS, but its two *writes* — inline edit and manual "Add activity" — must
-- each leave an audit-log entry (AC: "every change writes an audit log entry"),
-- and the manual add is capped on the Free tier (AC: "Free tier capped at 3").
--
-- Both go through SECURITY DEFINER functions rather than raw table writes so the
-- before/after snapshot and the audit entry are captured atomically with the
-- change, on one round-trip. The functions derive the actor from auth.uid()
-- (never a trusted parameter) and scope every write to the caller's own rows, so
-- running as definer doesn't widen what a founder can touch beyond their RLS view.
--
-- The Executor's own write path (ENT-66) already audits its inserts; these are
-- the *manual* counterparts for the human-driven edits the register allows.
--
-- Free-tier cap — the limit counts only *manual* activities (finding_id is null).
-- Executor-ratified rows (finding_id set, one per approved finding) are the core
-- loop and stay unlimited. Centralised in `ropa_manual_activity_limit()` so the
-- cap becomes plan-aware in one place once billing exists.
--
-- Idempotent: `create or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- The Free-tier manual-activity cap. A standalone immutable function so the
-- limit lives in exactly one place (and a later billing migration can replace it
-- with a plan-aware lookup without touching the writers).
create or replace function public.ropa_manual_activity_limit()
returns integer
language sql
immutable
as $$
  select 3
$$;

-- create_processing_activity — manual "Add activity".
-- Resolves the caller's current profile, enforces the Free-tier cap on manual
-- rows, inserts a finding-less activity, and records a 'create_ropa_manual'
-- audit entry. Returns the new row id.
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
  select count(*) into v_manual
  from public.processing_activities
  where user_id = v_user and finding_id is null;
  if v_manual >= public.ropa_manual_activity_limit() then
    raise exception 'free tier limit: a manual ROPA is capped at % activities',
      public.ropa_manual_activity_limit()
      using errcode = 'check_violation';
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

-- update_processing_activity — inline edit of any field.
-- Snapshots the row, applies the edit (scoped to the caller's own row), and
-- records an 'update_ropa' audit entry — but only when something actually
-- changed, so a no-op save doesn't litter the evidence trail.
create or replace function public.update_processing_activity(
  p_id               uuid,
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
  v_user   uuid := auth.uid();
  v_before jsonb;
  v_after  jsonb;
begin
  if v_user is null then
    raise exception 'update_processing_activity: not authenticated';
  end if;

  select to_jsonb(pa.*) into v_before
  from public.processing_activities pa
  where pa.id = p_id and pa.user_id = v_user;
  if v_before is null then
    raise exception 'update_processing_activity: activity not found or not owned';
  end if;

  update public.processing_activities set
    name             = coalesce(nullif(btrim(p_name), ''), name),
    purpose          = p_purpose,
    legal_basis      = p_legal_basis,
    data_categories  = coalesce(p_data_categories, '{}'),
    recipients       = coalesce(p_recipients, '{}'),
    retention_period = p_retention_period
  where id = p_id and user_id = v_user;

  select to_jsonb(pa.*) into v_after
  from public.processing_activities pa
  where pa.id = p_id;

  -- Audit only a real change (ignore the trigger's updated_at bump).
  if (v_before - 'updated_at') is distinct from (v_after - 'updated_at') then
    perform public.record_audit_log(
      v_user,
      (v_after ->> 'finding_id')::uuid,  -- links to the originating finding, if any
      'update_ropa', 'processing_activities', p_id, v_before, v_after, v_user
    );
  end if;

  return p_id;
end;
$$;
