-- AI Systems Register: manual add + edit with reviewed reclassification & audit (ENT-72)
--
-- The founder's view/edit surface for the AI Systems Register (epic ENT-35; PRD
-- §7.4, §10, §14 Q2). The register reads `ai_systems` directly under RLS; its
-- two writes go through SECURITY DEFINER RPCs so each leaves an audit-log entry
-- atomically with the change, the actor is auth.uid() (never a trusted param),
-- and writes are scoped to the caller's own rows.
--
--   * create_ai_system_manual — the manual "Add system" path, important for the
--     shadow-AI follow-up (PRD §14 Q2). Finding-less; records a
--     'create_ai_system_manual' audit entry. A High-Risk classification still
--     demands a reviewed approval, consistent with the Executor path (ENT-68).
--   * update_ai_system — inline edit. Per the AC, a *classification change*
--     requires a reviewed approval (PRD §10) — it carries the Annex III deployer
--     obligations — so a class change without review is rejected; it also stamps
--     last_reviewed_at and records a 'reclassify_ai_system' audit entry. Plain
--     field edits record 'update_ai_system'. A no-op save records nothing.
--
-- The `ai_systems` table and its RLS already exist (Executor path, ENT-68). This
-- migration adds no columns — only the two write RPCs.
--
-- Idempotent: `create or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- create_ai_system_manual — manual "Add system".
create or replace function public.create_ai_system_manual(
  p_name                 text,
  p_vendor               text,
  p_purpose              text,
  p_risk_classification  text,
  p_documentation_status text,
  p_reviewed             boolean default false
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user    uuid := auth.uid();
  v_profile uuid;
  v_class   text := coalesce(nullif(btrim(p_risk_classification), ''), 'unclassified');
  v_id      uuid;
  v_after   jsonb;
begin
  if v_user is null then
    raise exception 'create_ai_system_manual: not authenticated';
  end if;

  select id into v_profile
  from public.compliance_profiles
  where user_id = v_user
  order by created_at desc
  limit 1;
  if v_profile is null then
    raise exception 'create_ai_system_manual: no compliance profile for user';
  end if;

  -- A High-Risk classification needs a reviewed approval (consistent with ENT-68).
  if v_class = 'high' and not p_reviewed then
    raise exception 'create_ai_system_manual: a High-Risk classification requires a reviewed approval'
      using errcode = 'check_violation';
  end if;

  insert into public.ai_systems (
    profile_id, user_id, finding_id,
    name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
  )
  values (
    v_profile, v_user, null,
    coalesce(nullif(btrim(p_name), ''), 'Untitled system'),
    p_vendor, p_purpose, v_class,
    coalesce(nullif(btrim(p_documentation_status), ''), 'missing'),
    now()
  )
  returning id into v_id;

  select to_jsonb(a.*) into v_after from public.ai_systems a where a.id = v_id;

  perform public.record_audit_log(
    v_user, null, 'create_ai_system_manual', 'ai_systems', v_id, null, v_after, v_user
  );

  return v_id;
end;
$$;

-- update_ai_system — inline edit. A classification change requires a reviewed
-- approval; any real change records an audit entry.
create or replace function public.update_ai_system(
  p_id                   uuid,
  p_name                 text,
  p_vendor               text,
  p_purpose              text,
  p_risk_classification  text,
  p_documentation_status text,
  p_reviewed             boolean default false
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user      uuid := auth.uid();
  v_before    jsonb;
  v_after     jsonb;
  v_old_class text;
  v_new_class text;
  v_reclass   boolean;
begin
  if v_user is null then
    raise exception 'update_ai_system: not authenticated';
  end if;

  select to_jsonb(a.*) into v_before
  from public.ai_systems a
  where a.id = p_id and a.user_id = v_user;
  if v_before is null then
    raise exception 'update_ai_system: system not found or not owned';
  end if;

  v_old_class := v_before ->> 'risk_classification';
  -- A null/blank class on input means "leave the classification unchanged".
  v_new_class := coalesce(nullif(btrim(p_risk_classification), ''), v_old_class);
  v_reclass := v_new_class is distinct from v_old_class;

  -- A classification change is a reviewed approval (PRD §10).
  if v_reclass and not p_reviewed then
    raise exception 'update_ai_system: a classification change requires a reviewed approval'
      using errcode = 'check_violation';
  end if;

  update public.ai_systems set
    name                 = coalesce(nullif(btrim(p_name), ''), name),
    vendor               = p_vendor,
    purpose              = p_purpose,
    risk_classification  = v_new_class,
    documentation_status = coalesce(nullif(btrim(p_documentation_status), ''), documentation_status),
    -- Stamp the review time whenever the classification is (re)confirmed.
    last_reviewed_at     = case when v_reclass then now() else last_reviewed_at end
  where id = p_id and user_id = v_user;

  select to_jsonb(a.*) into v_after from public.ai_systems a where a.id = p_id;

  -- Audit only a real change (ignore the trigger's updated_at bump).
  if (v_before - 'updated_at') is distinct from (v_after - 'updated_at') then
    perform public.record_audit_log(
      v_user,
      (v_after ->> 'finding_id')::uuid,
      case when v_reclass then 'reclassify_ai_system' else 'update_ai_system' end,
      'ai_systems', p_id, v_before, v_after, v_user
    );
  end if;

  return p_id;
end;
$$;
