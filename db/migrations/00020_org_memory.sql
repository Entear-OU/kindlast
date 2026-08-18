-- +goose Up
-- 00020_org_memory.sql (ENT-228, §26.4, §26.5)
--
-- What Kindlast knows about an organisation, as rows a customer can read,
-- correct, export and have erased.
--
-- MEMORY IS PRODUCT DATA, WHICH IS THE WHOLE RULING
--
-- §26.5 puts this in Postgres under RLS via core-api rather than in an agent
-- framework's store, and the reason is not architectural taste. A GDPR product
-- whose own memory of a customer sits outside the customer's reach cannot
-- answer a request about that memory. Rectification and erasure are not
-- features here, they are the thing being sold, and a vector store nobody can
-- point a DSAR at would be the one place in this system where the product's
-- claim is false.
--
-- So: `org_id` on every row, FORCE ROW LEVEL SECURITY, the two-GUC policies,
-- and `on delete cascade` from `organisations`, so erasing an organisation
-- erases what we believed about it in the same statement.
--
-- TWO SHAPES, BECAUSE THEY ANSWER DIFFERENT QUESTIONS
--
-- `org_evidence` is what we OBSERVED: many narrow rows, each stamped with
-- where it came from and when. It grows forever and is never edited.
--
-- `org_profile_facts` is what we BELIEVE: at most one open row per fact, each
-- pointing at the evidence it came from where there was any. It is small, and
-- it changes.
--
-- Collapsing them into one table is the obvious simplification and it loses
-- the question worth asking. "What do you think our lawful basis is" and "what
-- did the tool actually return in March" are different questions, and a schema
-- that answers them with the same rows answers the first one badly.
--
-- WHAT REPLACES `compliance_profiles`, AND WHY IT IS NOT AN ALTER
--
-- The legacy `compliance_profiles` in 00001 is one wide row keyed by
-- `session_id` and `user_id`, with a column per question and no history and no
-- provenance. Three of §26.5's requirements are unanswerable against that
-- shape rather than merely awkward: where a fact came from, what it was before
-- somebody corrected it, and what the profile looked like when a given run
-- reasoned over it.
--
-- It is left in place and untouched here. The sweep still reads it, and moving
-- that read is ENT-212's, when onboarding writes into this profile instead of
-- a parallel one. Two profiles is a real cost and it is a temporary one; a
-- migration that changed both the schema and every reader in one step would be
-- unreviewable.

------------------------------------------------------------------------------
-- org_evidence: what we observed
------------------------------------------------------------------------------

