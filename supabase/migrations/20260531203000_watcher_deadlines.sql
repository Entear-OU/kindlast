-- Watcher deadline detector + DSAR store (ENT-55)
--
-- The first detector to register into the ENT-53 Watcher loop. Flags two
-- kinds of "approaching deadline" so the user is warned with time to act:
--
--   1. Obligations whose `effective_date` falls within the next 30 days,
--      restricted to the profile's *applicable* obligations.
--   2. DSARs whose `response_due_at` falls within 30 days with no logged
--      response.
--
-- Each finding carries `days_remaining` (in metadata) and, where it maps to
-- the catalogue, the obligation reference (`obligation_slug`). Re-emission
-- suppression is inherited wholesale from ENT-53: the partial unique index on
-- open findings means re-running the detector every day refreshes the live
-- finding in place rather than duplicating it.
--
-- Idempotent: `if not exists` / `or replace` / `drop … if exists` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. DSAR store ───────────────────────────────────────────────────────────────
--
-- Data-subject access requests are user-owned operational records (not
-- reference data), so they follow the baseline RLS convention: `user_id`
-- references auth.users and four per-operation policies scope to auth.uid().
--
--   * `response_due_at` is the GDPR Article 12(3) clock the Watcher watches
--     (one month from receipt; the application sets it on intake).
--   * `responded_at` is the "logged response" — NULL means no response yet.
--     ENT-55 uses NULL as the only response gate; ENT-57 layers the <10-day
--     Critical escalation and the open/in_progress status iteration on top.
--   * `status` is a small closed set; the Watcher treats open/in_progress as
--     "still owed a response".

create table if not exists public.dsars (
  id              uuid        primary key default gen_random_uuid(),
  user_id         uuid        not null references auth.users(id) on delete cascade,
  subject_name    text,
  request_type    text,
  status          text        not null default 'open'
                    check (status in ('open', 'in_progress', 'responded', 'closed')),
  received_at     timestamptz not null default now(),
  response_due_at timestamptz not null,
  responded_at    timestamptz,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);

create index if not exists dsars_user_idx on public.dsars (user_id);
create index if not exists dsars_due_idx on public.dsars (response_due_at)
  where status in ('open', 'in_progress');

drop trigger if exists set_updated_at on public.dsars;
create trigger set_updated_at
  before update on public.dsars
  for each row execute function public.set_updated_at();

alter table public.dsars enable row level security;

drop policy if exists "dsars_select_own" on public.dsars;
create policy "dsars_select_own" on public.dsars
  for select using (auth.uid() = user_id);

drop policy if exists "dsars_insert_own" on public.dsars;
create policy "dsars_insert_own" on public.dsars
  for insert with check (auth.uid() = user_id);

drop policy if exists "dsars_update_own" on public.dsars;
create policy "dsars_update_own" on public.dsars
  for update using (auth.uid() = user_id)
             with check (auth.uid() = user_id);

drop policy if exists "dsars_delete_own" on public.dsars;
create policy "dsars_delete_own" on public.dsars
  for delete using (auth.uid() = user_id);

