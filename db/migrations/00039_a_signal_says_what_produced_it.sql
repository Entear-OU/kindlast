-- +goose Up
-- 00039_a_signal_says_what_produced_it.sql (ENT-273)
--
-- `watcher_findings` gains a `source`, and a trigger refusing to let one
-- writer take over another's row.
--
-- WHAT WENT WRONG, WHICH WAS OBSERVED AND NOT IMAGINED
--
-- `emit_watcher_finding` upserts on `(profile_id, dedup_key) where status =
-- 'open'`, so whoever writes a key OWNS the row it lands on. The agentic
-- Watcher (ENT-258) is shown every open signal WITH its key, because a run
-- that is not told what is already open repeats it.
--
-- Those two facts together were a hole, and `scripts/watcher-comparison.py`
-- found it the first time it ran against a real model: a model that echoes
-- back a key it was shown does not raise a duplicate, it OVERWRITES the
-- detector's row. The observed case rewrote "Profile gap: Records of
-- Processing Activities" and dropped its severity from high to medium. A
-- deterministic finding, silently restated by a 4B, in the detector's own slot,
-- with nothing on the row saying so.
--
-- PR #244 namespaced every agent-written key with an `agent:` prefix, in Go.
-- That closed it for the one writer that exists and is why this is not urgent.
-- It is a convention in one function rather than an invariant, though, and the
-- rule in db/README.md is that something which must hold no matter who writes
-- is a constraint. This is that constraint.
--
-- WHY THE ROLE CANNOT BE THE ANSWER, WHICH IS THE WHOLE DIFFICULTY
--
-- The obvious shape is an RLS policy: let the agent write agent rows and the
-- detectors write detector rows. It does not work here, because BOTH run as
-- `kindlast_agent`. `run_watcher()` is the deterministic sweep and it is
-- invoked by the producer role; `RaiseSignal` is the agent and it is invoked by
-- the producer role. `emit_watcher_finding` is SECURITY INVOKER, so by the time
-- a row is written there is nothing about the session that distinguishes the
-- two. Postgres cannot tell them apart because they genuinely are the same
-- caller wearing different hats.
--
-- So the source has to be SAID, as an argument, and the invariant has to be
-- about the transition rather than about the writer: whatever a row was
-- created as, it stays. That is expressible as a trigger and is enforced
-- against every writer, including one that bypasses `emit_watcher_finding`
-- with its own INSERT.

alter table public.watcher_findings
  add column source text not null default 'detector';

-- The vocabulary, and it is deliberately two.
--
-- `detector` is a rule in `run_watcher()`: given the same profile it produces
-- the same signal, and a person can read the rule. `agent` is a model: it
-- produces what it produces, and what makes it accountable is `agent_runs`
-- rather than reproducibility. A customer's compliance record should not
-- present those as the same kind of claim, which is the reason this column
-- exists at all and not only the deduplication accident that found it.
--
-- Resist adding a third without a reason a customer would recognise. "Which
-- code path wrote it" is not one.
alter table public.watcher_findings
  add constraint watcher_findings_source_known
  check (source in ('detector', 'agent'));

-- Existing rows. The default above already made every one of them `detector`,
-- which is right for all but the agent's own: no deployment has run the agentic
-- Watcher, but development stacks and the comparison fixture have, and their
-- rows are identifiable by exactly the prefix PR #244 introduced.
--
-- Written as an update rather than a smarter default because it has to be
-- readable in a year: this is the one place the `agent:` convention is treated
-- as data, and it is a migration of rows that existed before the column did,
-- not a rule about rows written after it.
update public.watcher_findings
   set source = 'agent'
 where dedup_key like 'agent:%';

------------------------------------------------------------------------------
-- THE INVARIANT
------------------------------------------------------------------------------
-- A row does not change hands. A detector's signal cannot become an agent's,
-- and an agent's cannot become a detector's.
--
-- This is a trigger rather than a check constraint because a check constraint
-- sees one row and this is about the difference between two: the old row and
-- the new one. It is a trigger rather than a policy for the reason above, that
-- both writers are the same role.
--
-- WHAT IT COSTS WHEN IT FIRES, WHICH IS THE POINT
--
-- An agent that echoes a detector's key now gets an exception, and its run is
-- refused and recorded rather than quietly succeeding. That is louder than
-- letting the write land somewhere harmless, and it is the right way round:
-- the run tried to do something it must not, and a record saying so is worth
-- more than a silent correction. The Go prefix means this should never fire
-- from the path that exists today, which makes it a guard for the next writer
-- rather than a routine refusal.
-- +goose StatementBegin
create or replace function public.watcher_finding_keeps_its_source()
returns trigger
language plpgsql
set search_path to 'public', 'pg_temp'
as $$
begin
  if old.source is distinct from new.source then
    raise exception
      'watcher_findings: a signal raised by % cannot be taken over by %',
      old.source, new.source
      using errcode = 'check_violation';
  end if;
  return new;
end;
$$;
-- +goose StatementEnd

