-- DSAR Log: manual log + mark-responded with reviewed approval & audit (ENT-71)
--
-- The founder's view/edit surface for the DSAR Log (epic ENT-35; PRD §7.4, §10).
-- The log reads `dsars` directly under RLS; its two writes go through SECURITY
-- DEFINER RPCs so each leaves an audit-log entry atomically with the change and
-- the actor is auth.uid() (never a trusted parameter), scoped to the caller's
-- own rows.
--
--   * log_dsar — the manual "Log a DSAR I received offline" path. Opens an
--     `open` DSAR with a 30-day Article 12(3) countdown, finding-less (manual),
--     and records a 'create_dsar_manual' audit entry. Available on the Free tier.
--   * mark_dsar_responded — closes the loop on a request. This is an Executor
--     write, so it carries the PRD §10 "Reviewed approval" requirement: the call
--     must be an explicit, reviewed confirmation (p_reviewed) or it is rejected.
--     Records a 'mark_dsar_responded' audit entry with before/after.
--
-- The Free/Pro split on *completing* a DSAR (AC: "Free users can log DSARs but
-- cannot mark them complete without Pro") is a billing capability the product
-- doesn't have yet — there is no plan store to gate on. It is enforced in the UI
-- (the register passes a `canComplete` capability) as a seam that becomes
-- plan-aware once billing lands; the database here enforces the concrete §10
-- requirement (a reviewed approval) and the audit trail.
--
-- The `dsars` table and its RLS already exist (Watcher deadline detectors, ENT-57;
-- handler/finding_id added by the Executor path, ENT-67). This migration adds no
-- columns — only the two write RPCs.
--
-- Idempotent: `create or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- log_dsar — manual "Log a DSAR".
create or replace function public.log_dsar(
  p_subject_name text,
  p_request_type text,
  p_handler      text
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user  uuid := auth.uid();
  v_id    uuid;
  v_after jsonb;
begin
  if v_user is null then
    raise exception 'log_dsar: not authenticated';
  end if;

  insert into public.dsars (
    user_id, finding_id, subject_name, request_type, handler,
    status, received_at, response_due_at
  )
  values (
    v_user, null,
    nullif(btrim(p_subject_name), ''),
    nullif(btrim(p_request_type), ''),
    nullif(btrim(p_handler), ''),
    'open', now(), now() + interval '30 days'
  )
  returning id into v_id;

  select to_jsonb(d.*) into v_after from public.dsars d where d.id = v_id;

  perform public.record_audit_log(
    v_user, null, 'create_dsar_manual', 'dsars', v_id, null, v_after, v_user
  );

  return v_id;
end;
$$;

-- mark_dsar_responded — record that a request has been answered.
-- Requires a reviewed approval (PRD §10): without p_reviewed the call is
-- rejected and nothing changes. No-ops (already responded/closed) don't
-- re-record. Returns the dsar id, or null when there was nothing open to close.
create or replace function public.mark_dsar_responded(
  p_id       uuid,
  p_reviewed boolean default false
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
    raise exception 'mark_dsar_responded: not authenticated';
  end if;

  if not p_reviewed then
    raise exception 'mark_dsar_responded: marking a DSAR responded requires a reviewed approval'
      using errcode = 'check_violation';
  end if;

  select to_jsonb(d.*) into v_before
  from public.dsars d
  where d.id = p_id and d.user_id = v_user;
  if v_before is null then
    raise exception 'mark_dsar_responded: DSAR not found or not owned';
  end if;

  -- Idempotent: only an unanswered request transitions.
  if (v_before ->> 'status') in ('responded', 'closed') then
    return p_id;
  end if;

  update public.dsars
    set status = 'responded',
        responded_at = now()
  where id = p_id and user_id = v_user;

  select to_jsonb(d.*) into v_after from public.dsars d where d.id = p_id;

  perform public.record_audit_log(
    v_user,
    (v_after ->> 'finding_id')::uuid,  -- links to the originating finding, if any
    'mark_dsar_responded', 'dsars', p_id, v_before, v_after, v_user
  );

  return p_id;
end;
$$;
