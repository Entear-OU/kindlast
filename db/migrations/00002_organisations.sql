-- +goose Up
-- 00002_organisations.sql (ENT-192)
--
-- The organisation tenancy model from core-api-surface §20.1, applied on top
-- of the auth-free legacy schema in 00001. This file is the security
-- boundary: every row level security policy in the database is defined here,
-- in the two-GUC form, and nowhere else. Review it policy by policy, not as
-- a diff.
--
-- What happens, in order:
--
--   1. `organisations` and `memberships` (owner / member / viewer).
--   2. Every tenant table gains `org_id`.
--   3. STAMPING: every user id found in the data gets a personal
--      organisation and an owner membership, and every row they own is
--      stamped with that org. Row counts are reconciled in-transaction and
--      the migration aborts on any mismatch. On a fresh database this is a
--      no-op; on a production import it is the tenancy cut-over.
--   4. `user_id` stops meaning two things. Where it was tenancy it is
--      dropped (findings, watcher_findings, subscriptions); where it was
--      authorship it becomes `created_by` (profiles, onboarding, records,
--      review flags); where the row is genuinely about a human it stays
--      (audit_log actor, notification recipients, preferences).
--   5. `org_id` goes NOT NULL, indexed first (e.g. (org_id, created_at
--      desc) on findings).
--   6. The business functions from the old stack, rewritten org-aware and
--      SECURITY INVOKER: they read the same two GUCs the policies read, so
--      RLS applies inside them. The old SECURITY DEFINER pattern existed to
--      let Supabase bypass RLS selectively; nothing here bypasses anything.
--   7. Business triggers (Executor, notification enqueue).
--   8. Every policy, in the two-GUC form; FORCE ROW LEVEL SECURITY on every
--      table in the schema.
--   9. Grants: kindlast_app gets DML and nothing else. RLS is the sole
--      gate, exactly the contract ENT-159 established. TRUNCATE is
--      deliberately not granted.
--
-- The two-GUC predicate (verbatim from the design doc):
--
--     org_id = (select current_setting('app.current_org_id')::uuid)
--     and exists (
--       select 1 from memberships m
--       where m.org_id  = (select current_setting('app.current_org_id')::uuid)
--         and m.user_id = (select current_setting('app.current_user_id')::uuid)
--     )
--
-- Both reads are wrapped in scalar subselects so the planner treats them as
-- constants (evaluated once per query, not per row). The `exists` is NOT
-- redundant with middleware: it is what keeps the database enforcing
-- isolation when middleware sets an org the caller does not belong to,
-- which is exactly the bug class RLS exists to survive.

------------------------------------------------------------------------------
-- 1. The organisation model
------------------------------------------------------------------------------

create table public.organisations (
  id          uuid primary key default gen_random_uuid(),
  name        text not null,
  created_at  timestamptz not null default now()
);

-- user_id is the IdP subject. No foreign key: users live in Zitadel, not
-- in this database. Memberships are domain data (they drive billing and the
-- audit trail), so they live here rather than in the IdP.
create table public.memberships (
  org_id      uuid not null references public.organisations(id) on delete cascade,
  user_id     uuid not null,
  role        text not null check (role in ('owner', 'member', 'viewer')),
  created_at  timestamptz not null default now(),
  primary key (org_id, user_id)
);

-- Reverse lookup: "which orgs does this user belong to" (the /me bootstrap).
create index memberships_user_idx on public.memberships (user_id);

------------------------------------------------------------------------------
-- 2. org_id on every tenant table
------------------------------------------------------------------------------

alter table public.ai_systems               add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.audit_log                add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.compliance_profiles      add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.deadline_alert_log       add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.dsars                    add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.findings                 add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.notification_outbox      add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.notification_preferences add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.onboarding_messages      add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.onboarding_sessions      add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.processing_activities    add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.product_review_flags     add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.subscriptions            add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.watcher_findings         add column org_id uuid references public.organisations(id) on delete cascade;
alter table public.weekly_briefing_log      add column org_id uuid references public.organisations(id) on delete cascade;

-- Roles change; a compliance record has to say what authority the approver
-- held at the time. Snapshotted by record_audit_log() from then on.
alter table public.audit_log add column actor_role text;

------------------------------------------------------------------------------
-- 3. Stamping: a personal organisation per existing user
------------------------------------------------------------------------------

-- Two append-only tables carry forbid-update triggers; stamping org_id onto
-- their existing rows is the one legitimate update they will ever see.
alter table public.audit_log            disable trigger audit_log_no_update;
alter table public.product_review_flags disable trigger product_review_flags_no_update;

-- Stamping is plain SQL inside one do-block: dynamic SQL would hide exactly
-- the statements a reviewer of the tenancy cut-over needs to see.
-- +goose StatementBegin
do $$
declare
  v_users integer;
  v_orgs  integer;
begin
  create temporary table org_map (
    user_id uuid primary key,
    org_id  uuid not null
  );

  insert into org_map (user_id, org_id)
  select u.user_id, gen_random_uuid()
  from (
    select user_id from public.ai_systems
    union select user_id from public.audit_log
    union select approving_user_id from public.audit_log
    union select user_id from public.compliance_profiles
    union select user_id from public.deadline_alert_log
    union select user_id from public.dsars
    union select user_id from public.findings
    union select user_id from public.notification_outbox
    union select user_id from public.notification_preferences
    union select user_id from public.onboarding_messages
    union select user_id from public.onboarding_sessions
    union select user_id from public.processing_activities
    union select user_id from public.product_review_flags
    union select user_id from public.subscriptions
    union select user_id from public.watcher_findings
    union select user_id from public.weekly_briefing_log
  ) u
  where u.user_id is not null;

  insert into public.organisations (id, name)
  select org_id, 'Personal organisation' from org_map;

  insert into public.memberships (org_id, user_id, role)
  select org_id, user_id, 'owner' from org_map;

  -- Stamp every row with its owner's personal organisation.
  update public.ai_systems t               set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.audit_log t                set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.compliance_profiles t      set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.deadline_alert_log t       set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.dsars t                    set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.findings t                 set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.notification_outbox t      set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.notification_preferences t set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.onboarding_messages t      set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.onboarding_sessions t      set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.processing_activities t    set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.product_review_flags t     set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.subscriptions t            set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.watcher_findings t         set org_id = m.org_id from org_map m where m.user_id = t.user_id;
  update public.weekly_briefing_log t      set org_id = m.org_id from org_map m where m.user_id = t.user_id;

  -- Every organisation needs a subscription for the plan gate to read; users
  -- that somehow lack one (pre-trigger accounts) get the free default.
  insert into public.subscriptions (org_id, user_id, plan, status)
  select m.org_id, m.user_id, 'free', 'active'
  from org_map m
  where not exists (select 1 from public.subscriptions s where s.org_id = m.org_id);

  -- Reconciliation: the migration is not allowed to guess. One org per user,
  -- one owner membership per org, zero unstamped rows anywhere.
  select count(*) into v_users from org_map;
  select count(*) into v_orgs from public.organisations;
  if v_orgs <> v_users then
    raise exception 'stamping reconciliation failed: % users but % organisations', v_users, v_orgs;
  end if;
  if (select count(*) from public.memberships where role = 'owner') <> v_users then
    raise exception 'stamping reconciliation failed: owner membership count does not match users';
  end if;

  drop table org_map;
