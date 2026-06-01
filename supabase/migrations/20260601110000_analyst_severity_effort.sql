-- The Analyst: severity + effort estimate on every finding (ENT-61)
--
-- The last Analyst sub-issue. ENT-58–60 carried severity straight from the
-- signal and left effort_estimate a flat 'hours' baseline. ENT-61 makes both
-- first-class so the feed and notification routing can sort and gate:
--
--   * Severity is derived — the obligation's baseline severity adjusted for
--     proximity to deadline, data sensitivity, and recency — never downgrading
--     a Watcher escalation (ENT-57's <10-day DSAR → critical).
--   * Effort is a real minutes/hours/days estimate.
--   * Both become native ENUM columns. Declaration order is the sort order, so
--     `order by severity desc` ranks critical first (text + CHECK sorted
--     alphabetically — critical < high < low < medium — which is wrong).
--   * Notification preferences (email frequency) get a home + a severity gate
--     (the gate function itself is lib/notifications/preferences.ts; the email
--     sender is the Comms epic).
--
-- Idempotent: enum creation is guarded; column conversions and `create or
-- replace` re-run cleanly.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Enum types ───────────────────────────────────────────────────────────────
do $$
begin
  if not exists (select 1 from pg_type where typname = 'severity_level') then
    create type public.severity_level as enum ('low', 'medium', 'high', 'critical');
  end if;
  if not exists (select 1 from pg_type where typname = 'effort_level') then
    create type public.effort_level as enum ('minutes', 'hours', 'days');
  end if;
  if not exists (select 1 from pg_type where typname = 'email_frequency') then
    create type public.email_frequency as enum ('immediate', 'daily', 'weekly', 'off');
  end if;
end $$;

-- 2. Convert findings.severity / effort_estimate from text+CHECK to enums ──────
alter table public.findings drop constraint if exists findings_severity_check;
alter table public.findings drop constraint if exists findings_effort_estimate_check;

alter table public.findings
  alter column severity drop default,
  alter column severity type public.severity_level using severity::public.severity_level,
  alter column severity set default 'medium'::public.severity_level;

alter table public.findings
  alter column effort_estimate drop default,
  alter column effort_estimate type public.effort_level using effort_estimate::public.effort_level,
  alter column effort_estimate set default 'hours'::public.effort_level;

-- 3. analyst_severity() ────────────────────────────────────────────────────────
--
-- Pure function of the obligation baseline + signal context. Bumps are additive
-- and clamped; the final level is floored at the signal's own severity so a
-- Watcher escalation is never undone.
create or replace function public.analyst_severity(
  p_baseline        text,
  p_signal_severity text,
  p_kind            text,
  p_days_remaining  int,
  p_data_categories text[]
)
returns public.severity_level
language plpgsql
immutable
set search_path = public, pg_temp
as $$
declare
  v_level     int;
  v_sig_level int;
  v_sensitive text[] := array[
    'health', 'medical', 'biometric', 'genetic', 'financial', 'bank', 'payment',
    'children', 'child', 'racial', 'ethnic', 'religious', 'sexual', 'criminal',
    'political'
  ];
  c text;
begin
  v_level := case lower(coalesce(p_baseline, ''))
    when 'low' then 1 when 'medium' then 2 when 'high' then 3 when 'critical' then 4
    else 2 end;

  -- proximity to deadline (deadline / DSAR signals carry days_remaining)
  if p_days_remaining is not null then
    if p_days_remaining < 3 then
      v_level := v_level + 2;
    elsif p_days_remaining < 7 then
      v_level := v_level + 1;
    end if;
  end if;

  -- data sensitivity: any captured category matching a special-category marker
  if p_data_categories is not null then
    foreach c in array p_data_categories loop
      if exists (select 1 from unnest(v_sensitive) s where lower(c) like '%' || s || '%') then
        v_level := v_level + 1;
        exit;
      end if;
    end loop;
  end if;

  -- recency: a freshly-effective regulatory change is more urgent
  if p_kind = 'regulatory_update' then
    v_level := v_level + 1;
  end if;

  v_level := greatest(1, least(4, v_level));

  -- never downgrade below the signal's own (possibly escalated) severity
  v_sig_level := case lower(coalesce(p_signal_severity, ''))
    when 'low' then 1 when 'medium' then 2 when 'high' then 3 when 'critical' then 4
    else 0 end;
  v_level := greatest(v_level, v_sig_level);

  return (array['low', 'medium', 'high', 'critical'])[v_level]::public.severity_level;
end;
$$;

-- 4. analyst_effort() ──────────────────────────────────────────────────────────
--
-- Initial by-kind model: a DSAR response or a quick review is hours; standing up
-- a control (DPO, ROPA) or preparing for an obligation taking effect is days.
-- Refining per-obligation is a follow-up.
create or replace function public.analyst_effort(p_kind text)
returns public.effort_level
language sql
immutable
set search_path = public, pg_temp
as $$
  select (case p_kind
    when 'dsar'              then 'hours'
    when 'deadline'          then 'days'
    when 'profile_gap'       then 'days'
    when 'regulatory_update' then 'hours'
    else                          'hours'
  end)::public.effort_level;
$$;

-- 5. Conversion: compute severity + effort (ENT-60 body, narrative-preserving) ─
create or replace function public.analyst_convert_signal(p_signal_id uuid)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
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
    profile_id, user_id, watcher_finding_id, obligation_id, obligation_slug,
    detected, severity, proposed_action, regulatory_obligation, citation_url,
    supporting_context, effort_estimate, metadata
  )
  values (
    v_sig.profile_id,
    v_sig.user_id,
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
$$;

-- 6. notification_preferences ──────────────────────────────────────────────────
--
-- One row per user; the email-frequency preference the Comms agent's severity
-- gate (lib/notifications/preferences.ts) reads. User-owned under RLS.
create table if not exists public.notification_preferences (
  user_id         uuid        primary key references auth.users(id) on delete cascade,
  email_frequency public.email_frequency not null default 'daily',
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);

drop trigger if exists set_updated_at on public.notification_preferences;
create trigger set_updated_at
  before update on public.notification_preferences
  for each row execute function public.set_updated_at();

alter table public.notification_preferences enable row level security;

drop policy if exists "notif_prefs_select_own" on public.notification_preferences;
create policy "notif_prefs_select_own" on public.notification_preferences
  for select using (auth.uid() = user_id);

drop policy if exists "notif_prefs_insert_own" on public.notification_preferences;
create policy "notif_prefs_insert_own" on public.notification_preferences
  for insert with check (auth.uid() = user_id);

drop policy if exists "notif_prefs_update_own" on public.notification_preferences;
create policy "notif_prefs_update_own" on public.notification_preferences
  for update using (auth.uid() = user_id) with check (auth.uid() = user_id);
