-- The Analyst: preserve generated narrative across re-conversion (ENT-60)
--
-- ENT-60 adds an LLM pass that rewrites a finding's `detected` (plain-language
-- description) and `proposed_action` (a specific, verb-led step) — generated in
-- TypeScript (lib/analyst/*) and gated by a deterministic critic. The generation
-- itself lives in app code; this migration is the one piece of the contract that
-- belongs in the database:
--
--   * `narrative_generated_at` records when the LLM narrative was persisted, so
--     a later batch runner can find findings still showing the baseline.
--   * `analyst_convert_signal` must stop clobbering the narrative. The Watcher's
--     daily loop re-converts every open signal, and ENT-58/59's upsert refreshed
--     `detected`/`proposed_action` from the signal each time. Once a narrative is
--     generated that would wipe it on the next run. So the on-conflict path now
--     PRESERVES detected / proposed_action / narrative_generated_at, while still
--     refreshing the signal-derived fields (severity, citation, supporting
--     context, metadata). On first INSERT they still get the ENT-58 baseline.
--
-- Idempotent: `add column if not exists` + `create or replace`.
-- ─────────────────────────────────────────────────────────────────────────────

alter table public.findings
  add column if not exists narrative_generated_at timestamptz;

-- Re-declare the conversion (ENT-59 body) with a narrative-preserving conflict
-- clause. Only the `on conflict … do update set` list changed: detected,
-- proposed_action, and narrative_generated_at are no longer overwritten.
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
    v_sig.severity,
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
    'hours',
    jsonb_build_object(
      'signal_kind',      v_sig.kind,
      'signal_dedup_key', v_sig.dedup_key,
      'signal_metadata',  v_sig.metadata
    )
  )
  on conflict (watcher_finding_id) do update set
    -- detected / proposed_action / narrative_generated_at are PRESERVED: once the
    -- ENT-60 LLM pass has written the founder-facing narrative, a daily
    -- re-conversion must not revert it to the baseline. Signal-derived fields
    -- still refresh.
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
