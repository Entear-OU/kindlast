-- +goose Up
-- 00019_agent_runs.sql (ENT-218, §26.3, §26.4)
--
-- One row per agent run: what was asked, which skill and model answered, every
-- tool call, every citation resolved and rejected, what it cost, and how it
-- ended.
--
-- WHAT THIS IS FOR, WHICH IS NOT DEBUGGING
--
-- §26 requires that a run "leaves a record a customer can read". Not an
-- operator, a customer. The product's claim is that a human can check a finding
-- against the law, and a finding produced by a model is only checkable if the
-- customer can see what it was given, what it consulted and what it refused.
-- "How this was produced" on a finding reads from here.
--
-- That is why this is a domain table under RLS rather than a trace in an
-- observability tool. A record whose completeness depends on a vendor's
-- retention settings is not a record the customer owns, which is the same
-- argument §7.2 makes for keeping `audit_log` out of the tracing stack.
--
-- APPEND-ONLY BY SHAPE, NOT YET BY TRIGGER
--
-- A row is written ONCE, when the run ends, carrying both timestamps. There is
-- no insert-at-start-then-update-at-finish, and no update path at all.
--
-- The alternative would let a run amend its own history, which is the property
-- `audit_log` exists to deny and which matters here for the same reason: the
-- record is evidence about a decision, and evidence a producer can revise after
-- the fact is worth less than evidence it cannot.
--
-- The cost is that an in-flight run is invisible until it finishes. That is
-- acceptable at v0 volumes and is ENT-228's to revisit if a console wants a
-- progress indicator. `queued_at` is here from the start so ENT-238's queue
-- wait is a subtraction rather than a migration.
--
-- No append-only trigger yet, deliberately: `audit_log` has one because the
-- application holds an insert path and a policy that could be widened. Here
-- there is no update grant and no update policy for anybody, so the trigger
-- would be a third lock on a door with no handle. Add it the day an update
-- path is proposed, not before.
--
-- MINIMAL ON PURPOSE, AND ENT-228 GROWS IT
--
-- §26.4 gives this table a versioned profile reference, normalised evidence
-- rows and a link from the audit row of anything produced. None of that is
-- here. What is here is the shape those grow into: `tool_calls` and
-- `citations` are jsonb precisely so ENT-228 can normalise them into rows
-- without this table's other columns changing meaning.

