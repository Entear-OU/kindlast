-- +goose Up
-- 00022_finding_narrative.sql (ENT-245, ENT-162, ENT-164, §26.3)
--
-- Somewhere for a drafted narrative to go, and a pointer to the run that
-- produced it.
--
-- THE NARRATIVE IS A NEW COLUMN, NOT A REWRITE OF AN OLD ONE
--
-- This is the fix for ENT-164 and it is structural rather than cosmetic.
--
-- The narrative layer that existed before the console was removed wrote model
-- prose over `detected`, which the feed card renders as its HEADING. So a card
-- whose heading was a short phrase before the Analyst ran became a paragraph
-- after it, and the bug reads as a rendering problem when it is a schema one:
-- there was no slot for prose, so prose went into the slot for a phrase.
--
-- `detected`, `proposed_action`, `regulatory_obligation` and `supporting_
-- context` stay exactly as the deterministic sweep writes them. The model adds
-- and never overwrites. Three things follow that are worth having:
--
--   * A refused run costs nothing. The card is what it is today, because the
--     text it renders was never the model's to begin with (§26.3 makes refusal
--     what a working guardrail produces, so it has to be cheap).
--   * A deployment with no model configured is not a degraded one. Intelligence
--     sits behind a compose profile; without it every finding still has its
--     baseline text.
--   * "What did the model actually contribute" is answerable by looking at one
--     column rather than by diffing against a sweep nobody kept.

alter table public.findings
  -- The plain-language explanation, when a run produced one and every citation
  -- in it resolved. Null is the normal state, not a missing value: most
  -- findings have not been narrated, and a deployment may never narrate any.
  add column narrative text,

  -- The run that produced it, so "how this was produced" resolves from the
  -- card.
  --
  -- A FOREIGN KEY, AND `on delete set null` RATHER THAN CASCADE. Losing the
  -- run record must not delete the finding: the finding is the thing a
  -- customer acts on, and the provenance is evidence about it. Cascade here
  -- would let a retention job on `agent_runs` quietly remove compliance
  -- findings, which is the worst possible way to discover a retention policy.
  add column agent_run_id uuid references public.agent_runs(id) on delete set null,

  -- Why, when a run was made and refused. Free text for a human.
  --
  -- STORED RATHER THAN DISCARDED, because "we tried and the model cited an
  -- article that does not apply to you" is a fact a customer deciding whether
  -- to trust this product should be able to see. A refusal that leaves no
  -- trace is indistinguishable from never having run, and those are different
  -- things.
  add column narrative_refusal text;

-- `narrative_generated_at` already exists, from the era this is replacing. It
-- is left alone and reused rather than added again: it means the same thing
-- and a second timestamp beside it would be two answers to one question.

-- Findings waiting to be narrated, which is the query the narrator runs on
-- every pass. Partial, because the rows it wants are a shrinking minority of
-- the table and an index over all of them would be mostly dead weight.
create index findings_awaiting_narrative_idx
  on public.findings (org_id, created_at)
  where narrative is null and narrative_refusal is null;

------------------------------------------------------------------------------
-- Grants
------------------------------------------------------------------------------
-- `kindlast_agent` already holds `select, insert, update` on `findings` from
-- 00008, so the narrator can write these columns with no new grant.
--
-- THAT IS WIDER THAN THIS JOB NEEDS AND IT IS DELIBERATELY NOT NARROWED HERE.
--
-- The narrow version is available and is the same trick ENT-228 uses on
-- `org_profile_facts`: revoke the blanket update and grant
-- `update (narrative, agent_run_id, narrative_refusal,
-- narrative_generated_at)`, so the narrator could add prose and could never
-- change a severity, a status or a citation. That is a real improvement and it
-- is the right eventual shape.
--
-- It is not done in this migration because `run_analyst()` and the act-path
-- functions also write this table, and whether they do so as the caller or as
-- their definer has to be established before the blanket grant is removed.
-- Getting that wrong breaks the sweep for every deployment, and the failure
-- would appear as findings silently not being created. ENT-225 is auditing
-- those functions' privileges, and the narrowing belongs with it.
--
-- Written down rather than left as a TODO because a grant nobody questions is
-- a grant that survives forever.

-- The console reads the new columns through the existing select grant. Nothing
-- to add: 00002's default privileges cover columns added later to a table
-- `kindlast_app` already holds `select` on.

-- +goose Down
drop index if exists public.findings_awaiting_narrative_idx;
alter table public.findings
  drop column if exists narrative_refusal,
  drop column if exists agent_run_id,
  drop column if exists narrative;