end
$$;
-- +goose StatementEnd

alter table public.audit_log            enable trigger audit_log_no_update;
alter table public.product_review_flags enable trigger product_review_flags_no_update;

-- +goose StatementBegin
do $$
declare
  t text;
  n integer;
begin
  foreach t in array array[
    'ai_systems', 'audit_log', 'compliance_profiles', 'deadline_alert_log',
    'dsars', 'findings', 'notification_outbox', 'notification_preferences',
    'onboarding_messages', 'onboarding_sessions', 'processing_activities',
    'product_review_flags', 'subscriptions', 'watcher_findings',
    'weekly_briefing_log'
  ] loop
    execute format('select count(*) from public.%I where org_id is null', t) into n;
    if n > 0 then
      raise exception 'stamping reconciliation failed: % rows in % have no org_id', n, t;
    end if;
  end loop;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- 4. user_id stops meaning two things
------------------------------------------------------------------------------

-- Authored records: the record belongs to the organisation, the authorship
-- belongs to a human. Nullable, because humans leave; records stay.
alter table public.ai_systems            rename column user_id to created_by;
alter table public.ai_systems            alter column created_by drop not null;
alter table public.compliance_profiles   rename column user_id to created_by;
alter table public.compliance_profiles   alter column created_by drop not null;
alter table public.dsars                 rename column user_id to created_by;
alter table public.dsars                 alter column created_by drop not null;
alter table public.onboarding_messages   rename column user_id to created_by;
alter table public.onboarding_messages   alter column created_by drop not null;
alter table public.onboarding_sessions   rename column user_id to created_by;
alter table public.onboarding_sessions   alter column created_by drop not null;
alter table public.processing_activities rename column user_id to created_by;
alter table public.processing_activities alter column created_by drop not null;
alter table public.product_review_flags  rename column user_id to created_by;
alter table public.product_review_flags  alter column created_by drop not null;

-- Pure tenancy: the column was only ever "whose data is this", which org_id
-- now answers. Nothing human-shaped is lost: findings.approved_by remains,
-- and system-generated rows never had a human author.
alter table public.findings         drop column user_id;
alter table public.watcher_findings drop column user_id;

-- Billing moves from user to organisation (one company, one subscription).
alter table public.subscriptions drop column user_id;
alter table public.subscriptions add constraint subscriptions_org_key unique (org_id);

-- Notification recipients stay humans; delivery resolution moves to the
-- Comms path, so the enqueue no longer guesses a recipient up front.
alter table public.notification_outbox alter column user_id drop not null;

-- Preferences are personal within an organisation.
alter table public.notification_preferences drop constraint notification_preferences_pkey;
alter table public.notification_preferences add primary key (org_id, user_id);

------------------------------------------------------------------------------
-- 5. org_id NOT NULL, indexed first
------------------------------------------------------------------------------

alter table public.ai_systems               alter column org_id set not null;
alter table public.audit_log                alter column org_id set not null;
alter table public.compliance_profiles      alter column org_id set not null;
alter table public.deadline_alert_log       alter column org_id set not null;
alter table public.dsars                    alter column org_id set not null;
alter table public.findings                 alter column org_id set not null;
alter table public.notification_outbox      alter column org_id set not null;
alter table public.notification_preferences alter column org_id set not null;
alter table public.onboarding_messages      alter column org_id set not null;
alter table public.onboarding_sessions      alter column org_id set not null;
alter table public.processing_activities    alter column org_id set not null;
alter table public.product_review_flags     alter column org_id set not null;
alter table public.subscriptions            alter column org_id set not null;
alter table public.watcher_findings         alter column org_id set not null;
alter table public.weekly_briefing_log      alter column org_id set not null;

-- The feed wants (org_id, created_at desc), not created_at alone (§20.1).
create index findings_org_created_idx      on public.findings (org_id, created_at desc);
create index watcher_findings_org_idx      on public.watcher_findings (org_id);
create index ai_systems_org_idx            on public.ai_systems (org_id);
create index audit_log_org_occurred_idx    on public.audit_log (org_id, occurred_at desc);
create index compliance_profiles_org_idx   on public.compliance_profiles (org_id, created_at desc);
create index deadline_alert_log_org_idx    on public.deadline_alert_log (org_id);
create index dsars_org_idx                 on public.dsars (org_id, created_at desc);
create index notification_outbox_org_idx   on public.notification_outbox (org_id);
create index onboarding_messages_org_idx   on public.onboarding_messages (org_id);
create index onboarding_sessions_org_idx   on public.onboarding_sessions (org_id);
create index processing_activities_org_idx on public.processing_activities (org_id, created_at desc);
create index product_review_flags_org_idx  on public.product_review_flags (org_id);
create index weekly_briefing_log_org_idx   on public.weekly_briefing_log (org_id);

------------------------------------------------------------------------------
-- 6. Tenancy helpers
------------------------------------------------------------------------------
-- The two GUC readers are null-safe (two-arg current_setting): functions
-- raise their own 'not authenticated' errors. The policies below use the
-- single-arg doc form instead, so an app connection that never set its GUCs
-- fails loudly rather than quietly reading nothing.

