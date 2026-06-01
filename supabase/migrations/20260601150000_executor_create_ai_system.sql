-- The Executor: create an AI Systems Register entry on approval (ENT-68)
--
-- The Executor's third write path (epic ENT-35; PRD §5.4, §7.4, §10). When a
-- founder approves an AI-system finding, an `ai_systems` row is created so the
-- EU AI Act risk classification and the deployer's per-system obligations are
-- tracked, and the act is recorded in the immutable audit log (ENT-69).
--
-- Reuses the ENT-66 machinery: the `findings.action_type` discriminator (the
-- 'create_ai_system' value was already declared in its check constraint),
-- `approve_finding()` as the entry point, and an AFTER UPDATE trigger gated on
-- the pending→approved transition — the same shape as the ROPA and DSAR paths.
--
-- The one new wrinkle is the AC's "Reviewed approval" gate (PRD §10): a High-Risk
-- classification carries heavy deployer obligations, so ratifying one must be an
-- explicit, deliberate act — not a one-click approve. Modelled as:
--
--   * `findings.approval_reviewed` — whether the approval was a *reviewed*
--     confirmation, set by `approve_finding`'s new (defaulted) p_reviewed arg.
--   * The trigger raises when the proposed class is 'high' and the approval was
--     not reviewed, which rolls the transition back (the finding stays pending).
--     The founder then re-approves with the reviewed confirmation, which ratifies
--     the row. Non-high-risk classes ratify on a plain approval, unchanged.
--
-- Extending `approve_finding` with a defaulted parameter keeps it backward
-- compatible: the ENT-66/67 two-argument calls still resolve and still create
-- their records exactly as before (approval_reviewed defaults to false, which
-- only ever matters for the high-risk AI-system path).
--
-- Scope boundary — ENT-72 builds the AI Systems Register view/edit UI, the manual
-- "Add system" path (shadow-AI follow-up), and the inline reclassification flow
-- (which reuses this same reviewed-approval requirement) on top of this table.
--
-- Idempotent: `add column if not exists` / `create or replace` / `drop … if
-- exists`, and re-approving a finding never creates a second system.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. AI Systems Register ───────────────────────────────────────────────────────
--
-- User-owned (RLS convention). One row = one AI system the deployer uses.
--
--   * `finding_id`           — provenance back to the approved finding; soft
--                              reference (no FK), unique when present — the
--                              idempotency pivot, as on processing_activities.
--   * `risk_classification`  — EU AI Act tier proposed by the Analyst. Defaults
--                              to 'unclassified' so a row is never silently
--                              mis-tiered; 'high' demands a reviewed approval.
--   * `documentation_status` — whether the technical documentation / deployer
--                              records exist. Defaults to 'missing'.
--   * `last_reviewed_at`     — when the classification was last ratified by a
--                              human; set to now() at creation (the approval is
--                              that review). Surfaced by the register (ENT-72).

create table if not exists public.ai_systems (
  id                   uuid        primary key default gen_random_uuid(),
  profile_id           uuid        not null
                         references public.compliance_profiles(id) on delete cascade,
  user_id              uuid        not null references auth.users(id) on delete cascade,
  finding_id           uuid,
  name                 text        not null,
  vendor               text,
  purpose              text,
  risk_classification  text        not null default 'unclassified'
                         check (risk_classification in
                           ('unacceptable', 'high', 'limited', 'minimal', 'unclassified')),
  documentation_status text        not null default 'missing'
                         check (documentation_status in ('missing', 'in_progress', 'complete')),
  last_reviewed_at     timestamptz,
  created_at           timestamptz not null default now(),
  updated_at           timestamptz not null default now()
);

create unique index if not exists ai_systems_finding_idx
  on public.ai_systems (finding_id)
  where finding_id is not null;

create index if not exists ai_systems_profile_idx
  on public.ai_systems (profile_id);

drop trigger if exists set_updated_at on public.ai_systems;
create trigger set_updated_at
  before update on public.ai_systems
  for each row execute function public.set_updated_at();

alter table public.ai_systems enable row level security;

-- Full owner CRUD per the baseline convention; ENT-72 layers its view/edit UI on
-- these. Executor writes go through the SECURITY DEFINER trigger (unaffected by RLS).
drop policy if exists "ai_systems_select_own" on public.ai_systems;
create policy "ai_systems_select_own" on public.ai_systems
  for select using (auth.uid() = user_id);

