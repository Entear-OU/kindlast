-- Wire the Executor: carry an action_type from the obligation onto the finding (ENT-165)
--
-- `findings.action_type` is the discriminator all three Executor triggers key
-- off (ENT-66 ROPA, ENT-67 DSAR, ENT-68 AI systems). It was declared with
-- `not null default 'review'` and a CHECK listing the four values, and then
-- nothing ever wrote it. `analyst_convert_signal` inserted findings without
-- setting it, so every finding in production carried 'review', every trigger's
-- WHEN clause was permanently false, and the whole Executor was unreachable.
--
-- Approving a finding therefore only flipped a status. No record was created and
-- no audit entry was written, while the ROPA empty state told the founder "your
-- ROPA fills up as you approve findings" and the billing page sold "one-tap
-- Executor actions" as the headline Pro feature.
--
-- The fix puts the mapping where the rest of the per-obligation policy already
-- lives: the obligations catalogue, alongside `severity`, `due_within_days` and
-- `recurrence`. `analyst_convert_signal` then copies it onto the finding, the
-- same way it already copies the citation fields and the summary.
--
-- Idempotent: `add column if not exists`, `drop constraint if exists`, an UPDATE
-- keyed on the natural slug, and `create or replace function`.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. obligations.action_type ──────────────────────────────────────────────────
--
-- Same value domain and default as `findings.action_type`, so the copy in
-- `analyst_convert_signal` can never violate the finding's own CHECK.

alter table public.obligations
  add column if not exists action_type text not null default 'review';

alter table public.obligations drop constraint if exists obligations_action_type_check;
alter table public.obligations
  add constraint obligations_action_type_check
  check (action_type in ('review', 'create_ropa', 'create_dsar', 'create_ai_system'));

comment on column public.obligations.action_type is
  'Which compliance record the Executor ratifies when a finding for this obligation is approved. '
  '''review'' (the default) means no record is created and approval only records the decision.';

-- 2. The mapping ──────────────────────────────────────────────────────────────
--
-- Only the Art. 30 ROPA gap is mapped here, and deliberately so.
--
-- `create_ropa` is unambiguous: a ROPA-gap finding approved by the founder
-- should ratify the processing-activity record that the gap is about, which is
-- exactly what the ENT-66 trigger does and exactly what the ROPA empty state
-- promises.
--
-- `create_dsar` is NOT mapped from the catalogue. The ENT-67 trigger opens a new
-- `dsars` row with a 30-day Art. 12(3) countdown, which is the right response to
-- an actual incoming data-subject request, not to a standing profile gap against
-- the data-subject-rights obligation. Mapping the obligation would open a
-- spurious DSAR every time that gap were approved.
--
-- `create_ai_system` is left unmapped for the same kind of reason: the ENT-68
-- trigger ratifies a specific system's risk classification and carries a
-- reviewed-approval gate for high-risk classes, so which obligation (if any)
-- should mint a register entry is a product decision rather than a mechanical
-- one. The mechanism below supports it the moment that call is made: set
-- `action_type` on the obligation row.

update public.obligations
   set action_type = 'create_ropa'
 where slug = 'gdpr-art-30-ropa'
   and action_type <> 'create_ropa';

-- 3. analyst_convert_signal ───────────────────────────────────────────────────
--
-- Unchanged from the ENT-63 revision except for the action_type copy: it is set
-- on INSERT and refreshed on the idempotent re-convert, so retagging an
-- obligation takes effect on the next Watcher run without a backfill.

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
    supporting_context, effort_estimate, action_type, metadata
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
    action_type           = excluded.action_type,
    metadata              = excluded.metadata,
    updated_at            = now()
  returning id into v_id;

  return v_id;
end;
$$;