-- +goose StatementBegin
create or replace function public.app_current_org_id()
returns uuid
language sql stable
as $$
  select nullif(current_setting('app.current_org_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.app_current_user_id()
returns uuid
language sql stable
as $$
  select nullif(current_setting('app.current_user_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- The two SECURITY DEFINER helpers below exist for exactly one reason: the
-- policies ON memberships itself cannot subquery memberships (Postgres
-- raises infinite recursion), and a with_check that counts an org's members
-- must see ALL of them, not the RLS-filtered subset (an app-visible count
-- would let a stranger bootstrap themselves into a half-visible org). They
-- are owned by the migrator, whose BYPASSRLS scopes them past the policies;
-- both are STABLE, read-only, and keyed to the caller's GUCs.

-- +goose StatementBegin
create or replace function public.app_org_role(p_org uuid)
returns text
language sql stable security definer
set search_path to 'public', 'pg_temp'
as $$
  select role from public.memberships
  where org_id = p_org and user_id = public.app_current_user_id()
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.app_org_member_count(p_org uuid)
returns integer
language sql stable security definer
set search_path to 'public', 'pg_temp'
as $$
  select count(*)::integer from public.memberships where org_id = p_org
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- 7. Business functions, org-aware
------------------------------------------------------------------------------
-- Ported one by one from the Supabase stack. The mechanical rules:
--   * auth.uid()            -> app_current_user_id() (the acting human)
--   * "where user_id = me"  -> "where org_id = app_current_org_id()"
--     (tenancy filter; RLS enforces it again underneath)
--   * inserts write org_id, and created_by where the table has it
--   * SECURITY DEFINER -> SECURITY INVOKER throughout: the caller's RLS
--     applies inside. System sweeps (run_watcher, run_analyst,
--     expire_snoozed_findings) are meant to run on a maintenance
--     connection (kindlast_migrator or a future system role), never as
--     kindlast_app; until Temporal (build-order step 8) nothing schedules
--     them in this stack.
-- Signature changes, deliberate and breaking (the callers are being
-- rewritten as Go services anyway):
--   * approve_finding / reject_finding / snooze_finding lose their acting-
--     user parameters: the actor is always the GUC user.
--   * record_audit_log gains the org and snapshots the actor's role.

-- +goose StatementBegin
create or replace function public.record_audit_log(
  p_org_id uuid, p_actor_id uuid, p_finding_id uuid, p_action_type text,
  p_target_table text, p_target_id uuid, p_before jsonb, p_after jsonb,
  p_approving_user_id uuid
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_id   uuid;
  v_role text;
begin
  -- Snapshot the actor's role at the time of the action; roles change and
  -- the audit trail must say what authority the actor held then.
  select role into v_role
  from public.memberships
  where org_id = p_org_id and user_id = p_actor_id;

  insert into public.audit_log (
    org_id, user_id, actor_role, finding_id, action_type, target_table,
    target_id, before, after, approving_user_id
  )
  values (
    p_org_id, p_actor_id, v_role, p_finding_id, p_action_type, p_target_table,
    p_target_id, p_before, p_after, p_approving_user_id
  )
  returning id into v_id;

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.emit_watcher_finding(
  p_profile_id uuid, p_kind text, p_dedup_key text, p_title text,
  p_detail text default null, p_severity text default 'medium',
  p_obligation_slug text default null, p_metadata jsonb default '{}'::jsonb
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_org uuid;
  v_id  uuid;
begin
  select org_id into v_org
  from public.compliance_profiles
  where id = p_profile_id;

  if v_org is null then
    raise exception 'emit_watcher_finding: unknown profile %', p_profile_id;
  end if;

  insert into public.watcher_findings (
    profile_id, org_id, kind, obligation_slug, severity, title, detail, dedup_key, metadata
  )
  values (
    p_profile_id, v_org, p_kind, p_obligation_slug, p_severity,
    p_title, p_detail, p_dedup_key, p_metadata
  )
  on conflict (profile_id, dedup_key) where status = 'open'
  do update set
    kind            = excluded.kind,
    obligation_slug = excluded.obligation_slug,
    severity        = excluded.severity,
    title           = excluded.title,
    detail          = excluded.detail,
    metadata        = excluded.metadata,
    updated_at      = now()
  returning id into v_id;

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.analyst_convert_signal(p_signal_id uuid)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_sig    public.watcher_findings;
  v_obl    public.obligations;
  v_action text;
  v_cats   text[];
  v_id     uuid;
begin
  select * into v_sig from public.watcher_findings where id = p_signal_id;
  if not found then
    raise exception 'analyst_convert_signal: unknown signal %', p_signal_id;
  end if;

  if v_sig.obligation_slug is not null then
    select * into v_obl from public.obligations where slug = v_sig.obligation_slug;
  end if;

  if v_obl.id is null then
    raise log 'analyst_convert_signal: signal % has no resolvable obligation (slug %), skipping',
      p_signal_id, v_sig.obligation_slug;
    return null;
  end if;

  select data_categories into v_cats
  from public.compliance_profiles where id = v_sig.profile_id;

  v_action := case v_sig.kind
    when 'deadline'          then 'Review this obligation and prepare to meet its upcoming deadline.'
    when 'profile_gap'       then 'Put the missing control in place to satisfy this obligation.'
    when 'dsar'              then 'Prepare and log a response to this data-subject request before its deadline.'
    when 'regulatory_update' then 'Review this regulatory update and assess its impact on your obligations.'
    else                          'Review this finding and take the appropriate action.'
  end;

  insert into public.findings (
    profile_id, org_id, watcher_finding_id, obligation_id, obligation_slug,
    detected, severity, proposed_action, regulatory_obligation, citation_url,
    supporting_context, effort_estimate, metadata
  )
  values (
    v_sig.profile_id,
    v_sig.org_id,
    v_sig.id,
    v_obl.id,
    v_sig.obligation_slug,
    v_sig.title,
    public.analyst_severity(
      v_obl.severity, v_sig.severity, v_sig.kind,
      (v_sig.metadata ->> 'days_remaining')::int, v_cats
    ),
    v_action,
    public.analyst_citation_label(
      v_obl.citation_celex, v_obl.citation_kind, v_obl.citation_article,
      v_obl.citation_recital, v_obl.citation_annex, v_obl.citation_paragraph
    ),
    public.analyst_citation_url(
      v_obl.citation_celex, v_obl.citation_kind, v_obl.citation_article,
      v_obl.citation_recital, v_obl.citation_annex
    ),
    v_obl.summary,
    public.analyst_effort(v_sig.kind),
    jsonb_build_object(
      'signal_kind',      v_sig.kind,
      'signal_dedup_key', v_sig.dedup_key,
      'signal_metadata',  v_sig.metadata
    )
  )
  on conflict (watcher_finding_id) do update set
    -- detected / proposed_action / narrative_generated_at are PRESERVED (ENT-60).
    -- severity refreshes because proximity changes over time; effort is stable.
    obligation_id         = excluded.obligation_id,
    obligation_slug       = excluded.obligation_slug,
    severity              = excluded.severity,
    regulatory_obligation = excluded.regulatory_obligation,
    citation_url          = excluded.citation_url,
    supporting_context    = excluded.supporting_context,
    metadata              = excluded.metadata,
    updated_at            = now()
  returning id into v_id;

  return v_id;
end;
$function$;
-- +goose StatementEnd

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
    return null;  -- unknown finding, wrong org, or already approved
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

  return v_until;  -- null when nothing matched (unknown / wrong org)
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.expire_snoozed_findings()
returns integer
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_count integer;
begin
  with reemerged as (
    update public.findings
      set status = 'pending',
          snoozed_until = null
    where status = 'snoozed'
      and snoozed_until is not null
      and snoozed_until <= now()
    returning 1
  )
  select count(*) into v_count from reemerged;

  return v_count;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.ropa_manual_activity_limit()
returns integer
language sql stable
set search_path to 'public', 'pg_temp'
as $function$
  select case
    when exists (
      select 1 from public.subscriptions
      where org_id = public.app_current_org_id() and plan = 'pro'
    ) then null::integer
    else 3
  end
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.create_processing_activity(
  p_name text, p_purpose text, p_legal_basis text, p_data_categories text[],
  p_recipients text[], p_retention_period text
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_org     uuid := public.app_current_org_id();
  v_profile uuid;
  v_limit   integer;
  v_manual  integer;
  v_id      uuid;
  v_after   jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'create_processing_activity: not authenticated';
  end if;

  -- The organisation's current (most recent) compliance profile owns the activity.
  select id into v_profile
  from public.compliance_profiles
  where org_id = v_org
  order by created_at desc
  limit 1;
  if v_profile is null then
    raise exception 'create_processing_activity: no compliance profile for organisation';
  end if;

  -- Free-tier cap: manual activities only (Executor-ratified rows are unlimited).
  -- A NULL limit means Pro / uncapped. The cap is per organisation (§20.1).
  v_limit := public.ropa_manual_activity_limit();
  if v_limit is not null then
    select count(*) into v_manual
    from public.processing_activities
    where org_id = v_org and finding_id is null;
    if v_manual >= v_limit then
      raise exception 'free tier limit: a manual ROPA is capped at % activities', v_limit
        using errcode = 'check_violation';
    end if;
  end if;

  insert into public.processing_activities (
    profile_id, org_id, created_by, finding_id,
    name, purpose, legal_basis, data_categories, recipients, retention_period
  )
  values (
    v_profile, v_org, v_user, null,
    coalesce(nullif(btrim(p_name), ''), 'Untitled activity'),
    p_purpose, p_legal_basis,
    coalesce(p_data_categories, '{}'),
    coalesce(p_recipients, '{}'),
    p_retention_period
  )
  returning id into v_id;

  select to_jsonb(pa.*) into v_after from public.processing_activities pa where pa.id = v_id;

  perform public.record_audit_log(
    v_org, v_user, null, 'create_ropa_manual', 'processing_activities', v_id, null, v_after, v_user
  );

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.update_processing_activity(
  p_id uuid, p_name text, p_purpose text, p_legal_basis text,
  p_data_categories text[], p_recipients text[], p_retention_period text
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user   uuid := public.app_current_user_id();
  v_org    uuid := public.app_current_org_id();
  v_before jsonb;
  v_after  jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'update_processing_activity: not authenticated';
  end if;

  select to_jsonb(pa.*) into v_before
  from public.processing_activities pa
  where pa.id = p_id and pa.org_id = v_org;
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
  where id = p_id and org_id = v_org;

  select to_jsonb(pa.*) into v_after
  from public.processing_activities pa
  where pa.id = p_id;

  -- Audit only a real change (ignore the trigger's updated_at bump).
  if (v_before - 'updated_at') is distinct from (v_after - 'updated_at') then
    perform public.record_audit_log(
      v_org, v_user,
      (v_after ->> 'finding_id')::uuid,  -- links to the originating finding, if any
      'update_ropa', 'processing_activities', p_id, v_before, v_after, v_user
    );
  end if;

  return p_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.create_ai_system_manual(
  p_name text, p_vendor text, p_purpose text, p_risk_classification text,
  p_documentation_status text, p_reviewed boolean default false
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user    uuid := public.app_current_user_id();
  v_org     uuid := public.app_current_org_id();
  v_profile uuid;
  v_class   text := coalesce(nullif(btrim(p_risk_classification), ''), 'unclassified');
  v_id      uuid;
  v_after   jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'create_ai_system_manual: not authenticated';
  end if;

  select id into v_profile
  from public.compliance_profiles
  where org_id = v_org
  order by created_at desc
  limit 1;
  if v_profile is null then
    raise exception 'create_ai_system_manual: no compliance profile for organisation';
  end if;

  -- A High-Risk classification needs a reviewed approval (consistent with ENT-68).
  if v_class = 'high' and not p_reviewed then
    raise exception 'create_ai_system_manual: a High-Risk classification requires a reviewed approval'
      using errcode = 'check_violation';
  end if;

  insert into public.ai_systems (
    profile_id, org_id, created_by, finding_id,
    name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
  )
  values (
    v_profile, v_org, v_user, null,
    coalesce(nullif(btrim(p_name), ''), 'Untitled system'),
    p_vendor, p_purpose, v_class,
    coalesce(nullif(btrim(p_documentation_status), ''), 'missing'),
    now()
  )
  returning id into v_id;

  select to_jsonb(a.*) into v_after from public.ai_systems a where a.id = v_id;

  perform public.record_audit_log(
    v_org, v_user, null, 'create_ai_system_manual', 'ai_systems', v_id, null, v_after, v_user
  );

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.update_ai_system(
  p_id uuid, p_name text, p_vendor text, p_purpose text,
  p_risk_classification text, p_documentation_status text,
  p_reviewed boolean default false
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user      uuid := public.app_current_user_id();
  v_org       uuid := public.app_current_org_id();
  v_before    jsonb;
  v_after     jsonb;
  v_old_class text;
  v_new_class text;
  v_reclass   boolean;
begin
  if v_user is null or v_org is null then
    raise exception 'update_ai_system: not authenticated';
  end if;

  select to_jsonb(a.*) into v_before
  from public.ai_systems a
  where a.id = p_id and a.org_id = v_org;
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
  where id = p_id and org_id = v_org;

  select to_jsonb(a.*) into v_after from public.ai_systems a where a.id = p_id;

  -- Audit only a real change (ignore the trigger's updated_at bump).
  if (v_before - 'updated_at') is distinct from (v_after - 'updated_at') then
    perform public.record_audit_log(
      v_org, v_user,
      (v_after ->> 'finding_id')::uuid,
      case when v_reclass then 'reclassify_ai_system' else 'update_ai_system' end,
      'ai_systems', p_id, v_before, v_after, v_user
    );
  end if;

  return p_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.log_dsar(p_subject_name text, p_request_type text, p_handler text)
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

-- +goose StatementBegin
create or replace function public.mark_dsar_responded(p_id uuid, p_reviewed boolean default false)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_user   uuid := public.app_current_user_id();
  v_org    uuid := public.app_current_org_id();
  v_before jsonb;
  v_after  jsonb;
begin
  if v_user is null or v_org is null then
    raise exception 'mark_dsar_responded: not authenticated';
  end if;

  if not p_reviewed then
    raise exception 'mark_dsar_responded: marking a DSAR responded requires a reviewed approval'
      using errcode = 'check_violation';
  end if;

  select to_jsonb(d.*) into v_before
  from public.dsars d
  where d.id = p_id and d.org_id = v_org;
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
  where id = p_id and org_id = v_org;

  select to_jsonb(d.*) into v_after from public.dsars d where d.id = p_id;

  perform public.record_audit_log(
    v_org, v_user,
    (v_after ->> 'finding_id')::uuid,  -- links to the originating finding, if any
    'mark_dsar_responded', 'dsars', p_id, v_before, v_after, v_user
  );

  return p_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.finding_supporting_chunks(p_finding_id uuid)
returns table(ordinal integer, label text, quoted_text text, source_url text)
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_finding public.findings;
  v_obl     public.obligations;
  v_doc     public.regulatory_documents;
  v_article public.regulatory_articles;
  v_recital public.regulatory_recitals;
  v_para    record;
  v_ord     int;
begin
  -- Tenancy gate: resolve the finding; bail (no rows, no exception) unless it
  -- exists and belongs to the caller's organisation. RLS enforces the same
  -- thing underneath; the explicit check keeps the empty-result contract.
  select * into v_finding from public.findings where id = p_finding_id;
  if not found or v_finding.org_id <> public.app_current_org_id() then
    return;
  end if;

  select * into v_obl from public.obligations where id = v_finding.obligation_id;

  -- Resolve the corpus document by CELEX natural key (may be absent).
  if v_obl.id is not null then
    select * into v_doc
      from public.regulatory_documents
     where celex_number = v_obl.citation_celex;
  end if;

  -- Case 1: article with a matching corpus article row.
  if v_obl.citation_kind = 'article' and v_doc.id is not null then
    select * into v_article
      from public.regulatory_articles
     where document_id = v_doc.id
       and article_number = v_obl.citation_article;

    if found then
      -- Chunk 1: the article itself (heading + body).
      ordinal     := 1;
      label       := public.analyst_citation_label(
                       v_obl.citation_celex, 'article', v_obl.citation_article,
                       null, null, null);
      quoted_text := v_article.heading || E'\n\n' || v_article.summary;
      source_url  := public.analyst_citation_url(
                       v_obl.citation_celex, 'article', v_obl.citation_article,
                       null, null);
      return next;

      -- Chunks 2..N: sub-paragraphs in source (`ordering`) order, each deep-
      -- labelled (e.g. "GDPR Art. 30(1)(b)") but anchored to the same article URL.
      v_ord := 1;
      for v_para in
        select paragraph_label, summary
          from public.regulatory_article_paragraphs
         where article_id = v_article.id
         order by ordering asc
      loop
        v_ord       := v_ord + 1;
        ordinal     := v_ord;
        label       := public.analyst_citation_label(
                         v_obl.citation_celex, 'article', v_obl.citation_article,
                         null, null, v_para.paragraph_label);
        quoted_text := v_para.summary;
        source_url  := public.analyst_citation_url(
                         v_obl.citation_celex, 'article', v_obl.citation_article,
                         null, null);
        return next;
      end loop;

      return;
    end if;
  end if;

  -- Case 2: recital with a matching corpus recital row.
  if v_obl.citation_kind = 'recital' and v_doc.id is not null then
    select * into v_recital
      from public.regulatory_recitals
     where document_id = v_doc.id
       and recital_number = v_obl.citation_recital;

    if found then
      ordinal     := 1;
      label       := public.analyst_citation_label(
                       v_obl.citation_celex, 'recital', null,
                       v_obl.citation_recital, null, null);
      quoted_text := v_recital.summary;
      source_url  := public.analyst_citation_url(
                       v_obl.citation_celex, 'recital', null,
                       v_obl.citation_recital, null);
      return next;
      return;
    end if;
  end if;

  -- Fallback: annex, or no matching corpus document/article/recital. The detail
  -- view still gets one chunk, built from the finding's denormalised citation.
  ordinal     := 1;
  label       := v_finding.regulatory_obligation;
  quoted_text := v_finding.supporting_context;
  source_url  := v_finding.citation_url;
  return next;
  return;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.watcher_detect_deadlines(p_profile_id uuid)
returns void
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_profile public.compliance_profiles;
  o         record;
  d         record;
  v_days    int;
begin
  select * into v_profile from public.compliance_profiles where id = p_profile_id;
  if not found then
    return;
  end if;

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

  -- DSARs owed a response within the window. Org-scoped: the request belongs
  -- to the organisation, whoever logged it.
  for d in
    select id, subject_name, response_due_at
    from public.dsars
    where org_id = v_profile.org_id
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.watcher_detect_dsar_escalation(p_profile_id uuid)
returns void
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_profile public.compliance_profiles;
  d         record;
  v_days    int;
begin
  select * into v_profile from public.compliance_profiles where id = p_profile_id;
  if not found then
    return;
  end if;

  for d in
    select id, subject_name, response_due_at
    from public.dsars
    where org_id = v_profile.org_id
      and status in ('open', 'in_progress')
      and responded_at is null
      and (response_due_at::date - current_date) < 10
  loop
    v_days := (d.response_due_at::date - current_date);

    perform public.emit_watcher_finding(
      p_profile_id,
      'dsar',
      'dsar:' || d.id,
      case
        when v_days < 0 then
          'DSAR response is ' || abs(v_days) || ' day' || case when abs(v_days) = 1 then '' else 's' end || ' overdue'
        else
          'URGENT: DSAR response due in ' || v_days || ' day' || case when v_days = 1 then '' else 's' end
      end,
      'A data-subject request' ||
        case when d.subject_name is not null then ' from ' || d.subject_name else '' end ||
        case
          when v_days < 0 then
            ' is past its GDPR Article 12(3) one-month deadline with no logged response.'
          else
            ' is within 10 days of its GDPR Article 12(3) one-month deadline with no logged response.'
        end,
      'critical',
      'gdpr-arts-12-22-data-subject-rights',
      jsonb_build_object(
        'days_remaining', v_days,
        'dsar_id', d.id,
        'response_due_at', d.response_due_at,
        'escalated', true
      )
    );
  end loop;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.watcher_detect_gaps(p_profile_id uuid)
returns void
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_profile   public.compliance_profiles;
  o           record;
  v_token     text;
  v_missing   text[];
  v_key       text;
  v_recurring boolean;
  v_title     text;
  v_detail    text;
begin
  select * into v_profile from public.compliance_profiles where id = p_profile_id;
  if not found then
    return;
  end if;

  for o in
    select slug, title, severity, applies_when
    from public.obligations
    where applies_when ? 'requires'
      and jsonb_typeof(applies_when -> 'requires') = 'array'
  loop
    -- Applicability gate: the shared predicate decides whether the obligation
    -- even reaches this profile before we ask whether its control is in place.
    if not public.watcher_obligation_applies(o.applies_when, v_profile) then
      continue;
    end if;

    -- Collect the unsatisfied required controls.
    v_missing := array[]::text[];
    for v_token in
      select jsonb_array_elements_text(o.applies_when -> 'requires')
    loop
      if not public.watcher_gap_satisfied(v_token, v_profile) then
        v_missing := v_missing || v_token;
      end if;
    end loop;

    -- Every required control is in place ⇒ no gap.
    if array_length(v_missing, 1) is null then
      continue;
    end if;

    v_key := 'gap:obligation:' || o.slug;

    -- Re-surface rule: raise a different message if the user has previously
    -- dismissed this exact gap. The open-finding partial unique index means a
    -- dismissed row frees the key, so emit_watcher_finding() opens a fresh
    -- finding here rather than refreshing the dismissed one.
    v_recurring := exists (
      select 1 from public.watcher_findings
      where profile_id = p_profile_id
        and dedup_key  = v_key
        and status     = 'dismissed'
    );

    if v_recurring then
      v_title  := 'Recurring gap: ' || o.title;
      v_detail := 'You previously dismissed this finding, but the gap is still '
               || 'present in your profile and maps to an obligation that applies '
               || 'to you. Revisit it or dismiss it again.';
    else
      v_title  := 'Profile gap: ' || o.title;
      v_detail := 'Your profile indicates this obligation applies, but the '
               || 'corresponding control does not appear to be in place yet.';
    end if;

    perform public.emit_watcher_finding(
      p_profile_id,
      'profile_gap',
      v_key,
      v_title,
      v_detail,
      o.severity,
      o.slug,
      jsonb_build_object('missing', to_jsonb(v_missing), 'recurring', v_recurring)
    );
  end loop;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.run_watcher_for_profile(p_profile_id uuid)
returns void
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
begin
  -- Detectors (each calls public.emit_watcher_finding):
  perform public.watcher_detect_deadlines(p_profile_id);        -- ENT-55
  perform public.watcher_detect_gaps(p_profile_id);             -- ENT-56
  perform public.watcher_detect_dsar_escalation(p_profile_id);  -- ENT-57
  update public.compliance_profiles
  set watcher_last_run_at = now()
  where id = p_profile_id;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.run_watcher()
returns integer
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  r record;
  n integer := 0;
begin
  -- One sweep per organisation, against its most recent profile. Runs on a
  -- maintenance connection until Temporal owns the schedule (step 8).
  for r in
    select distinct on (org_id) id
    from public.compliance_profiles
    order by org_id, created_at desc
  loop
    perform public.run_watcher_for_profile(r.id);
    n := n + 1;
  end loop;

  return n;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.run_analyst_for_profile(p_profile_id uuid)
returns integer
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  s record;
  n integer := 0;
begin
  for s in
    select id from public.watcher_findings
    where profile_id = p_profile_id
      and status = 'open'
    order by created_at, id
  loop
    if public.analyst_convert_signal(s.id) is not null then
      n := n + 1;
    end if;
  end loop;

  return n;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.run_analyst()
returns integer
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  r record;
  n integer := 0;
begin
  for r in
    select distinct on (org_id) id
    from public.compliance_profiles
    order by org_id, created_at desc
  loop
    perform public.run_analyst_for_profile(r.id);
    n := n + 1;
  end loop;

  return n;
end;
$function$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- 8. Business triggers
------------------------------------------------------------------------------

-- +goose StatementBegin
create or replace function public.enqueue_finding_notification()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
begin
  -- The recipient is resolved at dispatch time (an org has members, not "a
  -- user"); the enqueue records only which org the doorbell belongs to.
  insert into public.notification_outbox (finding_id, org_id)
  values (new.id, new.org_id)
  on conflict (finding_id) do nothing;
  return new;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.executor_create_ropa_on_approval()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_pa_id    uuid;
  v_after    jsonb;
  v_approver uuid := new.approved_by;
begin
  -- One ratified activity per finding. A second approval transition (or a
  -- concurrent fire) is a no-op rather than a duplicate.
  if exists (select 1 from public.processing_activities where finding_id = new.id) then
    return new;
  end if;

  insert into public.processing_activities (
    profile_id, org_id, created_by, finding_id,
    name, purpose, legal_basis, data_categories, recipients, retention_period
  )
  values (
    new.profile_id,
    new.org_id,
    v_approver,
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
    new.org_id, v_approver, new.id, 'create_ropa',
    'processing_activities', v_pa_id, null, v_after, v_approver
  );

  return new;
end;
$function$;
-- +goose StatementEnd

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
  -- One DSAR per finding. A repeat approval transition is a no-op.
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
    v_payload ->> 'requester',     -- the data subject who made the request
    v_payload ->> 'request_type',
    v_payload ->> 'handler',
    'open',
    now(),
    now() + interval '30 days'     -- response_due_at = received_at + 30 days
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

-- +goose StatementBegin
create or replace function public.executor_create_ai_system_on_approval()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_payload  jsonb := coalesce(new.metadata -> 'payload', '{}'::jsonb);
  v_class    text  := coalesce(nullif(v_payload ->> 'risk_classification', ''), 'unclassified');
  v_id       uuid;
  v_after    jsonb;
  v_approver uuid := new.approved_by;
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
    profile_id, org_id, created_by, finding_id,
    name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
  )
  values (
    new.profile_id,
    new.org_id,
    v_approver,
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
    new.org_id, v_approver, new.id, 'create_ai_system',
    'ai_systems', v_id, null, v_after, v_approver
  );

  return new;
end;
$function$;
-- +goose StatementEnd

create trigger enqueue_finding_notification
  after insert on public.findings
  for each row execute function public.enqueue_finding_notification();

create trigger executor_create_ropa
  after update of status on public.findings
  for each row
  when (new.status = 'approved' and old.status is distinct from 'approved' and new.action_type = 'create_ropa')
  execute function public.executor_create_ropa_on_approval();

create trigger executor_create_dsar
  after update of status on public.findings
  for each row
  when (new.status = 'approved' and old.status is distinct from 'approved' and new.action_type = 'create_dsar')
  execute function public.executor_create_dsar_on_approval();

create trigger executor_create_ai_system
  after update of status on public.findings
  for each row
  when (new.status = 'approved' and old.status is distinct from 'approved' and new.action_type = 'create_ai_system')
  execute function public.executor_create_ai_system_on_approval();

------------------------------------------------------------------------------
-- 9. Row level security: every policy in the database
------------------------------------------------------------------------------
-- Reviewed policy by policy against the legacy set; the mapping is recorded
-- in the ENT-192 PR description. Two shapes:
--
--   TENANT  the two-GUC form (org equality + membership exists)
--   PUBLIC  using (true), the corpus tables: the regulatory corpus is
--           shared reference data, readable by design
--
-- Role-based authorization (owner / member / viewer semantics) is the API
-- layer's job (§0.5: three authorization layers); RLS enforces tenancy.

-- organisations ---------------------------------------------------------------
alter table public.organisations enable row level security;
alter table public.organisations force row level security;

create policy organisations_select_member on public.organisations
  for select using (
    exists (
      select 1 from public.memberships m
      where m.org_id = organisations.id
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- Any authenticated principal may create an organisation; becoming its owner
-- is the memberships bootstrap policy's job, atomically in the same
-- transaction.
create policy organisations_insert_any on public.organisations
  for insert with check (
    (select current_setting('app.current_user_id')::uuid) is not null
  );

create policy organisations_update_owner on public.organisations
  for update
  using (public.app_org_role(id) = 'owner')
  with check (public.app_org_role(id) = 'owner');

create policy organisations_delete_owner on public.organisations
  for delete using (public.app_org_role(id) = 'owner');

-- memberships -----------------------------------------------------------------
-- These policies cannot subquery memberships (recursion), hence the
-- SECURITY DEFINER helpers.
alter table public.memberships enable row level security;
alter table public.memberships force row level security;

-- Own rows always visible (this is also what makes the inline `exists` in
-- every tenant policy work); co-members visible via the helper.
create policy memberships_select_member on public.memberships
  for select using (
    user_id = (select current_setting('app.current_user_id')::uuid)
    or public.app_org_role(org_id) is not null
  );

-- Insert: an owner adds members, OR a user bootstraps an empty organisation
-- by making themselves its owner. The count comes from the definer helper
-- because an RLS-filtered count would let a stranger "bootstrap" an org
-- whose members they simply cannot see.
create policy memberships_insert_owner_or_bootstrap on public.memberships
  for insert with check (
    public.app_org_role(org_id) = 'owner'
    or (
      user_id = (select current_setting('app.current_user_id')::uuid)
      and role = 'owner'
      and public.app_org_member_count(org_id) = 0
    )
  );

create policy memberships_update_owner on public.memberships
  for update
  using (public.app_org_role(org_id) = 'owner')
  with check (public.app_org_role(org_id) = 'owner');

-- An owner removes members; anyone may remove themselves (leave).
create policy memberships_delete_owner_or_self on public.memberships
  for delete using (
    public.app_org_role(org_id) = 'owner'
    or user_id = (select current_setting('app.current_user_id')::uuid)
  );

-- Tenant tables ---------------------------------------------------------------
-- The two-GUC predicate, verbatim per table. SELECT/INSERT/UPDATE/DELETE
-- mirror the legacy per-command surface; deviations are called out inline.

-- ai_systems (legacy: select/insert/update/delete _own)
create policy ai_systems_select_org on public.ai_systems
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy ai_systems_insert_org on public.ai_systems
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy ai_systems_update_org on public.ai_systems
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy ai_systems_delete_org on public.ai_systems
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- audit_log (legacy: insert/select _own; append-only via trigger)
-- The insert additionally binds the actor column to the GUC user: an audit
-- row must name the human who actually acted.
create policy audit_log_select_org on public.audit_log
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy audit_log_insert_org on public.audit_log
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- compliance_profiles (legacy: select/insert/update/delete _own)
create policy compliance_profiles_select_org on public.compliance_profiles
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy compliance_profiles_insert_org on public.compliance_profiles
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy compliance_profiles_update_org on public.compliance_profiles
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy compliance_profiles_delete_org on public.compliance_profiles
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- deadline_alert_log (legacy: select _own; written by the system path)
create policy deadline_alert_log_select_org on public.deadline_alert_log
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- dsars (legacy: select/insert/update/delete _own)
create policy dsars_select_org on public.dsars
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy dsars_insert_org on public.dsars
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy dsars_update_org on public.dsars
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy dsars_delete_org on public.dsars
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- findings (legacy: select _own only; approvals went through SECURITY
-- DEFINER functions). DEVIATION, recorded: the functions are INVOKER now, so
-- members need UPDATE to act on a finding (approve/snooze/reject). INSERT
-- and DELETE stay system-only: the Analyst writes findings, nobody deletes
-- them.
create policy findings_select_org on public.findings
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy findings_update_org on public.findings
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- notification_outbox (legacy: select _own; written by the enqueue trigger
-- on the system insert path and consumed by the dispatch path)
create policy notification_outbox_select_org on public.notification_outbox
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- notification_preferences (legacy: select/insert/update _own). Preferences
-- are personal within the organisation, so every command additionally binds
-- user_id to the GUC user: members cannot read or edit each other's
-- preferences.
create policy notif_prefs_select_own on public.notification_preferences
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy notif_prefs_insert_own on public.notification_preferences
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy notif_prefs_update_own on public.notification_preferences
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- onboarding_messages (legacy: select/insert/update/delete _own)
create policy onboarding_messages_select_org on public.onboarding_messages
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_messages_insert_org on public.onboarding_messages
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_messages_update_org on public.onboarding_messages
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_messages_delete_org on public.onboarding_messages
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- onboarding_sessions (legacy: select/insert/update/delete _own)
create policy onboarding_sessions_select_org on public.onboarding_sessions
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_sessions_insert_org on public.onboarding_sessions
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_sessions_update_org on public.onboarding_sessions
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy onboarding_sessions_delete_org on public.onboarding_sessions
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- processing_activities (legacy: select/insert/update/delete _own)
create policy processing_activities_select_org on public.processing_activities
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy processing_activities_insert_org on public.processing_activities
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy processing_activities_update_org on public.processing_activities
  for update
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy processing_activities_delete_org on public.processing_activities
  for delete using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- product_review_flags (legacy: select _own; inserted by reject_finding,
-- which now needs an INSERT policy since it runs as the caller. DEVIATION,
-- recorded: insert added, org-scoped; the table stays append-only via its
-- forbid-update trigger.)
create policy product_review_flags_select_org on public.product_review_flags
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );
create policy product_review_flags_insert_org on public.product_review_flags
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- subscriptions (legacy: select _own; the billing webhook wrote as
-- service_role. Writes stay off the app role: the future billing path runs
-- on a system connection.)
create policy subscriptions_select_org on public.subscriptions
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- watcher_findings (legacy: select _own; written by the Watcher system path)
create policy watcher_findings_select_org on public.watcher_findings
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- weekly_briefing_log (legacy: select _own; written by the briefing system path)
create policy weekly_briefing_log_select_org on public.weekly_briefing_log
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- The regulatory corpus: shared reference data, public reads by design
-- (legacy select_public, carried unchanged). No write policies: ingestion
-- runs as the migrator.
create policy obligations_select_public on public.obligations
  for select using (true);
create policy regulatory_annex_items_select_public on public.regulatory_annex_items
  for select using (true);
create policy regulatory_annexes_select_public on public.regulatory_annexes
  for select using (true);
create policy regulatory_article_paragraphs_select_public on public.regulatory_article_paragraphs
  for select using (true);
create policy regulatory_article_recitals_select_public on public.regulatory_article_recitals
  for select using (true);
create policy regulatory_articles_select_public on public.regulatory_articles
  for select using (true);
create policy regulatory_documents_select_public on public.regulatory_documents
  for select using (true);
create policy regulatory_enforcement_decisions_select_public on public.regulatory_enforcement_decisions
  for select using (true);
create policy regulatory_guidelines_select_public on public.regulatory_guidelines
  for select using (true);
create policy regulatory_recitals_select_public on public.regulatory_recitals
  for select using (true);

-- billing_webhook_events: RLS on, ZERO policies, deliberately (carried from
-- ENT-152): provider webhook dedup state is infrastructure, reachable only
-- by the system path.

------------------------------------------------------------------------------
-- 10. FORCE ROW LEVEL SECURITY, asserted for the whole schema
------------------------------------------------------------------------------
-- The owner-bypass loophole (§14.1) closes here: even kindlast_migrator is
-- subject to policies on FORCEd tables unless its BYPASSRLS applies. Every
-- table in public, no exceptions; the db test suite asserts this via
-- pg_class rather than trusting this comment.

-- +goose StatementBegin
do $$
declare
  t text;
begin
  for t in
    select c.relname
    from pg_class c
    join pg_namespace n on n.oid = c.relnamespace
    where n.nspname = 'public' and c.relkind = 'r'
  loop
    execute format('alter table public.%I enable row level security', t);
    execute format('alter table public.%I force row level security', t);
  end loop;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- 11. Grants: RLS is the sole gate (the ENT-159 contract, ported)
------------------------------------------------------------------------------
-- kindlast_app: DML on everything, TRUNCATE on nothing, DDL impossible
-- (owns nothing, schema CREATE was never granted). kindlast_vector_ro gets
-- its grants when the chunk/embedding tables land (ENT-51); creating it
-- with zero grants now means the connection string exists from day one.

grant select, insert, update, delete on all tables in schema public to kindlast_app;

alter default privileges in schema public
  grant select, insert, update, delete on tables to kindlast_app;

-- +goose Down
-- The organisation model is the baseline; there is no down migration.
-- Restore from backup instead.