create trigger watcher_findings_source_is_fixed
  before update on public.watcher_findings
  for each row
  execute function public.watcher_finding_keeps_its_source();

------------------------------------------------------------------------------
-- AND THE ONE WRITER LEARNS TO SAY WHICH IT IS
------------------------------------------------------------------------------
-- `p_source` defaults to `detector`, so every existing caller keeps working
-- and means what it already meant: `run_watcher`'s detectors pass nothing and
-- get `detector`. Only `RaiseSignal` passes `agent`.
--
-- The upsert now carries `source` into the update, which looks like it
-- weakens the trigger and is what arms it. Without that line an agent landing
-- on a detector's row would leave `source` reading `detector` while replacing
-- the title and the severity, which is the observed failure with a column
-- added and nothing else fixed. With it, the update states who is writing and
-- the trigger refuses the transition.
--
-- The body is otherwise 00002's, restated because `create or replace` requires
-- the whole function.
--
-- AND THE OLD ARITY IS DROPPED FIRST, WHICH IS NOT TIDYING UP
--
-- `create or replace function` matches on the argument list, so adding a
-- parameter with a default REPLACES nothing: it creates an overload, and the
-- eight-argument original stays. Every existing caller then becomes ambiguous,
-- because Postgres cannot choose between the eight-argument function and the
-- nine-argument one whose ninth argument has a default. That is not a
-- hypothetical: this migration was written without the drop, and the first
-- test to call the function the way `run_watcher`'s detectors call it failed
-- with `function emit_watcher_finding(...) is not unique`. Shipped, it would
-- have broken every deterministic sweep in every deployment.
--
-- Dropping is safe because plpgsql resolves a function by name at call time
-- rather than binding at definition time, so `run_watcher` picks up whichever
-- one exists when it runs.
drop function if exists public.emit_watcher_finding(
  uuid, text, text, text, text, text, text, jsonb);

-- +goose StatementBegin
create or replace function public.emit_watcher_finding(
  p_profile_id      uuid,
  p_kind            text,
  p_dedup_key       text,
  p_title           text,
  p_detail          text  default null,
  p_severity        text  default 'medium',
  p_obligation_slug text  default null,
  p_metadata        jsonb default '{}'::jsonb,
  p_source          text  default 'detector'
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $$
declare
  v_org uuid;
  v_id  uuid;
begin
  select org_id into v_org
  from public.compliance_profiles
  where id = p_profile_id;

  if v_org is null then
    raise exception 'emit_watcher_finding: unknown profile %', p_profile_id;
  end if;

  insert into public.watcher_findings (
    profile_id, org_id, kind, obligation_slug, severity, title, detail,
    dedup_key, metadata, source
  )
  values (
    p_profile_id, v_org, p_kind, p_obligation_slug, p_severity,
    p_title, p_detail, p_dedup_key, p_metadata, p_source
  )
  on conflict (profile_id, dedup_key) where status = 'open'
  do update set
    kind            = excluded.kind,
    obligation_slug = excluded.obligation_slug,
    severity        = excluded.severity,
    title           = excluded.title,
    detail          = excluded.detail,
    metadata        = excluded.metadata,
    source          = excluded.source,
    updated_at      = now()
  returning id into v_id;

  return v_id;
end;
$$;
-- +goose StatementEnd

-- +goose Down
drop trigger if exists watcher_findings_source_is_fixed on public.watcher_findings;
drop function if exists public.watcher_finding_keeps_its_source();

-- +goose StatementBegin
create or replace function public.emit_watcher_finding(
  p_profile_id      uuid,
  p_kind            text,
  p_dedup_key       text,
  p_title           text,
  p_detail          text  default null,
  p_severity        text  default 'medium',
  p_obligation_slug text  default null,
  p_metadata        jsonb default '{}'::jsonb
)
returns uuid
language plpgsql
set search_path to 'public', 'pg_temp'
as $$
declare
  v_org uuid;
  v_id  uuid;
begin
  select org_id into v_org
  from public.compliance_profiles
  where id = p_profile_id;

  if v_org is null then
    raise exception 'emit_watcher_finding: unknown profile %', p_profile_id;
  end if;

  insert into public.watcher_findings (
    profile_id, org_id, kind, obligation_slug, severity, title, detail, dedup_key, metadata
  )
  values (
    p_profile_id, v_org, p_kind, p_obligation_slug, p_severity,
    p_title, p_detail, p_dedup_key, p_metadata
  )
  on conflict (profile_id, dedup_key) where status = 'open'
  do update set
    kind            = excluded.kind,
    obligation_slug = excluded.obligation_slug,
    severity        = excluded.severity,
    title           = excluded.title,
    detail          = excluded.detail,
    metadata        = excluded.metadata,
    updated_at      = now()
  returning id into v_id;

  return v_id;
end;
$$;
-- +goose StatementEnd

drop function if exists public.emit_watcher_finding(
  uuid, text, text, text, text, text, text, jsonb, text);

alter table public.watcher_findings drop constraint if exists watcher_findings_source_known;
alter table public.watcher_findings drop column if exists source;