drop policy if exists "ai_systems_insert_own" on public.ai_systems;
create policy "ai_systems_insert_own" on public.ai_systems
  for insert with check (auth.uid() = user_id);

drop policy if exists "ai_systems_update_own" on public.ai_systems;
create policy "ai_systems_update_own" on public.ai_systems
  for update using (auth.uid() = user_id) with check (auth.uid() = user_id);

drop policy if exists "ai_systems_delete_own" on public.ai_systems;
create policy "ai_systems_delete_own" on public.ai_systems
  for delete using (auth.uid() = user_id);

-- 2. Reviewed-approval attribution on findings ─────────────────────────────────
--
-- Whether the approval was a deliberate, reviewed confirmation. Default false:
-- the High-Risk gate is the only consumer, so every existing finding and every
-- plain approval is unaffected.

alter table public.findings
  add column if not exists approval_reviewed boolean not null default false;

-- 3. approve_finding() — now carrying the reviewed flag ────────────────────────
--
-- Re-declared with a third, defaulted argument so the ENT-66/67 two-argument
-- callers keep working unchanged. p_reviewed records a reviewed approval, which
-- the High-Risk AI-system gate requires. Drop the old 2-arg signature first so
-- there is a single, unambiguous entry point.

drop function if exists public.approve_finding(uuid, uuid);

create or replace function public.approve_finding(
  p_finding_id        uuid,
  p_approving_user_id uuid,
  p_reviewed          boolean default false
)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_updated uuid;
  v_target  uuid;
begin
  update public.findings
    set status = 'approved',
        approved_by = p_approving_user_id,
        approval_reviewed = p_reviewed
    where id = p_finding_id
      and status <> 'approved'
    returning id into v_updated;

  if v_updated is null then
    return null;  -- unknown finding, or already approved
  end if;

  -- The created record's id, for "take the founder to the new row". Generic
  -- across every Executor action: the trigger always records target_id.
  select target_id into v_target
  from public.audit_log
  where finding_id = p_finding_id
  order by occurred_at desc
  limit 1;

  return v_target;
end;
$$;

-- 4. Executor reaction ─────────────────────────────────────────────────────────
--
-- Fires on the pending→approved transition of a create_ai_system finding.
-- Enforces the reviewed-approval gate for a High-Risk proposal, then creates the
-- ai_systems row from the Analyst payload and records the write in the audit log.

create or replace function public.executor_create_ai_system_on_approval()
returns trigger
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_class    text  := coalesce(nullif(v_payload ->> 'risk_classification', ''), 'unclassified');
  v_id       uuid;
  v_after    jsonb;
  v_approver uuid := coalesce(new.approved_by, new.user_id);
begin
  -- One system per finding. A repeat approval transition is a no-op.
  if exists (select 1 from public.ai_systems where finding_id = new.id) then
    return new;
  end if;

  -- Reviewed-approval gate (PRD §10): a High-Risk classification cannot be
  -- ratified by a plain approval. Raising rolls the transition back, leaving the
  -- finding pending until the founder confirms via a reviewed approval.
  if v_class = 'high' and not coalesce(new.approval_reviewed, false) then
    raise exception
      'finding %: a High-Risk AI system classification requires a reviewed approval', new.id
      using errcode = 'check_violation';
  end if;

  insert into public.ai_systems (
    profile_id, user_id, finding_id,
    name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
  )
  values (
    new.profile_id,
    new.user_id,
    new.id,
    coalesce(nullif(btrim(v_payload ->> 'name'), ''), new.detected),
    v_payload ->> 'vendor',
    v_payload ->> 'purpose',
    v_class,
    coalesce(nullif(v_payload ->> 'documentation_status', ''), 'missing'),
    now()  -- the approval is the human review of the classification
  )
  returning id into v_id;

  select to_jsonb(a.*) into v_after
  from public.ai_systems a
  where a.id = v_id;

  perform public.record_audit_log(
    new.user_id,        -- owner the entry belongs to
    new.id,             -- finding id
    'create_ai_system', -- action type
    'ai_systems',       -- target table
    v_id,               -- target id
    null,               -- before (a create has no prior state)
    v_after,            -- after (whole new row; carries profile id + classification)
    v_approver          -- approving user
  );

  return new;
end;
$$;

drop trigger if exists executor_create_ai_system on public.findings;
create trigger executor_create_ai_system
  after update of status on public.findings
  for each row
  when (
    new.status = 'approved'
    and old.status is distinct from 'approved'
    and new.action_type = 'create_ai_system'
  )
  execute function public.executor_create_ai_system_on_approval();