-- +goose StatementBegin
create table public.agent_runs (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- WHAT RAN.
  --
  -- Skill and model are recorded WITH THEIR VERSIONS because a run is only
  -- reproducible if you know which version answered. §26 pins skills and
  -- models precisely so this column can be trusted; a bare name here would
  -- record that "the analyst skill" ran, which is not a fact anybody can act
  -- on a year later.
  --
  -- `model` is a free string rather than an enum because it names whatever
  -- served the request, which after ENT-235 is a local GGUF and may later be a
  -- hosted provider. `model_version` for a local model is the file's digest,
  -- not a marketing version.
  skill text not null,
  skill_version text not null,
  model text not null,
  model_version text not null,

  -- WHO IT WAS FOR.
  --
  -- Nullable, and the nullability is the point. A scheduled sweep runs for the
  -- organisation and for no particular person; a narrative drafted because
  -- somebody clicked runs on behalf of that person. Recording a user id on the
  -- first kind would be a lie that later reads as "this person asked for this".
  --
  -- Not a foreign key, matching every other user reference in this schema:
  -- identity is Zitadel's and the domain mirrors rather than owns it.
  on_behalf_of_user_id uuid,

  -- WHAT WAS ASKED, AND WHAT CAME BACK.
  --
  -- `request` is the input the run was given, not the assembled prompt. The
  -- prompt includes the corpus prefix and is reconstructible from skill
  -- version plus request; storing it per run would duplicate the corpus into
  -- every row.
  request jsonb not null default '{}'::jsonb,

  -- Ordered tool calls with arguments and result summaries, and the citations
  -- the run produced, split into those that resolved and those refused.
  --
  -- jsonb rather than tables because ENT-228 owns the normalised shape and
  -- guessing it now would mean migrating twice. The contract that survives
  -- either way: a reader can see every tool call in order, and can see that a
  -- citation was REFUSED rather than only that it is absent. A validator that
  -- silently dropped a bad citation would leave a record indistinguishable
  -- from one where the model never tried.
  tool_calls jsonb not null default '[]'::jsonb,
  citations jsonb not null default '{"resolved": [], "rejected": []}'::jsonb,

  -- HOW IT ENDED.
  --
  -- `refused` is a first-class outcome, not a failure. §26.3 makes refusal
  -- what a guardrail produces when a budget is exhausted, a citation does not
  -- resolve, or a tool outside the skill's allow-list is requested. A schema
  -- that offered only succeeded and failed would push refusals into one or the
  -- other and lose the distinction that matters most for trust.
  outcome text not null,
  constraint agent_runs_outcome_check
    check (outcome in ('succeeded', 'refused', 'failed')),

  -- Why, when it was not `succeeded`. Free text for a human, not a code to
  -- branch on.
  outcome_detail text,

  -- WHAT IT COST.
  --
  -- Cached input is separate because it is priced separately by every provider
  -- that offers it, and because with a local model it is the measurement that
  -- shows the corpus prefix is being reused rather than reprocessed (§26).
  input_tokens integer not null default 0,
  cached_input_tokens integer not null default 0,
  output_tokens integer not null default 0,

  -- Micros rather than a float. Money in a float is a bug waiting for a
  -- reconciliation. Zero for a local model, which is a true statement about
  -- marginal cost rather than a missing value.
  cost_micros bigint not null default 0,

  -- WHEN.
  --
  -- Three timestamps, because ENT-238 needs to tell "slow because the model is
  -- slow" from "slow because it waited in a queue", and those are different
  -- problems with different fixes. queued_at to started_at is wait;
  -- started_at to finished_at is work.
  queued_at timestamptz not null default now(),
  started_at timestamptz not null,
  finished_at timestamptz not null,

  created_at timestamptz not null default now(),

  constraint agent_runs_finished_after_started
    check (finished_at >= started_at),
  constraint agent_runs_started_after_queued
    check (started_at >= queued_at)
);
-- +goose StatementEnd

-- The console reads a run by the finding it produced, and lists recent runs
-- for an organisation. Keyset over (org_id, finished_at desc), matching how
-- `audit_log` is read, because it is the same question asked of a different
-- register.
create index agent_runs_org_finished_idx
  on public.agent_runs (org_id, finished_at desc);

alter table public.agent_runs enable row level security;
alter table public.agent_runs force row level security;

------------------------------------------------------------------------------
-- Grants. THIS TABLE STARTS CLOSED (ENT-243).
------------------------------------------------------------------------------
-- 00002 set default privileges granting `kindlast_app` select, insert, update
-- and delete on every table the migrator creates, so this table arrives with
-- all four whatever is written below. Only an explicit revoke narrows it, and
-- ENT-243's ruling is that a table asks for what it needs rather than refusing
-- what it does not.
--
-- So: revoke everything first, then grant precisely. Written this way round on
-- purpose, because `grant select` on its own would read as "the app can only
-- read" to every future reviewer and would be false.
revoke all on public.agent_runs from kindlast_app;

-- The console reads. It does not write, because it does not run agents.
grant select on public.agent_runs to kindlast_app;

create policy agent_runs_select_org on public.agent_runs
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

------------------------------------------------------------------------------
-- kindlast_agent: record a run, across every organisation
------------------------------------------------------------------------------
-- Insert and select. No update and no delete, so the append-only shape above
-- is a grant rather than a convention.
grant select, insert on public.agent_runs to kindlast_agent;

-- Unconditional, matching `transactional_outbox_agent` in 00014 and for the
-- same reason: the agent runs for organisations nobody is signed in to, so it
-- has no tenancy GUCs to be checked against. What keeps that honest is that
-- the role can reach almost nothing else, and that every row it writes names
-- the organisation it was for.
create policy agent_runs_agent on public.agent_runs
  to kindlast_agent
  using (true)
  with check (true);

-- +goose Down
drop policy if exists agent_runs_agent on public.agent_runs;
drop policy if exists agent_runs_select_org on public.agent_runs;
revoke all on public.agent_runs from kindlast_agent;
revoke all on public.agent_runs from kindlast_app;
drop table if exists public.agent_runs;