-- +goose StatementBegin
create table public.org_evidence (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- WHERE IT CAME FROM, which is the column that makes the rest worth storing.
  --
  -- An observation with no provenance is a claim, and a compliance product
  -- that cannot say where a claim came from is asking to be believed. Not
  -- null, no default: a writer that has not decided what this is should fail
  -- rather than record 'unknown'.
  source text not null,
  constraint org_evidence_source_check
    check (source in ('onboarding', 'integration', 'human', 'agent', 'import')),

  -- Which connection produced it, for the integration case.
  --
  -- No foreign key, because the connections table is ENT-231's and does not
  -- exist. A column with no constraint is honest about that; inventing the
  -- table now to have something to point at would be guessing at a schema
  -- somebody else is designing.
  connection_id uuid,

  -- WHEN IT WAS TRUE, AND WHEN WE LEARNED IT. Two timestamps because they are
  -- routinely far apart and the gap is the interesting part: a record of
  -- processing activities last edited in March and first read by us in August
  -- is a five-month blind spot, and one timestamp would hide it.
  observed_at timestamptz not null,
  fetched_at timestamptz not null default now(),

  -- What kind of observation this is. Free text rather than an enum, because
  -- the vocabulary belongs to whatever integrations exist, and that is a
  -- decision that changes per quarter rather than an invariant.
  kind text not null,

  -- The observation. jsonb rather than columns because a narrow observation
  -- from a helpdesk and one from a cloud provider share nothing but their
  -- stamps.
  body jsonb not null default '{}'::jsonb,

  -- A digest of the content, so a writer can ask "have we seen this before".
  --
  -- INDEXED AND NOT UNIQUE, DELIBERATELY. A unique constraint would make
  -- re-observing the same content impossible to record, and "the tool still
  -- says this in August" is a fact rather than a duplicate. §26.5 calls dedup
  -- an explicit operation, so it is Go's call, made against this index, rather
  -- than an insert the database silently refuses.
  content_hash text,
  constraint org_evidence_content_hash_check
    check (content_hash is null or content_hash ~ '^[0-9a-f]{64}$'),

  -- Supersession, recorded rather than implied.
  --
  -- The row that replaced this one. Setting it is the only permitted update on
  -- this table, and the column-level grant below is what enforces that. A
  -- superseded observation is not deleted, because "we used to think this" is
  -- exactly what somebody auditing a finding needs.
  superseded_by uuid references public.org_evidence(id) on delete set null,
  constraint org_evidence_not_self_superseding
    check (superseded_by is null or superseded_by <> id),

  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- Read by organisation, newest first, which is every listing the console has.
create index org_evidence_org_observed_idx
  on public.org_evidence (org_id, observed_at desc);

-- The dedup lookup: "have we seen this content for this organisation".
create index org_evidence_org_hash_idx
  on public.org_evidence (org_id, content_hash)
  where content_hash is not null;

alter table public.org_evidence enable row level security;
alter table public.org_evidence force row level security;

------------------------------------------------------------------------------
-- org_profile_facts: what we believe, and since when
------------------------------------------------------------------------------

-- +goose StatementBegin
create table public.org_profile_facts (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- WHICH FACT. Text, and the vocabulary is NOT constrained here.
  --
  -- That is the rule in AGENTS.md applied rather than an omission: which facts
  -- the product understands is a decision that changes as it learns, so it
  -- lives in the proto enum the typed patch carries and in the Go that
  -- validates against it. A check constraint here would mean a migration every
  -- time the product learned a new question, and a migration is the wrong unit
  -- for a vocabulary.
  --
  -- What IS an invariant, and is therefore here: at most one open value per
  -- key per organisation, and a closed value that can never be edited.
  key text not null,
  constraint org_profile_facts_key_check check (key <> ''),

  -- The value, as a typed patch left it. jsonb because a fact is sometimes a
  -- boolean, sometimes a list of jurisdictions, and sometimes a number.
  value jsonb not null,

  -- WHEN THIS BECAME WHAT WE BELIEVED, AND WHEN IT STOPPED.
  --
  -- `valid_to is null` means current. This is the versioning §26.5 asks for,
  -- and it is temporal rather than a version integer on purpose: a run records
  -- the INSTANT it reasoned at (`agent_runs.profile_as_of` below), and as-of
  -- that instant this table reconstructs exactly what it saw. An integer would
  -- be a second thing to maintain, a second thing to get wrong, and would
  -- answer the question no better.
  -- `clock_timestamp()`, NOT `now()`, AND THE DIFFERENCE IS NOT PEDANTRY.
  --
  -- `now()` is the TRANSACTION timestamp and is identical for every statement
  -- in a transaction. Closing one value and opening its successor in the same
  -- transaction with `now()` produces two rows with the same `valid_from`, a
  -- zero-length interval for the superseded one, and a history whose order is
  -- undefined because the column it sorts by ties.
  --
  -- None of that shows up in a single correction, which is why it survived
  -- being written. A test in the store package found it, and this default is
  -- here so the next writer, onboarding at ENT-212, does not find it again.
  valid_from timestamptz not null default clock_timestamp(),
  valid_to timestamptz,
  constraint org_profile_facts_valid_range
    check (valid_to is null or valid_to >= valid_from),

  -- WHERE THIS FACT CAME FROM, so the console can show it against every value
  -- rather than against the profile as a whole.
  source text not null,
  constraint org_profile_facts_source_check
    check (source in ('onboarding', 'integration', 'human', 'agent', 'import')),

  -- The observation it was derived from, when it was derived from one. Null
  -- for a fact a human simply stated, which is most of onboarding.
  evidence_id uuid references public.org_evidence(id) on delete set null,

  -- Which human recorded it, when a human did. `created_by` semantics: this
  -- records who acted and is NEVER used for isolation, per AGENTS.md. Not a
  -- foreign key, matching every other user reference in this schema, because
  -- identity is Zitadel's and the domain mirrors rather than owns it.
  recorded_by uuid,

  -- Why it changed, in the correcting human's words.
  --
  -- FREE TEXT, AND NOT A CONTRADICTION OF THE RULE ABOVE. "Never free-text
  -- rewrite" is about the VALUE: nothing may replace a typed fact with prose.
  -- This is an annotation beside the value, and it is what makes a profile
  -- change legible a year later. "We appointed a DPO in June" is the sentence
  -- somebody auditing a finding needs and no enum can carry.
  note text,

  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- ONE OPEN VALUE PER FACT, ENFORCED RATHER THAN INTENDED.
--
-- This is the constraint that makes correction work. A writer recording a new
-- value MUST close the previous one, because inserting a second open row for
-- the same key fails here. Without it, "correct a fact" silently becomes "add
-- a second answer", and every reader would then need to decide which one wins,
-- which is exactly the sort of rule that gets decided differently in three
-- places.
--
-- It is a constraint rather than a trigger because it must hold no matter who
-- writes, which is the test in db/README.md for what belongs in Postgres.
create unique index org_profile_facts_one_open_per_key
  on public.org_profile_facts (org_id, key)
  where valid_to is null;

-- Reconstructing the profile as of an instant, which is what a run's
-- `profile_as_of` turns into a query.
create index org_profile_facts_org_key_from_idx
  on public.org_profile_facts (org_id, key, valid_from desc);

-- +goose StatementBegin
create function public.org_profile_facts_history_is_immutable() returns trigger
  language plpgsql
  as $$
begin
  -- A CLOSED FACT IS HISTORY AND HISTORY DOES NOT MOVE.
  --
  -- The column-level grant below already stops the app rewriting a value, but
  -- `kindlast_migrator` bypasses RLS and holds every grant, so grants do not
  -- constrain it at all. The product-facing claim on this table is "this is
  -- what we believed and when", and that wants "nobody, including us".
  --
  -- The same argument as `agent_runs` in 00019, and it applies more strongly
  -- here, because this table is the one a customer is invited to check.
  if old.valid_to is not null then
    raise exception
      'org_profile_facts row % is closed: a superseded fact cannot be changed',
      old.id
      using errcode = 'check_violation';
  end if;

  -- CLOSING IS THE ONLY EDIT AN OPEN ROW GETS.
  --
  -- Two things arrive here and both are refused by this branch. An update that
  -- changes a value and leaves the row open, which is the in-place rewrite the
  -- column-level grant already denies the app and which the migrator would
  -- otherwise have. And an update that sets `valid_to` back to null, which
  -- would let a writer rewrite the past by closing the current value and
  -- reviving an older one, arriving where the rule above forbids by a longer
  -- route.
  --
  -- The message names the permitted edit rather than the attempted one,
  -- because "you may only close it" tells a reader what to do next and "you
  -- may not do that" does not.
  if new.valid_to is null then
    raise exception
      'org_profile_facts row % is open: the only permitted update is closing '
      'it by setting valid_to', old.id
      using errcode = 'check_violation';
  end if;

  return new;
end;
$$;
-- +goose StatementEnd

create trigger org_profile_facts_history_immutable
  before update on public.org_profile_facts
  for each row execute function public.org_profile_facts_history_is_immutable();

alter table public.org_profile_facts enable row level security;
alter table public.org_profile_facts force row level security;

------------------------------------------------------------------------------
-- agent_runs: which profile a run reasoned over
------------------------------------------------------------------------------
-- §26.4 wants a run to reference the profile version it reasoned over, and
-- 00019 left the column out because there was no profile to version.
--
-- An instant rather than a version, for the reason written against `valid_from`
-- above: the fact store is temporal, so `profile_as_of` plus the two columns
-- reconstruct exactly the facts that were open when the run read them.
--
-- Nullable, because a run that read no profile should say so rather than claim
-- an as-of it never used. ENT-218's Analyst is exactly that run today: the
-- caller passes everything and the harness reads nothing.
alter table public.agent_runs
  add column profile_as_of timestamptz;

------------------------------------------------------------------------------
-- Grants. BOTH TABLES START CLOSED (ENT-243).
------------------------------------------------------------------------------
-- 00002's default privileges grant `kindlast_app` select, insert, update and
-- delete on every table the migrator creates, so these arrive with all four
-- whatever is written below. Only an explicit revoke narrows it.
revoke all on public.org_evidence from kindlast_app;
revoke all on public.org_profile_facts from kindlast_app;

------------------------------------------------------------------------------
-- kindlast_app: the console, and every write a human makes
------------------------------------------------------------------------------
-- Writes go through core-api, which connects as this role with the tenancy
-- GUCs set, so a human correcting a fact is checked by RLS on the way in. That
-- includes the integrations gateway when it lands: it calls core-api rather
-- than the database, so it needs no role of its own and `kindlast_ingest`
-- stays as narrow as 00018 made it.
--
-- NO DELETE ON EITHER TABLE, FOR ANYONE.
--
-- Erasure is `delete from organisations`, which cascades. Giving the console a
-- row-level delete would make "correct this" and "erase this" the same
-- gesture, and the difference between them is the entire point of keeping
-- history.
grant select, insert on public.org_evidence to kindlast_app;
grant select, insert on public.org_profile_facts to kindlast_app;

-- COLUMN-LEVEL UPDATE, WHICH IS THE ENFORCEMENT §26.5 ASKS FOR.
--
-- "Updated only by typed patches, never free-text rewrite" is a promise until
-- it is a privilege. `update (valid_to)` means a value physically cannot be
-- rewritten in place by this role: correcting a fact is closing one row and
-- inserting another, and the only alternative is an error.
--
-- The same for evidence, where the single permitted edit is recording that
-- something superseded it.
--
-- Readable out of `information_schema.column_privileges`, which is how the
-- isolation suite asserts it rather than trusting this comment.
grant update (valid_to) on public.org_profile_facts to kindlast_app;
grant update (superseded_by) on public.org_evidence to kindlast_app;

create policy org_evidence_select_org on public.org_evidence
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy org_evidence_insert_org on public.org_evidence
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- `using` and `with check` both, and both matter. `using` decides which rows
-- are visible to update; `with check` decides what they may become. Omitting
-- the second would let a caller move a row into another organisation, which is
-- a tenancy escape written as an update.
create policy org_evidence_update_org on public.org_evidence
  for update using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  ) with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
  );

