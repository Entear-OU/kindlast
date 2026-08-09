-- Analyst writes the Executor's pre-fill payload (ENT-171)
--
-- The three Executor triggers (ENT-66/67/68) build their ratified record from
-- `findings.metadata -> 'payload'`, falling back to `new.detected` for the name
-- and to null for every other column:
--
--   coalesce(nullif(btrim(v_payload ->> 'name'), ''), new.detected)
--
-- Nothing ever wrote that payload. A grep for it across migrations, lib and app
-- returns only the three readers. So the fallback was not a fallback, it was the
-- only path, and every approved finding produced a blank row titled with the gap
-- sentence: a ROPA activity called "Profile gap: Records of Processing
-- Activities (ROPA)" with no legal basis, categories, recipients or retention.
-- The ROPA empty state promises the opposite ("The agent pre-fills a ratified
-- entry for each one"), and Billing sells it as a Pro feature.
--
-- The payload is built here, in `analyst_convert_signal`, because this is where
-- the obligation and the compliance profile are already in scope and the result
-- is deterministic and testable. Two rules govern what goes in it:
--
--   * Only facts the profile actually states. `data_categories` and the vendor
--     list are the founder's own answers, so they are carried through. Purpose,
--     legal basis and retention period are not derivable from onboarding, so
--     they stay null and the founder fills them. An invented legal basis in a
--     statutory register is worse than an empty one.
--   * A name that describes the thing being recorded. For a ROPA that is the
--     processing itself, derived from the profile's data subjects; for anything
--     else the obligation's own title. Never `detected`, which describes the
--     gap rather than the record.
--
-- Re-converting a signal refreshes `metadata` (the existing on-conflict clause),
-- so the next Watcher sweep repairs findings created before this migration
-- without a backfill.
--
-- Idempotent: `create or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- The founder's vendor list is free text from onboarding ("ChatGPT, GitHub
-- Copilot"). Split it into the recipients array the ROPA row wants, dropping
-- blanks so a trailing comma cannot produce an empty recipient.
create or replace function public.analyst_vendor_recipients(p_vendor_list text)
returns text[]
language sql
immutable
as $$
  select coalesce(
    array(
      select btrim(v)
      from unnest(string_to_array(coalesce(p_vendor_list, ''), ',')) as v
      where btrim(v) <> ''
    ),
    '{}'::text[]
  );
$$;

-- The name for the record the Executor will create. A ROPA entry describes a
-- processing activity, so it is named after who the data is about; everything
-- else is named after the obligation.
create or replace function public.analyst_payload_name(
  p_action_type   text,
  p_obligation    text,
  p_data_subjects text[],
  p_industry      text
)
returns text
language sql
immutable
as $$
  select case
    when p_action_type = 'create_ropa' and coalesce(array_length(p_data_subjects, 1), 0) > 0
      then 'Processing of ' || array_to_string(p_data_subjects, ', ') || ' data'
    when p_action_type = 'create_ropa' and coalesce(btrim(p_industry), '') <> ''
      then btrim(p_industry) || ' processing'
    else p_obligation
  end;
$$;

create or replace function public.analyst_convert_signal(p_signal_id uuid)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_sig     public.watcher_findings;
  v_obl     public.obligations;
  v_profile public.compliance_profiles;
  v_action  text;
  v_payload jsonb;
  v_id      uuid;
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

  select * into v_profile from public.compliance_profiles where id = v_sig.profile_id;

  v_action := case v_sig.kind
    when 'deadline'          then 'Review this obligation and prepare to meet its upcoming deadline.'
    when 'profile_gap'       then 'Put the missing control in place to satisfy this obligation.'
    when 'dsar'              then 'Prepare and log a response to this data-subject request before its deadline.'
    when 'regulatory_update' then 'Review this regulatory update and assess its impact on your obligations.'
    else                          'Review this finding and take the appropriate action.'
  end;

  -- ENT-171: the Executor's pre-fill. Facts only; unknowable fields stay absent
  -- so the register shows them as empty rather than as a guess.
  v_payload := jsonb_strip_nulls(jsonb_build_object(
    'name', public.analyst_payload_name(
      v_obl.action_type, v_obl.title, v_profile.data_subjects, v_profile.industry
    ),
    'data_categories', to_jsonb(coalesce(v_profile.data_categories, '{}'::text[])),
    'recipients',      to_jsonb(public.analyst_vendor_recipients(v_profile.vendor_list))
  ));

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
      (v_sig.metadata ->> 'days_remaining')::int, v_profile.data_categories
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
      'signal_metadata',  v_sig.metadata,
      'payload',          v_payload
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
