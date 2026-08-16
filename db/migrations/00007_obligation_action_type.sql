-- +goose Up
-- 00007_obligation_action_type.sql (ENT-165, inside ENT-203)
--
-- The Executor can fire.
--
-- WHAT WAS WRONG
--
-- `findings.action_type` exists, is constrained to four values, and is read by
-- the three executor triggers, which each require it to be `create_ropa`,
-- `create_dsar` or `create_ai_system`. Nothing ever wrote it.
-- `analyst_convert_signal`'s INSERT column list omits the column, so every
-- finding is created with the column default, `'review'`, and all three
-- triggers are permanently unreachable.
--
-- The visible consequence is that approving a finding changes a status and does
-- nothing else, while the billing page sells "One-tap Executor actions" at
-- 49 EUR/month. The invisible one was fixed in 00006: with no trigger firing,
-- no audit row was written either.
--
-- WHY THE ACTION LIVES ON THE OBLIGATION
--
-- ENT-165 proposed adding `action_type` to `obligations` and this follows that,
-- because the obligation is the only place that knows. What approving a finding
-- should *do* is a property of the regulatory requirement, not of the signal
-- that noticed it: Article 30 requires a record of processing activities, so a
-- finding against Article 30 should create a ROPA entry, whether the Watcher
-- raised it from a profile gap, a deadline or a regulatory update. Deriving it
-- from `watcher_findings.kind` instead would give the same obligation different
-- consequences depending on which sweep happened to catch it.
--
-- Default `'review'`, and the default is the honest one. An obligation whose
-- action nobody has classified yet produces a finding a human reviews, which is
-- what happens today for every obligation. This migration therefore changes no
-- behaviour on its own: it makes the mechanism work and leaves the corpus to
-- say which obligations do more than that. Populating it is a data task, not a
-- schema one, and is deliberately not smuggled in here.
--
-- WHAT AN APPROVAL WRITES AFTERWARDS
--
-- Two audit rows, not one, once an obligation carries a real action: the
-- decision (00006) and the record created (the trigger). That is the correct
-- reading of both rather than a duplicate, and it is why 00006's target_id
-- lookup excludes rows targeting the finding itself. `approve_finding` starts
-- returning a non-null id here, which is the first time "take the founder to
-- the new row" has ever been able to work.
--
-- ON CONFLICT
--
-- `action_type` refreshes alongside `obligation_id`, in the group that tracks
-- the obligation rather than the group ENT-60 preserves. Re-running the Analyst
-- after an obligation is classified should reclassify its open findings.
--
-- Note what this deliberately does NOT do: refreshing the column on a finding
-- that was already approved fires nothing, because the executor triggers are
-- `after update of status` and gated on the status transition. Records are
-- never created retroactively for decisions taken before the obligation was
-- classified, which is the safe direction to be wrong in.

alter table public.obligations
  add column action_type text not null default 'review';

-- The same four values `findings.action_type` allows. Stated again rather than
-- referenced because a check constraint cannot be shared, and left to drift the
-- two would eventually disagree in a way that only shows up as a trigger that
-- silently never fires.
alter table public.obligations
  add constraint obligations_action_type_check
  check (action_type = any (array['review', 'create_ropa', 'create_dsar', 'create_ai_system']));

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
    supporting_context, effort_estimate, action_type, metadata
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
    -- ENT-165: what approving this finding should do, from the obligation.
    v_obl.action_type,
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
    -- Tracks the obligation, so classifying one reclassifies its open findings.
    action_type           = excluded.action_type,
    metadata              = excluded.metadata,
    updated_at            = now()
  returning id into v_id;

  return v_id;
end;
$function$;
-- +goose StatementEnd

-- +goose Down

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

alter table public.obligations drop constraint obligations_action_type_check;
alter table public.obligations drop column action_type;
