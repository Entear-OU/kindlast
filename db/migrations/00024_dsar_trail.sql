-- +goose Up
-- 00024_dsar_trail.sql (ENT-226, ENT-205, §14.3, §14.5, §26.4)
--
-- The trail a DSAR response was built from: which store was searched for a data
-- subject's personal data, when, what came back, and what went into the answer.
--
-- WHY THIS TABLE EXISTS
--
-- ENT-205 delivered the register: who asked, what they asked for, when it
-- arrived, when the clock runs out, and when a response went out. The last of
-- those, `dsars.responded_at`, is the field a regulator reads as evidence that
-- an Article 12(3) deadline was met, and until now it was an assertion with
-- nothing behind it. The organisation says it answered in time and the record
-- cannot show what was searched, what was found, or what was returned.
--
-- For a product whose whole claim is that a human can check a claim, that was
-- the weakest row in the register.
--
-- WHICH READING OF "THREE STORES" THIS IS, BECAUSE THERE ARE TWO
--
-- The criterion carried over from ENT-205 says personal data lives in three
-- stores. §14.3 names Kindlast's own three (`postgres-app`,
-- `postgres-platform`, Redis), and answering a request about those is an
-- operator's runbook rather than a product surface: it shipped as ENT-234 and
-- lives at `docs/personal-data-runbook.md`.
--
-- This table is the other reading, and it is the one the architecture session
-- settled on for this issue. A customer's DSAR concerns their data subjects and
-- their own estate, so the trail is what was searched and returned across the
-- customer's systems, with Kindlast recording provenance rather than performing
-- the search.
--
-- That is why `source` below is free text and not an enum. An enum here would
-- be Kindlast enumerating somebody else's systems, which is the same mistake
-- `dsars.request_type` deliberately avoids: an Article 15-22 request arrives
-- worded by the person making it, and a closed list at intake loses what was
-- actually asked for. The §14.3 three are legitimate values here, for the case
-- where a customer's request reaches data Kindlast holds on their behalf, and
-- the console offers them as suggestions rather than as the only options.
--
-- WHAT IS AN INVARIANT HERE AND WHAT IS NOT
--
-- Per §14.5: a trail entry's existence, its attribution and its immutability
-- must hold no matter who writes, so they are constraints. What goes IN an
-- entry, which store to search next, whether a response may go out with an
-- empty trail, are decisions, and they are Go's. In particular this migration
-- adds no function: `log_dsar` and `mark_dsar_responded` were dropped in 00016
-- precisely because they decided things, and a trail that arrived with its own
-- plpgsql would be walking that back.

-- What the composite foreign key below references. `dsars.id` is already unique
-- as the primary key; this pairs it with the tenancy column so a child row can
-- be pinned to both at once. It has to exist before the table that references
-- it, which is why it is up here rather than beside the other index.
create unique index dsars_id_org_idx on public.dsars (id, org_id);

