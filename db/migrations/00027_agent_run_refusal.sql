-- +goose Up
-- 00027_agent_run_refusal.sql (ENT-248)
--
-- What an output critic refused, and which rule refused it.
--
-- WHY A COLUMN AND NOT A LONGER `outcome_detail`
--
-- Two live narrations on the 2B tier (PR #184) cited Article 30 correctly and
-- stated Article 30(5) backwards in the prose beside it. The citation validator
-- passed both, correctly: they cited the one obligation they were offered.
-- ENT-248 adds a claim critic that refuses a narrative asserting law, and asks
-- for the refusal to record both the rejected text and the pattern that fired.
--
-- Both could have been concatenated into `outcome_detail`, and that would have
-- been wrong in two directions at once.
--
-- `outcome_detail` is shown to the customer. `findings.narrative_refusal` is
-- copied from it (00022), and the finding page prints it under a heading saying
-- the Analyst's draft was refused. Putting the rejected text there would print
-- a false statement of law on the page whose whole job is to say that the false
-- statement was rejected. That is the sentence reaching the reader through a
-- different door, which is worse than not catching it, because the framing
-- makes it look reviewed.
--
-- And the pattern names would then only be recoverable by parsing English out
-- of a prose column. A maintainer asking how often the claim critic fires on
-- "regardless of" should be counting rows. That is exactly the mistake the
-- records store made with `check_violation` messages, which AGENTS.md names as
-- one of the reasons decisions moved out of plpgsql.
--
-- SO: THE SHORT REASON STAYS WHERE A CUSTOMER READS IT, AND THE EVIDENCE MOVES
--
-- `outcome_detail` keeps naming the rule and quoting the words around the
-- match, which is what somebody needs to act. This column holds the whole
-- rejected field and the machine-readable rule names, for whoever is asking
-- what the model actually wrote.
--
-- WHY IT IS NOT A CONSTRAINT-BEARING SHAPE
--
-- jsonb with an empty-object default, matching `tool_calls` and `citations`
-- beside it (00019). The shape is `{"critic": text, "patterns": [text],
-- "text": text}`, and it is not enforced here for the reason db/README.md
-- gives: this is a record of what one process observed, not an invariant that
-- must hold no matter who writes. A check constraint would make adding a third
-- critic a migration, and the critics live in Python where the shape is a
-- Pydantic model with a test.
--
-- `{}` rather than null so a reader can tell "no critic objected" from "a
-- critic objected and nobody said what to". Every existing row gets `{}`, which
-- is true of all of them: before this migration no critic recorded anything,
-- and the two runs that should have been refused were recorded as successes.
--
-- NO RLS CHANGE. `agent_runs` already has row level security enabled and
-- forced, with the two-GUC policies from 00019, and a column inherits them.
-- Adding a column changes no grant either: the table-level grants to
-- `kindlast_app` and `kindlast_agent` cover it.

alter table public.agent_runs
  add column refusal jsonb not null default '{}'::jsonb;

comment on column public.agent_runs.refusal is
  'What an output critic refused (ENT-248): {"critic", "patterns", "text"}. '
  'Empty object when no critic refused. The rejected text lives here rather '
  'than in outcome_detail because outcome_detail is shown to the customer, and '
  'a narrative refused for stating the law wrongly must not be printed under '
  'the heading explaining that it was refused.';

-- +goose Down
alter table public.agent_runs
  drop column refusal;
