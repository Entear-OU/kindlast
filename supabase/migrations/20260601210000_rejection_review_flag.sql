-- Repeated rejection → one-time product-review flag (ENT-65, PRD §14 Q4)
--
-- ENT-63 made each finding actionable; reject persists the founder's optional
-- reason but does nothing else with it. ENT-65 closes the feedback loop: when a
-- founder keeps rejecting the *same condition* the product should hear about it.
-- A condition the founder rejects over and over is a signal the product is wrong
-- about that user — a false positive the Analyst keeps re-raising, or an
-- obligation that genuinely doesn't apply. The third rejection of one condition
-- is the threshold at which a human on the product team should look (PRD §14 Q4:
-- "after the third rejection of the same finding, raise it for product review").
--
-- "Same condition" is (profile_id, obligation_slug): one user's stance on one
-- obligation, not one specific finding row. The Analyst emits a fresh finding
-- per sweep (a distinct watcher signal each time), so counting findings rejected
-- under the same slug is what captures "they keep saying no to this".
--
-- Design — mirrors the Executor audit log (ENT-69):
--
--   * The flag is internal product evidence, raised by the system, never edited.
--     Like audit_log it is owner-readable (RLS select-own, for completeness) but
--     has NO insert/update/delete policy: the sole writer is the SECURITY DEFINER
--     reject_finding() below (which bypasses RLS), and the product team reads it
--     via the service role. A BEFORE UPDATE immutability guard makes the row
--     unchangeable even to the service role / definer functions — the audit_log
--     precedent, applied here so a "raised once" flag can't be silently rewritten.
--   * `finding_id` is a soft reference (plain uuid, no FK), exactly as
--     audit_log.finding_id and processing_activities.finding_id (ENT-66): the
--     provenance must outlive a finding that is later resolved or purged, and an
--     ON DELETE cascade/SET NULL would itself be an UPDATE the guard rejects.
--   * `unique (profile_id, obligation_slug)` makes "raise at most once per
--     condition" a database invariant; reject_finding inserts ON CONFLICT DO
--     NOTHING so the third rejection raises it and the fourth+ are no-ops.
--
-- reject_finding() is REDECLARED here (create or replace) to add the flag logic.
-- Its signature and existing behaviour are unchanged: SECURITY DEFINER, actor =
-- auth.uid(), scoped to the caller's user_id, only fires when status <> rejected,
-- still returns whether a row changed. The flag is a pure side effect on the
-- third rejection.
--
-- Idempotent: `create table if not exists`, `create or replace`, `drop … if
-- exists` + create, so the migration re-applies cleanly in local dev.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Product-review flag store ─────────────────────────────────────────────────
--
-- One row per (profile_id, obligation_slug) the founder has rejected ≥ 3 times.
--
--   * `obligation_slug`  — the condition's natural key (text, as on findings).
--                          Stable across corpus re-ingests even when the
--                          catalogue obligation_id changes.
--   * `finding_id`       — the finding whose rejection tripped the flag. Soft
--                          reference (no FK): provenance that should outlive a
--                          purged finding, mirroring audit_log /
--                          processing_activities.finding_id (ENT-66).
--   * `rejection_count`  — how many findings for this condition were rejected
--                          when the flag was raised (≥ 3).
--   * `reasons`          — the distinct, non-null rejection reasons the founder
--                          left across those findings, so the product team has
--                          the *why* in one place.

create table if not exists public.product_review_flags (
  id              uuid        primary key default gen_random_uuid(),
  user_id         uuid        not null references auth.users(id) on delete cascade,
  profile_id      uuid        not null
                    references public.compliance_profiles(id) on delete cascade,
  obligation_slug text        not null,
  finding_id      uuid,
  rejection_count int         not null,
  reasons         text[]      not null default '{}',
  created_at      timestamptz not null default now(),
  unique (profile_id, obligation_slug)  -- raised at most once per condition
);

-- 2. Immutability guard ───────────────────────────────────────────────────────
--
-- Rejects every UPDATE on the table, for every role — RLS denies UPDATE to the
-- owner role by default, but the service role and SECURITY DEFINER functions
-- bypass RLS, so this trigger is what makes a raised flag immutable to *them*
-- too. DELETE is deliberately not guarded (retention/cleanup prunes whole rows,
-- not a silent mutation of a flag's content), matching the audit_log precedent.

create or replace function public.product_review_flags_forbid_update()
returns trigger
language plpgsql
as $$
begin
  raise exception 'product_review_flags is append-only: UPDATE on row % is not permitted', old.id
    using errcode = 'check_violation';
end;
$$;

drop trigger if exists product_review_flags_no_update on public.product_review_flags;
create trigger product_review_flags_no_update
  before update on public.product_review_flags
  for each row execute function public.product_review_flags_forbid_update();

-- 3. Row-level security ────────────────────────────────────────────────────────
--
-- Owner role: SELECT of its own rows, for completeness (a founder could see a
-- flag raised on their account). The absence of INSERT / UPDATE / DELETE
-- policies is the enforcement — RLS denies by default — so the only writer is
-- the SECURITY DEFINER reject_finding() below, and the product team reads via
-- the service role.

alter table public.product_review_flags enable row level security;

drop policy if exists "product_review_flags_select_own" on public.product_review_flags;
create policy "product_review_flags_select_own" on public.product_review_flags
  for select using (auth.uid() = user_id);

-- 4. reject_finding (redeclared) ───────────────────────────────────────────────
--
-- Unchanged behaviour: status → rejected, persist the (trimmed, optional) reason,
-- only when the caller owns the finding and it isn't already rejected; returns
-- whether a row changed. ADDED: on a real status flip, count how many findings
-- for this (profile_id, obligation_slug) are now rejected and, on the third,
-- raise the product-review flag exactly once.
create or replace function public.reject_finding(
  p_finding_id uuid,
  p_reason     text default null
)
returns boolean
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user    uuid := auth.uid();
  v_updated uuid;
  v_profile uuid;
  v_slug    text;
  v_count   int;
  -- The third rejection of the same condition is when the product team should
  -- look (PRD §14 Q4). Defined inline — no separate function needed.
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
    and user_id = v_user
    and status <> 'rejected'
  returning id, profile_id, obligation_slug
    into v_updated, v_profile, v_slug;

  -- Raise the product-review flag on the third rejection of this condition.
  -- A finding with no slug has no condition to aggregate on, so it's skipped.
  if v_updated is not null and v_slug is not null then
    select count(*)
      into v_count
      from public.findings
     where profile_id = v_profile
       and obligation_slug = v_slug
       and status = 'rejected';

    if v_count >= c_threshold then
      insert into public.product_review_flags (
        user_id, profile_id, obligation_slug, finding_id, rejection_count, reasons
      )
      values (
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
      on conflict (profile_id, obligation_slug) do nothing;  -- raised exactly once
    end if;
  end if;

  return v_updated is not null;
end;
$$;