-- +goose StatementBegin
create table public.dsar_trail_entries (
  id uuid primary key default gen_random_uuid(),

  -- Tenancy, as on every tenant table. Never "whose data is this": that is the
  -- data subject, who has no account here and is named on the `dsars` row.
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- The request this is evidence about.
  --
  -- `on delete cascade`, unlike `findings.agent_run_id` which is `set null`.
  -- The asymmetry is deliberate and it is about what the row is evidence FOR. A
  -- narrative's run is provenance about a finding that stands on its own, so
  -- losing the run must not lose the finding. A trail entry is not a fact about
  -- anything except this request: with the request gone there is nothing for it
  -- to be the trail of, and an orphan naming a data subject is exactly the
  -- residue an erasure was meant to remove.
  dsar_id uuid not null,

  -- WHICH STORE, AND THE COMPOSITE FOREIGN KEY THAT PINS IT TO THIS TENANT.
  --
  -- `(dsar_id, org_id)` rather than `dsar_id` alone, against the unique index
  -- added above. Without the second column a handler that read `dsar_id` out of
  -- a request body and never checked whose it was could file Alpha's search
  -- results against Beta's request: the RLS with-check would pass, because the
  -- row's own `org_id` is Alpha's, and the trail of a request in another tenant
  -- would silently grow an entry.
  --
  -- RLS structurally cannot express that check, because it is a relationship
  -- between two rows rather than a predicate on one. So it is a foreign key,
  -- which is what §14.5 means by an invariant.
  constraint dsar_trail_entries_dsar_fkey
    foreign key (dsar_id, org_id) references public.dsars (id, org_id)
    on delete cascade,

  -- The store that was searched, in the customer's own words: "Salesforce",
  -- "the HR system", "postgres-app". Free text, see the header.
  source text not null,
  constraint dsar_trail_entries_source_not_blank check (btrim(source) <> ''),

  -- What happened at that store. A closed vocabulary, because unlike the store
  -- names these are Kindlast's own and a reader has to be able to count them:
  -- "three stores searched, one holding data, none withheld" is only a sentence
  -- if the values mean the same thing on every row.
  --
  --   searched   somebody looked here
  --   found      personal data about the subject was here
  --   none_found somebody looked here and there was nothing
  --   disclosed  what was found went into the response
  --   withheld   what was found was deliberately not disclosed
  --
  -- `none_found` is a separate value from the absence of a row, and that is the
  -- distinction the table is most valuable for. "We looked in the CRM and there
  -- was nothing" and "nobody has looked in the CRM" are different facts, and a
  -- register that conflates them tells a customer they are covered when they
  -- are not. Same argument as `unclassified` against `minimal` on `ai_systems`,
  -- and as a REFUSED citation against an absent one on `agent_runs`.
  --
  -- `withheld` exists because Article 15(4) and the Member State exemptions are
  -- real: a response that leaves something out for a reason is a lawful
  -- response, and one that leaves something out silently is not evidence of
  -- anything. The reason goes in `detail`.
  action text not null,
  constraint dsar_trail_entries_action_check
    check (action in ('searched', 'found', 'none_found', 'disclosed', 'withheld')),

  -- What was searched for, what came back, or why something was withheld. Free
  -- text for a human, and NOT a place to copy a data subject's personal data
  -- into: the handler writes "employment record, 2019-2024", not the record.
  -- Nothing enforces that and nothing can, which is why the console's help text
  -- says it too.
  detail text,

  -- WHEN IT HAPPENED, WHICH IS NOT WHEN IT WAS RECORDED.
  --
  -- Two timestamps for the same reason `dsars` carries `received_at` beside
  -- `created_at` (ENT-224): a handler who searched the CRM on Tuesday and wrote
  -- it down on Friday has done one thing, not two, and a trail that only knows
  -- Friday cannot show that the search happened inside the statutory month.
  --
  -- A future `occurred_at` is refused in Go rather than here, because `now()` is
  -- not immutable and a check constraint cannot see it. That is the same split
  -- ENT-224 landed on for the receipt date.
  occurred_at timestamptz not null,

  -- When it entered the record. Never supplied by a caller: it is the one
  -- timestamp on the row that describes this database rather than the world,
  -- and the append-only trigger below means it can never move afterwards.
  recorded_at timestamptz not null default now(),

  -- WHO OR WHAT PRODUCED IT.
  --
  -- `created_by` is which human wrote it down, matching every other record
  -- table, and is never used for isolation. Not a foreign key, matching every
  -- other user reference in this schema: identity is Zitadel's and the domain
  -- mirrors rather than owns it.
  created_by uuid,

  -- The agent run that contributed it, when one did. ENT-245's pattern on
  -- `findings.narrative`, and the same `on delete set null` for the same
  -- reason: a retention job on `agent_runs` must not be able to delete a
  -- customer's compliance evidence. Losing the provenance is bad; losing the
  -- entry is worse.
  --
  -- Null on every row today, because nothing writes here except a person
  -- through the console. It is here rather than in a later migration because
  -- the whole point of recording provenance is that it was recorded at the
  -- time, and a column added afterwards can only ever describe rows written
  -- after it.
  agent_run_id uuid references public.agent_runs(id) on delete set null,

  -- An entry that names neither a human nor a run is not inspectable, and an
  -- inspectable trail is the entire product claim here. One or the other, and
  -- both is fine: an agent gathered it and a person filed it is a true
  -- description of what §26.4's gateway will do.
  constraint dsar_trail_entries_attributed
    check (created_by is not null or agent_run_id is not null)
);
-- +goose StatementEnd

-- The only query this table gets: one request's trail, in the order it
-- happened. Ordered by `occurred_at` rather than `recorded_at`, because a trail
-- reads forward through the search and not through the typing, and with `id` as
-- the tie-break so a keyset cursor cannot skip or repeat an entry when two
-- searches share a timestamp.
create index dsar_trail_entries_dsar_idx
  on public.dsar_trail_entries (dsar_id, occurred_at, id);

