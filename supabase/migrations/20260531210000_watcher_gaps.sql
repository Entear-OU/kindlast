-- Watcher profile-gap detector (ENT-56)
--
-- The second detector in the ENT-53 Watcher loop. ENT-55 watches the calendar;
-- this watches the *profile*. A static gap is an obligation that applies to the
-- org whose corresponding control is not in place — e.g. has_dpo != 'yes'
-- against the Article 37 DPO obligation, or operating an AI system with no
-- register entry. These surface as day-one findings without waiting on any
-- regulatory change.
--
-- Rules are data, not code (AC): an obligation opts into gap detection by
-- listing the controls it needs in `applies_when.requires` — a token array
-- alongside the existing applicability keys. Each token maps to one profile
-- signal in `watcher_gap_satisfied()`. Each detected gap maps to exactly one
-- obligation row (dedup_key = 'gap:obligation:<slug>'), so a single obligation
-- with several required controls still produces a single finding listing the
-- missing ones in metadata.
--
-- Applicability reuses the shared `watcher_obligation_applies()` predicate
-- (ENT-55): an obligation that doesn't apply to the profile can never raise a
-- gap, so the warn-by-default applicability and the gap check compose.
--
-- Re-surface (AC: "only re-surface a gap finding if previously rejected by the
-- user, with a different message"): a dismissed finding frees the open-finding
-- dedup key (the ENT-53 partial unique index is scoped to status='open'), so a
-- gap that is still present re-opens as a fresh finding on the next run. The
-- detector words that re-raise differently — "Recurring gap" instead of
-- "Profile gap" — when a prior dismissed finding exists for the same key.
--
-- Idempotent: `or replace` throughout; re-emission suppression for live
-- findings is inherited wholesale from ENT-53's emit_watcher_finding().
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Control-satisfaction predicate ───────────────────────────────────────────
--
-- "Is the control named by this requires-token in place for this profile?"
-- Unlike the applicability predicate, this is *not* warn-by-default: an
-- unrecognised token returns satisfied (true) so an unknown rule never
-- fabricates a gap finding (a false-positive gap is user-facing noise, where a
-- missed deadline is a safety risk — the two detectors weigh errors oppositely).
-- New tokens are added here as the profile schema grows the fields they map to.

create or replace function public.watcher_gap_satisfied(
  p_token   text,
  p_profile public.compliance_profiles
)
returns boolean
language plpgsql
immutable
set search_path = public, pg_temp
as $$
begin
  case p_token
    when 'ropa' then
      return p_profile.has_ropa = 'yes';
    when 'dpo' then
      return p_profile.has_dpo = 'yes';
    when 'ai_register' then
      -- Satisfied only when the org operates no AI systems. Using AI with no
      -- register entry is the gap; there is no dedicated register field yet, so
      -- "operates any AI system" stands in until one lands (ENT-56 owns the
      -- re-map). NULL/empty array ⇒ no AI ⇒ satisfied.
      return coalesce(array_length(p_profile.ai_systems, 1), 0) = 0;
    when 'transfer_safeguards' then
      -- Satisfied when at least one transfer destination is documented. Pairs
      -- with the cross_border_transfers applicability gate so this only matters
      -- for orgs that actually transfer outside the EU.
      return coalesce(array_length(p_profile.transfer_destinations, 1), 0) > 0;
    else
      -- Unknown token: ignore (treat as satisfied) so an unrecognised rule
      -- never raises a gap. Logged for visibility during corpus authoring.
      raise log 'watcher_gap_satisfied: unknown requires token %', p_token;
      return true;
  end case;
end;
$$;

-- 2. Gap detector ─────────────────────────────────────────────────────────────
--
-- One finding per applicable obligation that carries a `requires` array with at
-- least one unsatisfied control. metadata.missing lists the unsatisfied tokens;
-- metadata.recurring flags a re-surfaced (previously dismissed) gap.

create or replace function public.watcher_detect_gaps(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
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
$$;

-- 3. Register the detector into the daily loop ────────────────────────────────
--
-- Re-declares the per-profile entry point to invoke the gap detector after the
-- deadline detector (ENT-55) and before stamping the last-run timestamp.
-- ENT-57 (DSAR escalation) appends its own `perform` here.

create or replace function public.run_watcher_for_profile(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
begin
  -- Detectors (each calls public.emit_watcher_finding):
  perform public.watcher_detect_deadlines(p_profile_id);  -- ENT-55
  perform public.watcher_detect_gaps(p_profile_id);       -- ENT-56
  -- ENT-57 DSAR escalation registers here.

  update public.compliance_profiles
  set watcher_last_run_at = now()
  where id = p_profile_id;
end;
$$;
