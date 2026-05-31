-- Watcher DSAR escalation detector (ENT-57)
--
-- The third detector in the ENT-53 Watcher loop. ENT-55's deadline detector
-- already opens a `medium` DSAR finding (dedup_key 'dsar:<id>') for every
-- unanswered request whose response_due_at is within 30 days. ENT-57 layers the
-- GDPR Article 12(3) escalation on top: once fewer than 10 days remain — or the
-- deadline has already passed — the same finding is bumped to `critical` so a
-- client never silently runs out the one-month clock.
--
-- Escalation re-emits the *same* dedup key, so emit_watcher_finding() refreshes
-- the existing open finding in place (severity → critical, reworded title)
-- rather than opening a competing one. One finding per DSAR, its severity
-- tracking the clock; if ENT-55 hasn't run for some reason, the upsert still
-- opens the finding directly, so the detector is correct independent of order.
--
-- Threshold is strict: days_remaining < 10 ⇒ critical; exactly 10 days stays
-- medium (ENT-55's level). Day arithmetic matches ENT-55 exactly
-- (response_due_at::date − current_date) so the two detectors agree on
-- days_remaining for the same DSAR on the same run.
--
-- Re-emission suppression for the live finding is inherited wholesale from
-- ENT-53's open-finding partial unique index.
--
-- Idempotent: `or replace` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Escalation detector ──────────────────────────────────────────────────────
--
-- Iterates the profile owner's open/in_progress DSARs with no logged response
-- whose deadline is under 10 days away (negative = overdue) and re-emits the
-- finding at critical severity. The 30-day upper bound is intentionally absent:
-- an overdue DSAR is the most urgent case of all and must escalate even though
-- ENT-55's 30-day window technically still includes it.

create or replace function public.watcher_detect_dsar_escalation(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
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
    where user_id = v_profile.user_id
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
$$;

-- 2. Register the detector into the daily loop ────────────────────────────────
--
-- Runs after the deadline detector (ENT-55) so the medium DSAR finding exists
-- before this escalates the urgent subset, and after the gap detector (ENT-56).

create or replace function public.run_watcher_for_profile(p_profile_id uuid)
returns void
language plpgsql
security definer
set search_path = public, pg_temp
as $$
begin
  -- Detectors (each calls public.emit_watcher_finding):
  perform public.watcher_detect_deadlines(p_profile_id);        -- ENT-55
  perform public.watcher_detect_gaps(p_profile_id);             -- ENT-56
  perform public.watcher_detect_dsar_escalation(p_profile_id);  -- ENT-57

  update public.compliance_profiles
  set watcher_last_run_at = now()
  where id = p_profile_id;
end;
$$;