------------------------------------------------------------------------------
-- Append-only, enforced rather than observed
------------------------------------------------------------------------------
-- A trail somebody can quietly edit is not evidence. The same argument
-- `audit_log` has rested on since 00001 and `agent_runs` since 00019, and it
-- applies here with more force than to either: those two are records ABOUT
-- decisions, and this one is the substance of a response a regulator may ask
-- to see.
--
-- The trigger binds `kindlast_migrator` too, which is the half that matters.
-- The migrator bypasses RLS and holds every grant, so grants and policies do
-- not constrain it at all, and the claim being made here is "this is what
-- happened" rather than "this is what the application last wrote".
--
-- Correcting an entry therefore means adding another one that says so, which is
-- how a paper file works and is the behaviour a compliance record should have.

-- +goose StatementBegin
create function public.dsar_trail_entries_forbid_update() returns trigger
  language plpgsql
  as $$
begin
  raise exception 'dsar_trail_entries is append-only: UPDATE on row % is not permitted', old.id
    using errcode = 'check_violation';
end;
$$;
-- +goose StatementEnd

create trigger dsar_trail_entries_no_update
  before update on public.dsar_trail_entries
  for each row execute function public.dsar_trail_entries_forbid_update();

-- THERE IS DELIBERATELY NO DELETE TRIGGER.
--
-- Not an omission, and worth stating because the UPDATE trigger next to it
-- makes its absence look like one. `docs/personal-data-runbook.md` records the
-- same shape for `audit_log` and the reasoning carries over exactly: the trail
-- cannot be rewritten by anyone, and can be removed only wholesale, by removing
-- the request or the organisation it belongs to. There is no way to delete one
-- inconvenient entry, which is the property that matters.
--
-- A `before delete` trigger would also break the one statement the erasure
-- procedure depends on. Every tenant table cascades from `organisations`, so
-- `delete from organisations where id = ...` is how a customer's data leaves,
-- and a trigger raising inside that cascade would turn erasure into an incident.
-- Withholding the DELETE grant from `kindlast_app` is what stops the product
-- deleting an entry; the cascade runs as the migrator and is unaffected.

alter table public.dsar_trail_entries enable row level security;
alter table public.dsar_trail_entries force row level security;

------------------------------------------------------------------------------
-- Grants. THIS TABLE STARTS CLOSED (ENT-243).
------------------------------------------------------------------------------
-- 00002 set default privileges granting `kindlast_app` all four verbs on every
-- table the migrator creates, so this table arrives with them whatever is
-- written below. Only an explicit revoke narrows it, and ENT-243's ruling is
-- that a table asks for what it needs rather than refusing what it does not.
revoke all on public.dsar_trail_entries from kindlast_app;

-- Read and append. No update, so the trigger above is a grant as well as a
-- trigger; no delete, so the wholesale-only shape is a grant too.
grant select, insert on public.dsar_trail_entries to kindlast_app;

-- NOTHING IS GRANTED TO `kindlast_agent`, AND THAT IS THE POINT.
--
-- An agent contributing to a trail is exactly what `agent_run_id` is for, and
-- it still gets no handle on this table, because AGENTS.md is unambiguous that
-- an agent's tools are core-api RPCs and nothing else. §26.4's gateway will
-- write these rows through `AddDsarTrailEntry` on the caller's transaction,
-- which is also the only way the entry gets an audit row and a `created_by`
-- naming the person the run acted for.
--
-- `agent_runs` grants the agent role an unconditional insert and is not a
-- precedent for doing it here: that table is the agent's own record of itself,
-- written for organisations nobody is signed in to. This one is a customer's
-- evidence about a data subject.

create policy dsar_trail_entries_select_org on public.dsar_trail_entries
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy dsar_trail_entries_insert_org on public.dsar_trail_entries
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- +goose Down
drop policy if exists dsar_trail_entries_insert_org on public.dsar_trail_entries;
drop policy if exists dsar_trail_entries_select_org on public.dsar_trail_entries;
revoke all on public.dsar_trail_entries from kindlast_app;
drop table if exists public.dsar_trail_entries;
-- After the table, because dropping it takes the trigger with it and the
-- function would otherwise be dropped out from under a live trigger. Same
-- ordering 00019 uses, and for the same reason.
drop function if exists public.dsar_trail_entries_forbid_update();
-- Last, because the foreign key on the table above depends on it.
drop index if exists public.dsars_id_org_idx;
