-- +goose Up
-- 00023_watcher_reads_profile_facts.sql (ENT-246)
--
-- Article 35's DPIA stops applying to every controller.
--
-- WHAT WAS WRONG
--
-- `watcher_obligation_applies` implemented one threshold no obligation writes
-- (`employees_min`) and ignored three that four obligations do write
-- (`high_risk`, `large_scale_monitoring`, `lawful_basis_includes`). An
-- unrecognised condition narrows nothing, so each of those obligations bound
-- every organisation the role gate let through. In practice that meant the
-- product telling a five-person controller doing nothing high-risk that it owes
-- a Data Protection Impact Assessment, and telling every controller alive that
-- it must designate a Data Protection Officer.
--
-- A DPIA is expensive and a DPO is a hire. AGENTS.md opens by saying a
-- fabricated obligation is worse than nothing, because the product's value is
-- that a human can check the claim against the law. This one was generated
-- deterministically rather than by a model, which makes it worse rather than
-- better: nobody was going to review it.
--
-- ENT-233 made the drift visible (the vocabulary is declared in
-- `domain/corpus/applieswhen.go` and pinned by a test against these functions).
-- This closes it.
--
-- WHERE THE ANSWERS COME FROM
--
-- All three conditions are properties of what an organisation DOES. The legacy
-- `compliance_profiles` records only properties of what it IS, which is why
-- these were unevaluated rather than merely unimplemented: there was no column
-- to read. ENT-228's `org_profile_facts` is the store for exactly this, keyed
-- by a vocabulary that lives in Go so a new question is not a migration, and
-- ENT-233's own notes named it as where these questions had to go.
--
-- So the four new facts (`high_risk_processing`, `high_risk_ai_system`,
-- `large_scale_monitoring`, `lawful_bases`) are added in Go, in the proto enum
-- and `domain/memory`, and this migration adds no column and no constraint. It
-- teaches the evaluator to read them.
--
-- WHY THE EVALUATOR IS STILL PLPGSQL, WHICH IS NOT WHERE DECISIONS BELONG
--
-- Applicability is a decision by db/README.md's test, and decisions are Go's.
-- But the only caller is `run_watcher()`, a plpgsql sweep that iterates
-- profiles and obligations in the database, and moving that wholesale is
-- ENT-225's, which is exactly the shape of change that must not be bundled into
-- a fix. The alternative available here was to write a second evaluator in Go
-- that nothing calls, and two implementations of one rule, one of them
-- decorative, is the arrangement that produced this bug.
--
-- What keeps the language boundary honest in the meantime is the declaration in
-- `domain/corpus/applieswhen.go` and `corpus_vocabulary_test.go`, which asserts
-- every declared token against these running functions rather than trusting
-- either file. ENT-225 inherits both.
--
-- AN ABSENT FACT MEANS THE OBLIGATION DOES NOT APPLY
--
-- The direction matters more than the mechanism, so it is written down in both
-- places. Asserting a DPIA from silence is the fabricated obligation above:
-- nobody asked, so there are no grounds. The mirror risk, an organisation that
-- does do high-risk processing no longer being told about Article 35, is real
-- and is one answer away from being fixed: the fact is editable on the memory
-- page today and onboarding writes it at ENT-212.
--
-- `unsure` counts as applying. That is not a rounding of "no": ENT-228 kept
-- `unsure` as its own answer because "we asked and they did not know" is a
-- different claim from "they said no", and an organisation that does not know
-- whether its processing is high-risk has not done the Article 35(1) screening
-- the obligation exists for.
--
-- HIGH RISK WAS TWO QUESTIONS WEARING ONE TOKEN
--
-- `thresholds.high_risk` was written by two GDPR obligations and two AI Act
-- obligations. GDPR Article 35 asks whether the PROCESSING is likely to result
-- in a high risk to people's rights. The AI Act asks whether an AI SYSTEM falls
-- within Annex III. Different tests, different regulations, and one shared
-- answer would have meant a controller's answer about profiling deciding
-- whether Annex III bound it. The token is split, in the corpus and here, and
-- the stored rows are rewritten below so a stack that never re-ingests still
-- gets the fix.
--
-- The AI Act half could have been answered from `ai_systems.risk_classification`
-- instead of by asking, since that register already records which systems are
-- High-Risk. It is not, deliberately: `kindlast_agent` holds no grant on
-- `ai_systems` (00008), and widening the producer role's reach is a change to a
-- security boundary that deserves its own review rather than a ride on a fix.
--
-- VOLATILITY CHANGES, AND THAT IS THE ONE THING TO NOTICE ON REVIEW
--
-- The function was IMMUTABLE, which was true when it read only its arguments
-- and is a lie the moment it reads a table. IMMUTABLE lets the planner fold a
-- call to a constant, so a stale answer would be a correct optimisation of an
-- incorrect declaration. STABLE is what "reads the database, consistent within
-- one statement" means, and a sweep is one statement's worth of consistency.
--
-- Row level security still applies: this is a SECURITY INVOKER function, and
-- the producer reads `org_profile_facts` under the `org_profile_facts_agent`
-- policy 00020 already granted it. No grant, policy or role changes here.