-- 2. Applicability predicate ──────────────────────────────────────────────────
--
-- Shared with ENT-56 (gap detection): "does this obligation apply to this
-- profile?". Evaluates the `applies_when` predicates that map onto the
-- ENT-45 profile columns and is deliberately *conservative* about the rest —
-- a near-term deadline is a safety warning, so an indeterminate predicate
-- (one we can't decide from the profile) leaves the obligation applicable
-- (warn-by-default) rather than silently dropping a real deadline.
--
-- Mappings (all keys optional; an empty applies_when applies to everyone):
--   * role: 'controller'            → always true (the product targets SME controllers)
--           'deployer' | 'provider' → the profile lists at least one AI system
--   * thresholds.cross_border_transfers → transfers_outside_eu = 'yes'
--   * thresholds.employees_min          → staff_count >= N (NULL staff_count ⇒ applicable)
--   * engages_processor                 → vendor_list is non-empty
--   * any other key (high_risk, large_scale_monitoring, lawful_basis_includes,
--     processing_includes, …)           → not evaluated ⇒ applicable
--
-- ENT-56 owns tightening the indeterminate predicates as the profile schema
-- grows the fields they need.

create or replace function public.watcher_obligation_applies(
  p_applies_when jsonb,
  p_profile      public.compliance_profiles
)
returns boolean
language plpgsql
immutable
set search_path = public, pg_temp
as $$
declare
  v_role  text  := p_applies_when ->> 'role';
  v_min   int   := (p_applies_when #>> '{thresholds,employees_min}')::int;
begin
  -- role
  if v_role in ('deployer', 'provider') then
    if coalesce(array_length(p_profile.ai_systems, 1), 0) = 0 then
      return false;
    end if;
  end if;
  -- 'controller' (and absent role) impose no role restriction.

  -- cross-border transfers
  if coalesce((p_applies_when #>> '{thresholds,cross_border_transfers}')::boolean, false) then
    if p_profile.transfers_outside_eu is distinct from 'yes' then
      return false;
    end if;
  end if;

  -- employee threshold (NULL staff_count is treated as "unknown ⇒ applicable")
  if v_min is not null and p_profile.staff_count is not null
     and p_profile.staff_count < v_min then
    return false;
  end if;

  -- engages a processor
  if coalesce((p_applies_when ->> 'engages_processor')::boolean, false) then
    if coalesce(btrim(p_profile.vendor_list), '') = '' then
      return false;
    end if;
  end if;

  return true;
end;
$$;

-- 3. Deadline detector ────────────────────────────────────────────────────────
--
-- Emits one finding per approaching deadline. Window is 30 days. Obligations
-- already in force (effective_date in the past) are not "approaching"; DSARs
-- already responded to are not owed anything.
--
-- DSAR findings reference Articles 12–22 (data-subject rights) as their
-- obligation anchor and stash the dsar id in metadata so a UI / the Analyst
-- can deep-link the underlying request.

create or replace function public.watcher_detect_deadlines(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_profile public.compliance_profiles;
  v_user_id uuid;
  o         record;
  d         record;
  v_days    int;
begin
  select * into v_profile from public.compliance_profiles where id = p_profile_id;
  if not found then
    return;
  end if;
  v_user_id := v_profile.user_id;

  -- Obligations coming into force within the window, applicable to this profile.
  for o in
    select slug, title, severity, effective_date
    from public.obligations
    where effective_date is not null
      and effective_date >= current_date
      and effective_date <= current_date + 30
  loop
    if not public.watcher_obligation_applies(
         (select applies_when from public.obligations where slug = o.slug), v_profile) then
      continue;
    end if;

    v_days := o.effective_date - current_date;
    perform public.emit_watcher_finding(
      p_profile_id,
      'deadline',
      'deadline:obligation:' || o.slug,
      o.title || ' takes effect in ' || v_days || ' day' || case when v_days = 1 then '' else 's' end,
      'This obligation''s effective date (' || o.effective_date || ') is within 30 days.',
      o.severity,
      o.slug,
      jsonb_build_object('days_remaining', v_days, 'effective_date', o.effective_date)
    );
  end loop;

  -- DSARs owed a response within the window.
  for d in
    select id, subject_name, response_due_at
    from public.dsars
    where user_id = v_user_id
      and status in ('open', 'in_progress')
      and responded_at is null
      and response_due_at <= now() + interval '30 days'
  loop
    v_days := (d.response_due_at::date - current_date);
    perform public.emit_watcher_finding(
      p_profile_id,
      'dsar',
      'dsar:' || d.id,
      'DSAR response due in ' || v_days || ' day' || case when v_days = 1 then '' else 's' end,
      'A data-subject request' ||
        case when d.subject_name is not null then ' from ' || d.subject_name else '' end ||
        ' has a response deadline within 30 days and no logged response.',
      'medium',
      'gdpr-arts-12-22-data-subject-rights',
      jsonb_build_object('days_remaining', v_days, 'dsar_id', d.id, 'response_due_at', d.response_due_at)
    );
  end loop;
end;
$$;

-- 4. Register the detector into the daily loop ────────────────────────────────
--
-- Re-declares ENT-53's per-profile entry point to invoke the deadline
-- detector before stamping the last-run timestamp. Subsequent detectors
-- (ENT-56, ENT-57) append their own `perform` calls here.

create or replace function public.run_watcher_for_profile(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
begin
  -- Detectors (each calls public.emit_watcher_finding):
  perform public.watcher_detect_deadlines(p_profile_id);  -- ENT-55
  -- ENT-56 profile gaps, ENT-57 DSAR escalation register here.

  update public.compliance_profiles
  set watcher_last_run_at = now()
  where id = p_profile_id;
end;
$$;
