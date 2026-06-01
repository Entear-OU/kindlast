-- The Executor: create a ROPA entry on approval (ENT-66)
--
-- The Executor is the only agent that writes to compliance records, and it acts
-- only on explicit human approval (epic ENT-35; PRD §5.4, §7.4, §10). This is its
-- first write path: when a founder approves a ROPA-typed finding, one ratified
-- `processing_activities` row is created from the Analyst's pre-fill, and the act
-- is recorded in the immutable `audit_log` (ENT-69).
--
-- Architecture — SQL-first, consistent with the Watcher (ENT-53) and the Analyst
-- (ENT-58). The "Executor" reaction to an approval is a deterministic data
-- operation, so it lives in the database as a trigger on the approval transition
-- rather than in application code. That keeps the guarantee — *every* approval
-- of a create_ropa finding creates exactly one row and exactly one audit entry —
-- true regardless of which caller (the future feed UI, a server action, the
-- service role) performs the approval.
--
-- The pieces:
--   1. `processing_activities` — the ROPA register itself. The store both this
--      issue (the Executor writer) and ENT-70 (the founder's view/edit UI) need.
--   2. `findings.action_type` — the discriminator the Executor keys off, plus
--      `findings.approved_by` to attribute the human approval. The Analyst tags a
--      ROPA-gap finding with action_type='create_ropa' (its classification, epic
--      ENT-34); this migration adds the column + the default 'review' (no
--      Executor write) so existing findings are untouched.
--   3. `approve_finding()` — the explicit-approval entry point. Sets the
--      transition; the trigger does the side effects; returns the id of the
--      record the Executor created so the caller can take the founder to the new
--      row in edit mode (AC: "Founder is taken to the new row…").
--   4. The trigger + its function — fire only on pending→approved of a
--      create_ropa finding; pre-fill from the Analyst payload; write the audit row.
--
-- Scope boundary — ENT-67/68 add the DSAR and AI-systems write paths (their own
-- records, their own action_types, already listed in the check constraint so they
-- need no constraint change). ENT-70 builds the ROPA edit UI, the manual "Add
-- activity" path, the Free-tier cap, and audit-on-manual-edit on top of this table.
--
-- Idempotent: every statement uses `if not exists` / `or replace` /
-- `drop … if exists`, and re-approving a finding never creates a second row
-- (unique index on finding_id + an existence guard in the trigger).
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. ROPA register ─────────────────────────────────────────────────────────────
--
-- User-owned (RLS convention). One row = one processing activity (GDPR Art. 30).
--
--   * `finding_id`       — the approved finding that produced this row. Soft
--                          reference (plain uuid, no FK): provenance that must
--                          outlive the operational finding, which may later be
--                          purged. Unique (when present) so one finding ratifies
--                          at most one activity — the idempotency pivot.
--   * `name` / `purpose` — what the activity is and why data is processed.
--   * `legal_basis`      — GDPR Art. 6 basis. Free text: the founder amends it,
--                          and the catalogue of phrasings isn't worth a check.
--   * `data_categories`  — categories of personal data processed.
--   * `recipients`       — who the data is shared with.
--   * `retention_period` — how long it is kept.
--   * `updated_at`       — the "last_updated" the register surfaces (ENT-70).

create table if not exists public.processing_activities (
  id               uuid        primary key default gen_random_uuid(),
  profile_id       uuid        not null
                     references public.compliance_profiles(id) on delete cascade,
  user_id          uuid        not null references auth.users(id) on delete cascade,
  finding_id       uuid,
  name             text        not null,
  purpose          text,
  legal_basis      text,
  data_categories  text[]      not null default '{}',
  recipients       text[]      not null default '{}',
  retention_period text,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

-- One ratified activity per finding — the idempotency pivot for re-approval.
create unique index if not exists processing_activities_finding_idx
  on public.processing_activities (finding_id)
  where finding_id is not null;

create index if not exists processing_activities_profile_idx
  on public.processing_activities (profile_id);

drop trigger if exists set_updated_at on public.processing_activities;
create trigger set_updated_at
  before update on public.processing_activities
  for each row execute function public.set_updated_at();

alter table public.processing_activities enable row level security;

-- Full owner CRUD per the baseline convention: the founder reads the register,
-- and ENT-70 layers inline edit + manual add on these same policies. Executor
-- writes go through the SECURITY DEFINER trigger, which is unaffected by RLS.
drop policy if exists "processing_activities_select_own" on public.processing_activities;
create policy "processing_activities_select_own" on public.processing_activities
  for select using (auth.uid() = user_id);

drop policy if exists "processing_activities_insert_own" on public.processing_activities;
create policy "processing_activities_insert_own" on public.processing_activities
  for insert with check (auth.uid() = user_id);

drop policy if exists "processing_activities_update_own" on public.processing_activities;
create policy "processing_activities_update_own" on public.processing_activities
  for update using (auth.uid() = user_id) with check (auth.uid() = user_id);

drop policy if exists "processing_activities_delete_own" on public.processing_activities;
create policy "processing_activities_delete_own" on public.processing_activities
  for delete using (auth.uid() = user_id);

-- 2. Finding discriminator + approval attribution ──────────────────────────────
--
-- `action_type` tells the Executor which compliance record (if any) an approved
-- finding produces. Default 'review' = no Executor write, so every existing
-- finding is a no-op. The create_* values for ENT-67/68 are listed now so those
-- issues add behaviour without touching the constraint.

alter table public.findings
  add column if not exists action_type text not null default 'review';

alter table public.findings drop constraint if exists findings_action_type_check;
alter table public.findings
  add constraint findings_action_type_check
  check (action_type in ('review', 'create_ropa', 'create_dsar', 'create_ai_system'));

-- Who approved the finding — the human-in-the-loop attribution carried into the
-- audit log. ON DELETE SET NULL: the audit row already preserves the approver
-- id, so the finding can forget a deleted user without blocking the deletion.
alter table public.findings
  add column if not exists approved_by uuid references auth.users(id) on delete set null;

-- 3. Executor reaction ─────────────────────────────────────────────────────────
--
-- Fires on the pending→approved transition of a create_ropa finding. Pre-fills a
-- processing_activities row from the Analyst payload (findings.metadata->'payload'),
-- falling back to the finding's own text where a field is absent, then records the
-- write in the audit log. SECURITY DEFINER so it can write both tables while they
-- stay RLS-locked to direct callers. The audit `after` snapshot is the whole new
-- row — it carries profile_id, satisfying the "profile id" the AC asks the audit
-- entry to record without widening the ENT-69 audit schema.

-- jsonb array → text[] (empty array for a missing / non-array value). Shared with
-- the ENT-67/68 write paths.
create or replace function public.jsonb_text_array(p jsonb)
returns text[]
language sql
immutable
as $$
  select case
    when jsonb_typeof(p) = 'array' then array(select jsonb_array_elements_text(p))
    else '{}'::text[]
  end;
$$;

create or replace function public.executor_create_ropa_on_approval()
returns trigger
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_pa_id    uuid;
  v_after    jsonb;
  v_approver uuid := coalesce(new.approved_by, new.user_id);
begin
  -- One ratified activity per finding. A second approval transition (or a
  -- concurrent fire) is a no-op rather than a duplicate.
  if exists (select 1 from public.processing_activities where finding_id = new.id) then
    return new;
  end if;

  insert into public.processing_activities (
    profile_id, user_id, finding_id,
    name, purpose, legal_basis, data_categories, recipients, retention_period
  )
  values (
    new.profile_id,
    new.user_id,
    new.id,
    coalesce(nullif(btrim(v_payload ->> 'name'), ''), new.detected),
    v_payload ->> 'purpose',
    v_payload ->> 'legal_basis',
    public.jsonb_text_array(v_payload -> 'data_categories'),
    public.jsonb_text_array(v_payload -> 'recipients'),
    v_payload ->> 'retention_period'
  )
  returning id into v_pa_id;

  select to_jsonb(pa.*) into v_after
  from public.processing_activities pa
  where pa.id = v_pa_id;

  perform public.record_audit_log(
    new.user_id,              -- owner the entry belongs to
    new.id,                   -- finding id
    'create_ropa',            -- action type
    'processing_activities',  -- target table
    v_pa_id,                  -- target id
    null,                     -- before (a create has no prior state)
    v_after,                  -- after (whole new row; carries profile id)
    v_approver                -- approving user
  );

  return new;
end;
$$;

-- AFTER UPDATE OF status: the WHEN clause is the precise transition guard — new
-- status approved, previous status anything but approved, action_type create_ropa.
drop trigger if exists executor_create_ropa on public.findings;
create trigger executor_create_ropa
  after update of status on public.findings
  for each row
  when (
    new.status = 'approved'
    and old.status is distinct from 'approved'
    and new.action_type = 'create_ropa'
  )
  execute function public.executor_create_ropa_on_approval();

-- 4. approve_finding() ─────────────────────────────────────────────────────────
--
-- The explicit human-approval entry point (PRD §5.4: "only acts on explicit human
-- approval"). Records who approved, flips the finding to approved — which fires
-- the Executor trigger synchronously — and returns the id of the record the
-- Executor created (the most recent audit target for this finding), so the caller
-- can take the founder straight to the new row in edit mode. Returns null when the
-- finding was already approved or no record was produced. SECURITY DEFINER: the
-- approval path writes through here rather than needing a findings UPDATE policy.

create or replace function public.approve_finding(
  p_finding_id        uuid,
  p_approving_user_id uuid
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
        approved_by = p_approving_user_id
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