-- +goose StatementBegin
create function public.watcher_fact_affirms(p_org uuid, p_key text)
returns boolean
language sql
stable
set search_path to 'public', 'pg_temp'
as $$
  -- Affirmed means the organisation told us yes, or told us it does not know.
  -- Absent is not affirmed, and neither is 'no'. See the header.
  select exists (
    select 1
    from public.org_profile_facts f
    where f.org_id  = p_org
      and f.key     = p_key
      and f.valid_to is null
      and f.value in ('"yes"'::jsonb, '"unsure"'::jsonb)
  );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function public.watcher_obligation_applies(p_applies_when jsonb, p_profile public.compliance_profiles) RETURNS boolean
    LANGUAGE plpgsql STABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
declare
  v_role  text := p_applies_when ->> 'role';
  v_basis text := p_applies_when ->> 'lawful_basis_includes';
begin
  -- role
  if v_role in ('deployer', 'provider') then
    if coalesce(array_length(p_profile.ai_systems, 1), 0) = 0 then
      return false;
    end if;
  end if;
  -- 'controller' (and absent role) impose no role restriction.

  -- cross-border transfers
  if coalesce((p_applies_when #>> '{thresholds,cross_border_transfers}')::boolean, false) then
    if p_profile.transfers_outside_eu is distinct from 'yes' then
      return false;
    end if;
  end if;

  -- High-risk processing, GDPR Article 35(1) and Article 34's "high risk to
  -- the rights and freedoms of natural persons".
  if coalesce((p_applies_when #>> '{thresholds,high_risk_processing}')::boolean, false) then
    if not public.watcher_fact_affirms(p_profile.org_id, 'high_risk_processing') then
      return false;
    end if;
  end if;

  -- High-risk AI system, the AI Act's Annex III classification. A separate
  -- question from the one above and never inferred from it.
  if coalesce((p_applies_when #>> '{thresholds,high_risk_ai_system}')::boolean, false) then
    if not public.watcher_fact_affirms(p_profile.org_id, 'high_risk_ai_system') then
      return false;
    end if;
  end if;

  -- Regular and systematic monitoring of data subjects on a large scale,
  -- Article 37(1)(b).
  if coalesce((p_applies_when #>> '{thresholds,large_scale_monitoring}')::boolean, false) then
    if not public.watcher_fact_affirms(p_profile.org_id, 'large_scale_monitoring') then
      return false;
    end if;
  end if;

  -- Lawful basis, for obligations that bind only where a particular Article 6
  -- basis is relied on. Containment rather than equality: an organisation
  -- relies on several, and Article 7 binds when consent is among them.
  if v_basis is not null then
    if not exists (
      select 1
      from public.org_profile_facts f
      where f.org_id  = p_profile.org_id
        and f.key     = 'lawful_bases'
        and f.valid_to is null
        and f.value @> to_jsonb(v_basis)
    ) then
      return false;
    end if;
  end if;

  -- engages a processor
  if coalesce((p_applies_when ->> 'engages_processor')::boolean, false) then
    if coalesce(btrim(p_profile.vendor_list), '') = '' then
      return false;
    end if;
  end if;

  -- `employees_min` is deliberately not evaluated any more, and is not in the
  -- vocabulary. No obligation ever used it. The one it looks like it should
  -- serve is Article 30's ROPA, whose 250-employee exemption is narrow enough
  -- that the curated summary tells the reader most SMEs cannot rely on it, so a
  -- headcount threshold sitting here invited somebody to encode Article 30(5)
  -- as `employees_min: 250` and exempt organisations the Article does not.
  return true;
end;
$$;
-- +goose StatementEnd

-- The stored corpus, renamed to match.
--
-- A stack that has not re-ingested `data/corpus/` still holds the seeded rows
-- from 00001, which say `high_risk`. That token is now unrecognised, and an
-- unrecognised token narrows nothing, so leaving these rows alone would leave
-- the bug in place for exactly the deployments least likely to notice. The
-- split follows the citation because the question does: 32016R0679 is GDPR,
-- 32024R1689 is the AI Act.
update public.obligations
set applies_when = jsonb_set(
      applies_when #- '{thresholds,high_risk}',
      '{thresholds,high_risk_processing}',
      applies_when #> '{thresholds,high_risk}')
where applies_when #> '{thresholds,high_risk}' is not null
  and citation_celex = '32016R0679';

update public.obligations
set applies_when = jsonb_set(
      applies_when #- '{thresholds,high_risk}',
      '{thresholds,high_risk_ai_system}',
      applies_when #> '{thresholds,high_risk}')
where applies_when #> '{thresholds,high_risk}' is not null
  and citation_celex <> '32016R0679';

-- +goose Down

-- The rename, reversed. Both halves collapse back onto the one token, which is
-- what the pre-ENT-246 vocabulary had.
update public.obligations
set applies_when = jsonb_set(
      applies_when #- '{thresholds,high_risk_processing}',
      '{thresholds,high_risk}',
      applies_when #> '{thresholds,high_risk_processing}')
where applies_when #> '{thresholds,high_risk_processing}' is not null;

update public.obligations
set applies_when = jsonb_set(
      applies_when #- '{thresholds,high_risk_ai_system}',
      '{thresholds,high_risk}',
      applies_when #> '{thresholds,high_risk_ai_system}')
where applies_when #> '{thresholds,high_risk_ai_system}' is not null;

-- +goose StatementBegin
create or replace function public.watcher_obligation_applies(p_applies_when jsonb, p_profile public.compliance_profiles) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
declare
  v_role  text  := p_applies_when ->> 'role';
  v_min   int   := (p_applies_when #>> '{thresholds,employees_min}')::int;
begin
  -- role
  if v_role in ('deployer', 'provider') then
    if coalesce(array_length(p_profile.ai_systems, 1), 0) = 0 then
      return false;
    end if;
  end if;
  -- 'controller' (and absent role) impose no role restriction.

  -- cross-border transfers
  if coalesce((p_applies_when #>> '{thresholds,cross_border_transfers}')::boolean, false) then
    if p_profile.transfers_outside_eu is distinct from 'yes' then
      return false;
    end if;
  end if;

  -- employee threshold (NULL staff_count is treated as "unknown ⇒ applicable")
  if v_min is not null and p_profile.staff_count is not null
     and p_profile.staff_count < v_min then
    return false;
  end if;

  -- engages a processor
  if coalesce((p_applies_when ->> 'engages_processor')::boolean, false) then
    if coalesce(btrim(p_profile.vendor_list), '') = '' then
      return false;
    end if;
  end if;

  return true;
end;
$$;
-- +goose StatementEnd

drop function if exists public.watcher_fact_affirms(uuid, text);