create policy org_profile_facts_select_org on public.org_profile_facts
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy org_profile_facts_insert_org on public.org_profile_facts
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy org_profile_facts_update_org on public.org_profile_facts
  for update using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  ) with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
  );

------------------------------------------------------------------------------
-- kindlast_agent: read across organisations, write nothing
------------------------------------------------------------------------------
-- Select only. An agent reasons over what it is given and records what it did;
-- it does not decide what the organisation believes about itself. A profile
-- the agent could edit is a profile the customer no longer owns, which is the
-- opposite of what this schema exists for.
--
-- Unconditional, matching `agent_runs` in 00019 and for the same reason: the
-- agent runs for organisations nobody is signed in to, so it has no tenancy
-- GUCs to be checked against. What keeps that honest is that the role reaches
-- almost nothing else.
grant select on public.org_evidence to kindlast_agent;
grant select on public.org_profile_facts to kindlast_agent;

create policy org_evidence_agent on public.org_evidence
  for select to kindlast_agent using (true);

create policy org_profile_facts_agent on public.org_profile_facts
  for select to kindlast_agent using (true);

-- +goose Down
drop policy if exists org_profile_facts_agent on public.org_profile_facts;
drop policy if exists org_evidence_agent on public.org_evidence;
drop policy if exists org_profile_facts_update_org on public.org_profile_facts;
drop policy if exists org_profile_facts_insert_org on public.org_profile_facts;
drop policy if exists org_profile_facts_select_org on public.org_profile_facts;
drop policy if exists org_evidence_update_org on public.org_evidence;
drop policy if exists org_evidence_insert_org on public.org_evidence;
drop policy if exists org_evidence_select_org on public.org_evidence;

revoke all on public.org_profile_facts from kindlast_agent;
revoke all on public.org_evidence from kindlast_agent;
revoke all on public.org_profile_facts from kindlast_app;
revoke all on public.org_evidence from kindlast_app;

alter table public.agent_runs drop column if exists profile_as_of;

drop table if exists public.org_profile_facts;
-- After the table, because dropping it takes the trigger with it and the
-- function would otherwise be dropped out from under a live trigger.
drop function if exists public.org_profile_facts_history_is_immutable();

drop table if exists public.org_evidence;
