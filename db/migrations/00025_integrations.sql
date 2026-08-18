-- +goose Up
-- 00025_integrations.sql (ENT-231, §26.4, §26.5)
--
-- Where evidence comes from: a customer's own tools, reached through a policy
-- gateway, recorded as rows the customer can inspect and switch off.
--
-- WHY THERE IS A SCHEMA HERE AT ALL, RATHER THAN AN MCP CLIENT IN THE AGENT
--
-- The agent framework would happily hold an MCP client. Four things go wrong if
-- it does, and each of them is a table below rather than an argument.
--
--   A customer's server would define the tool surface. So `integration_tools`
--   records what a connection offered AT DISCOVERY, whether each tool can
--   write, and which ones this organisation has actually granted. Nothing is
--   reachable because a server said it exists.
--
--   Third-party credentials would live in the Python service. So the
--   credential is a ciphertext column here, written and read by core-api,
--   which is the only process in this system holding the key.
--
--   Third-party text would arrive one hop from the model with no label. So a
--   fetch lands in `org_evidence` (00020), stamped with its source, its
--   connection and when it was fetched, which is data with provenance rather
--   than instruction.
--
--   And the company would be rediscovered from scratch on every run, with no
--   picture anybody could look at. So `integration_fetches` is the "what we
--   fetched" view: one row per attempt, including the refusals, which are the
--   rows that show the policy working.
--
-- WHAT IS NOT HERE
--
-- No columns for OAuth connectors (Google Workspace, Slack, GitHub). `kind` is
-- constrained to `mcp` and widening it is a one-line migration, but an OAuth
-- connector needs a registered application and a client secret per provider,
-- neither of which exists yet, so shaping the columns for it now would be
-- guessing at something nobody can test.
--
-- No scheduling. A background Watcher sweeping every connection nightly is
-- Temporal's, at build-order step 8, and Temporal is not here. Every fetch
-- these tables record is one a person asked for, synchronously, from the
-- console.
--
-- WHO WRITES THESE ROWS
--
-- core-api, on `kindlast_app`, in the requesting human's transaction with both
-- tenancy GUCs set. The gateway holds no database credential: it is handed a
-- fetch to perform and returns what it found, exactly as 00020 anticipated
-- when it said the integrations gateway "calls core-api rather than the
-- database, so it needs no role of its own". No seventh role, therefore, and
-- `kindlast_ingest` stays as narrow as 00018 made it.

------------------------------------------------------------------------------
-- integrations: one customer system we may reach
------------------------------------------------------------------------------

-- +goose StatementBegin
create table public.integrations (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- What sort of connection this is. Constrained, and to one value today.
  --
  -- A check constraint rather than free text, unlike `org_evidence.kind` next
  -- door, and the difference is worth stating because the two look alike. An
  -- evidence kind is a vocabulary that grows with whatever integrations exist,
  -- so it is Go's. A connection kind decides which code path dials out, and a
  -- row naming a kind no code implements is a row that can only fail.
  kind text not null,
  constraint integrations_kind_check check (kind in ('mcp')),

  -- What the customer calls it, and what the console lists.
  display_name text not null,
  constraint integrations_display_name_check check (btrim(display_name) <> ''),

  -- Where it answers. Stored as given, checked against the egress allow-list
  -- by the gateway on every call rather than once at insert.
  --
  -- ONCE AT INSERT WOULD BE THE BUG. An allow-list is deployment
  -- configuration, so a host withdrawn from it must stop being reachable for
  -- connections that already exist, and a check performed only here would
  -- leave those connections dialling a host the operator has taken out.
  endpoint_url text not null,
  constraint integrations_endpoint_url_check
    check (endpoint_url ~ '^https?://'),

  -- The credential, encrypted by core-api before it ever reaches this column.
  --
  -- NULL IS A REAL CONFIGURATION AND NOT A MISSING VALUE: an MCP endpoint on a
  -- customer's own network may want no bearer token at all, and NOT NULL here
  -- would force a writer to invent an empty one.
  --
  -- bytea rather than text, because it is ciphertext rather than something
  -- anybody should be tempted to read, and `key_id` beside it so a key can be
  -- rotated without a migration: a row says which key sealed it, and core-api
  -- keeps retired keys for decryption until every row has been re-sealed.
  credential_ciphertext bytea,
  credential_key_id text,
  constraint integrations_credential_key_check
    check ((credential_ciphertext is null) = (credential_key_id is null)),

  -- Active or revoked. Revoked is terminal: reconnecting is a new row with a
  -- new consent, because permission to reach a customer's system is not
  -- something to resurrect silently months later.
  status text not null default 'active',
  constraint integrations_status_check check (status in ('active', 'revoked')),

  created_by uuid,
  created_at timestamptz not null default now(),
  revoked_at timestamptz,
  revoked_by uuid,

  constraint integrations_revocation_consistent
    check ((status = 'revoked') = (revoked_at is not null)),

  -- Two connections may point at the same endpoint with different
  -- credentials, so the endpoint is not unique. The name is, per organisation,
  -- because a console listing two things called "Helpdesk" is a console where
  -- somebody revokes the wrong one.
  constraint integrations_name_unique unique (org_id, display_name)
);
-- +goose StatementEnd

create index integrations_org_status_idx
  on public.integrations (org_id, status, created_at desc);

alter table public.integrations enable row level security;
alter table public.integrations force row level security;

------------------------------------------------------------------------------
-- integration_tools: what the connection offered, and what we may call
------------------------------------------------------------------------------
-- THE TABLE THE FIRST ACCEPTANCE CRITERION LIVES IN.
--
-- A connection's write-capable tools must be unreachable unless explicitly
-- granted, per connection. Three things here make that structural rather than
-- a rule somebody remembers:
--
--   `granted` DEFAULTS TO FALSE. A discovery insert that says nothing about a
--   tool produces a tool nobody may call. The safe direction is the lazy one.
--
--   `write_capable` IS NOT UPDATABLE BY THE APPLICATION. See the grants below:
--   `kindlast_app` holds update on the three grant columns and on nothing
--   else. So a caller cannot relabel a write tool as read-only to walk it past
--   the gate; the label is what discovery recorded and only a new discovery
--   changes it.
--
--   AND THE GATEWAY REFUSES INDEPENDENTLY. These rows are what core-api sends
--   the gateway per call, and the gateway checks them again before it dials.
--   This table records the decision; it is not the only thing enforcing it,
--   because a table cannot stop a request that never asks it.

-- +goose StatementBegin
create table public.integration_tools (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,
  integration_id uuid not null
    references public.integrations(id) on delete cascade,

  -- The tool's name as the server gave it, which is the string a call names.
  name text not null,
  constraint integration_tools_name_check check (btrim(name) <> ''),

  -- What the server said it does, shown on the consent screen. Untrusted text
  -- from a third party: it is rendered to a human and never concatenated into
  -- a prompt.
  description text not null default '',

  -- Whether calling it can change something on the customer's side.
  --
  -- Recorded at discovery, from the server's own annotation where it offers
  -- one and from a conservative reading of the tool where it does not. NOT
  -- NULL and no default, because a writer that has not decided must fail
  -- rather than record "read-only" by omission, which is the one wrong answer
  -- this column can hold.
  write_capable boolean not null,

  granted boolean not null default false,
  granted_at timestamptz,
  granted_by uuid,
  constraint integration_tools_grant_consistent
    check (not granted or granted_at is not null),

  discovered_at timestamptz not null default now(),

  constraint integration_tools_unique_per_connection
    unique (integration_id, name)
);
-- +goose StatementEnd

create index integration_tools_org_connection_idx
  on public.integration_tools (org_id, integration_id, name);

alter table public.integration_tools enable row level security;
alter table public.integration_tools force row level security;

------------------------------------------------------------------------------
-- integration_consents: what a human was shown, and agreed to
------------------------------------------------------------------------------
-- APPEND ONLY, WITH NO UPDATE GRANT FOR ANYBODY.
--
-- A consent record that could be edited afterwards is not a consent record. It
-- stores the tool list as it was rendered on the screen, so "which tools did
-- they actually agree to" is answerable from this row alone rather than from
-- `integration_tools` as it stands today, which is the set after every later
-- change.
--
-- Changing an allow-list therefore writes a NEW consent row. The history is
-- the point: widening what Kindlast may call inside a customer's systems is
-- exactly the change somebody will later want to reconstruct.

-- +goose StatementBegin
create table public.integration_consents (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,
  integration_id uuid not null
    references public.integrations(id) on delete cascade,

  -- Which human agreed. `created_by` semantics per AGENTS.md: it records who
  -- acted and is never used for isolation.
  consented_by uuid not null,
  consented_at timestamptz not null default now(),

  -- The endpoint as it was at the moment of consent. Duplicated from
  -- `integrations` deliberately: consent to reach one host is not consent to
  -- reach whatever that row says later.
  endpoint_url text not null,

  -- Every tool the connection exposed, as shown, with its write flag. jsonb,
  -- because this is a snapshot of a screen rather than something to query
  -- across rows.
  offered_tools jsonb not null default '[]'::jsonb,

  -- Which of them Kindlast was permitted to call. A text array rather than
  -- jsonb, because this half IS queried: "was this tool ever consented to" is
  -- the question an incident asks.
  granted_tools text[] not null default '{}',
  constraint integration_consents_granted_tools_check
    check (array_position(granted_tools, null) is null)
);
-- +goose StatementEnd

create index integration_consents_connection_idx
  on public.integration_consents (org_id, integration_id, consented_at desc);

alter table public.integration_consents enable row level security;
alter table public.integration_consents force row level security;

------------------------------------------------------------------------------
-- integration_fetches: what we fetched, including what we refused to fetch
------------------------------------------------------------------------------
-- THE "WHAT WE FETCHED" VIEW, AND THE REFUSALS ARE THE IMPORTANT HALF.
--
-- A log holding only successful fetches would be indistinguishable from a
-- deployment where the policy gateway does nothing. The rows that make the
-- gateway legible to a customer are the ones saying "this asked for a tool it
-- was not granted, and we did not dial".
--
-- Insert only, for everybody. A fetch is written once, when it has finished,
-- carrying both timestamps. There is no start row to amend, for the reason
-- `agent_runs` in 00019 has none: a record its producer can revise is worth
-- less than one it cannot.

-- +goose StatementBegin
create table public.integration_fetches (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,
  integration_id uuid not null
    references public.integrations(id) on delete cascade,

  -- Which tool was asked for. Recorded even when the answer was no, because
  -- "what did it try to call" is the question a refusal exists to answer.
  tool text not null,
  constraint integration_fetches_tool_check check (btrim(tool) <> ''),

  -- The arguments, after redaction. jsonb, and it may be empty.
  arguments_json jsonb not null default '{}'::jsonb,

  requested_at timestamptz not null default now(),
  finished_at timestamptz not null default now(),

  outcome text not null,
  constraint integration_fetches_outcome_check
    check (outcome in ('succeeded', 'refused', 'failed')),

  -- Why, when it was not a success. For a human to read, not a code to branch
  -- on, and required: an outcome that is not success and says nothing is a row
  -- that tells nobody anything.
  detail text,
  constraint integration_fetches_detail_present
    check (outcome = 'succeeded' or btrim(coalesce(detail, '')) <> ''),

  -- What it produced, when it produced anything. The link from this log to the
  -- observation, so "what we fetched" and "what we believe" are one story.
  evidence_id uuid references public.org_evidence(id) on delete set null,
  constraint integration_fetches_evidence_only_on_success
    check (evidence_id is null or outcome = 'succeeded'),

  -- How many values the redactor replaced on the way in. Zero is a fact rather
  -- than a missing value: it says the redactor ran and found nothing.
  redactions integer not null default 0,
  constraint integration_fetches_redactions_check check (redactions >= 0),

  -- Which human asked. Never used for isolation.
  requested_by uuid
);
-- +goose StatementEnd

create index integration_fetches_org_time_idx
  on public.integration_fetches (org_id, requested_at desc);

create index integration_fetches_connection_time_idx
  on public.integration_fetches (org_id, integration_id, requested_at desc);

alter table public.integration_fetches enable row level security;
alter table public.integration_fetches force row level security;

------------------------------------------------------------------------------
-- audit_evidence: the observation a recorded decision rested on
------------------------------------------------------------------------------
-- ENT-231 asks that the audit row of a finding be able to point at the
-- evidence it used. This is that pointer.
--
-- A JUNCTION TABLE RATHER THAN A `uuid[]` COLUMN ON `audit_log`, and the
-- reason is the one thing an array cannot give: Postgres 17 has no array
-- foreign key, so a column of ids would be a set of claims that some row
-- somewhere exists. A row here cannot name an observation that is not there,
-- and cannot name one belonging to another organisation, because both sides
-- are foreign keys and the policy below pins the org.
--
-- No update and no delete for anybody, matching `audit_log` itself, whose
-- whole claim is that nobody including us can revise it. A decision's stated
-- basis is part of the decision.

-- +goose StatementBegin
create table public.audit_evidence (
  org_id uuid not null references public.organisations(id) on delete cascade,
  audit_id uuid not null references public.audit_log(id) on delete cascade,
  evidence_id uuid not null references public.org_evidence(id) on delete cascade,
  recorded_at timestamptz not null default now(),
  primary key (audit_id, evidence_id)
);
-- +goose StatementEnd

create index audit_evidence_org_evidence_idx
  on public.audit_evidence (org_id, evidence_id);

alter table public.audit_evidence enable row level security;
alter table public.audit_evidence force row level security;

------------------------------------------------------------------------------
-- org_evidence.connection_id becomes a real reference
------------------------------------------------------------------------------
-- 00020 left this column with no foreign key and said why: "the connections
-- table is ENT-231's and does not exist. A column with no constraint is honest
-- about that". It exists now, so the constraint replaces the honesty.
--
-- DEFERRABLE INITIALLY DEFERRED, WHICH IS NOT DECORATION.
--
-- Erasing an organisation is `delete from organisations`, and both
-- `org_evidence` and `integrations` hang off it by cascade. Postgres promises
-- no order between two cascade branches, so an immediate check could see an
-- evidence row pointing at a connection that has already gone. Deferring to
-- commit means the pair is checked once, after the whole erasure, where it
-- holds.
--
-- And no `on delete set null`, because null here would mean "we do not know
-- where this came from", which is the one thing an evidence row must never
-- say. Revoking a connection does not delete it; only erasing the organisation
-- does, and then the evidence goes too.
alter table public.org_evidence
  add constraint org_evidence_connection_fk
  foreign key (connection_id) references public.integrations(id)
  deferrable initially deferred;

------------------------------------------------------------------------------
-- Grants. EVERY TABLE STARTS CLOSED (ENT-243).
------------------------------------------------------------------------------
-- 00002's default privileges hand `kindlast_app` select, insert, update and
-- delete on every table the migrator creates, so these arrive with all four
-- whatever is written below. Only an explicit revoke narrows it.
revoke all on public.integrations from kindlast_app;
revoke all on public.integration_tools from kindlast_app;
revoke all on public.integration_consents from kindlast_app;
revoke all on public.integration_fetches from kindlast_app;
revoke all on public.audit_evidence from kindlast_app;

-- The console reads everything and creates connections, tools, consents and
-- fetch records.
grant select, insert on public.integrations to kindlast_app;
grant select, insert on public.integration_tools to kindlast_app;
grant select, insert on public.integration_consents to kindlast_app;
grant select, insert on public.integration_fetches to kindlast_app;
grant select, insert on public.audit_evidence to kindlast_app;

-- COLUMN-LEVEL UPDATE, WHICH IS WHERE THE POLICY STOPS BEING A PROMISE.
--
-- On `integrations`: revoking is one permitted edit and rotating a credential
-- is the other. NOT the endpoint. A connection whose endpoint could be edited
-- in place would let somebody move a consented connection to a different host
-- with no new consent, which is the whole mechanism defeated by an UPDATE.
-- Moving a connection is revoking one and creating another.
grant update (status, revoked_at, revoked_by)
  on public.integrations to kindlast_app;
grant update (credential_ciphertext, credential_key_id)
  on public.integrations to kindlast_app;

-- On `integration_tools`: the grant flags, and nothing else.
--
-- `write_capable` is deliberately absent, and it is the reason for doing this
-- column by column. If the application could set it, "a write-capable tool is
-- unreachable unless explicitly granted" would be defeated by a single UPDATE
-- relabelling the tool, with every other check still passing.
--
-- `name` is absent for the same reason one step removed: renaming a tool row
-- would move a grant from one tool onto another.
grant update (granted, granted_at, granted_by)
  on public.integration_tools to kindlast_app;

-- No update on `integration_consents`, `integration_fetches` or
-- `audit_evidence` for anybody. Each records something that happened. No
-- delete anywhere either: erasure is `delete from organisations`, which
-- cascades, and a row-level delete would make "revoke this" and "erase this"
-- the same gesture.

------------------------------------------------------------------------------
-- Policies, in the two-GUC form
------------------------------------------------------------------------------
-- Org equality plus a membership `exists`, so a middleware bug that set an
-- organisation the caller does not belong to would still read zero rows.

create policy integrations_select_org on public.integrations
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integrations_insert_org on public.integrations
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- `using` and `with check` both. `using` decides which rows may be updated,
-- `with check` decides what they may become; omitting the second would let a
-- caller move a connection into another organisation, which is a tenancy
-- escape written as an update.
create policy integrations_update_org on public.integrations
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

create policy integration_tools_select_org on public.integration_tools
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integration_tools_insert_org on public.integration_tools
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integration_tools_update_org on public.integration_tools
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

create policy integration_consents_select_org on public.integration_consents
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integration_consents_insert_org on public.integration_consents
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integration_fetches_select_org on public.integration_fetches
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy integration_fetches_insert_org on public.integration_fetches
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy audit_evidence_select_org on public.audit_evidence
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy audit_evidence_insert_org on public.audit_evidence
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

------------------------------------------------------------------------------
-- kindlast_agent: read what was fetched, write nothing, never the credential
------------------------------------------------------------------------------
-- An agent reasons over stored evidence, which 00020 already lets it read. It
-- reads the connection and the fetch log too, so a narration can say where an
-- observation came from without a round trip through the console's role.
--
-- COLUMN-LEVEL SELECT ON `integrations`, WHICH IS THE UNUSUAL ONE HERE.
--
-- A plain `grant select` would let the producer role read
-- `credential_ciphertext`. It is only a ciphertext and the agent holds no key,
-- so reading it would buy an attacker little; naming the columns costs one
-- line and removes the argument entirely. The rule this schema keeps
-- everywhere else, that a role reaches exactly what its job needs, is worth
-- keeping where the value at stake is a customer's credential.
--
-- Unconditional policies, matching `agent_runs` in 00019 and `org_evidence` in
-- 00020: the agent runs for organisations nobody is signed in to, so it has no
-- tenancy GUCs to be checked against. What keeps that honest is that the role
-- reaches almost nothing else.
grant select (id, org_id, kind, display_name, endpoint_url, status,
              created_at, revoked_at)
  on public.integrations to kindlast_agent;
grant select on public.integration_tools to kindlast_agent;
grant select on public.audit_evidence to kindlast_agent;

------------------------------------------------------------------------------
-- AND THE ONE WRITE THE PRODUCER ROLE GETS, WHICH IS THE INGEST PATH
------------------------------------------------------------------------------
-- `IngestService.IngestEvidence` records what a machine fetched: the scheduled
-- Watcher at build-order step 8, and any gateway-initiated fetch that runs for
-- an organisation nobody is signed in to. It runs on `kindlast_agent`, so that
-- role needs insert on the two tables a fetch produces and on nothing else.
--
-- WHY THIS DOES NOT CONTRADICT 00020, WHICH GAVE THE AGENT SELECT ONLY.
--
-- 00020's argument is about what an organisation BELIEVES: "a profile the agent
-- could edit is a profile the customer no longer owns". That stands, and
-- `org_profile_facts` stays select-only for this role. What is added here is
-- the ability to record an OBSERVATION, which is what `org_evidence` is for and
-- what an agent fetching from a customer's tool is doing. Believing and
-- observing are the two shapes 00020 separated on purpose, and only the second
-- moves.
--
-- Insert and nothing else. No update, so a superseded observation is recorded
-- by the console's role as 00020 arranged; no delete, so erasure stays a single
-- gesture on `organisations`.
grant select, insert on public.org_evidence to kindlast_agent;
grant select, insert on public.integration_fetches to kindlast_agent;

-- The insert policies are org-scoped through the GUC and carry no membership
-- check, matching every other producer policy since 00008: the agent runs for
-- organisations nobody is signed in to, so there is no member to check. Tenancy
-- still binds it, because it can only write into the organisation its GUC
-- names, and a caller that sets none writes nothing at all rather than
-- everything.
create policy org_evidence_agent_insert on public.org_evidence
  for insert to kindlast_agent with check (
    org_id = (select current_setting('app.current_org_id', true)::uuid)
  );

create policy integration_fetches_agent_insert on public.integration_fetches
  for insert to kindlast_agent with check (
    org_id = (select current_setting('app.current_org_id', true)::uuid)
  );

create policy integrations_agent on public.integrations
  for select to kindlast_agent using (true);

create policy integration_tools_agent on public.integration_tools
  for select to kindlast_agent using (true);

create policy integration_fetches_agent on public.integration_fetches
  for select to kindlast_agent using (true);

create policy audit_evidence_agent on public.audit_evidence
  for select to kindlast_agent using (true);

-- +goose Down
drop policy if exists integration_fetches_agent_insert on public.integration_fetches;
drop policy if exists org_evidence_agent_insert on public.org_evidence;
drop policy if exists audit_evidence_agent on public.audit_evidence;
drop policy if exists integration_fetches_agent on public.integration_fetches;
drop policy if exists integration_tools_agent on public.integration_tools;
drop policy if exists integrations_agent on public.integrations;

drop policy if exists audit_evidence_insert_org on public.audit_evidence;
drop policy if exists audit_evidence_select_org on public.audit_evidence;
drop policy if exists integration_fetches_insert_org on public.integration_fetches;
drop policy if exists integration_fetches_select_org on public.integration_fetches;
drop policy if exists integration_consents_insert_org on public.integration_consents;
drop policy if exists integration_consents_select_org on public.integration_consents;
drop policy if exists integration_tools_update_org on public.integration_tools;
drop policy if exists integration_tools_insert_org on public.integration_tools;
drop policy if exists integration_tools_select_org on public.integration_tools;
drop policy if exists integrations_update_org on public.integrations;
drop policy if exists integrations_insert_org on public.integrations;
drop policy if exists integrations_select_org on public.integrations;

revoke all on public.audit_evidence from kindlast_agent;
revoke all on public.integration_fetches from kindlast_agent;
revoke all on public.integration_tools from kindlast_agent;
revoke all on public.integrations from kindlast_agent;

-- Narrowed rather than revoked, because 00020 granted select here and this
-- migration only widened it to insert. A blanket revoke would leave the agent
-- unable to read evidence at all, which is 00020's grant and not this one's to
-- take away.
revoke insert on public.org_evidence from kindlast_agent;

revoke all on public.audit_evidence from kindlast_app;
revoke all on public.integration_fetches from kindlast_app;
revoke all on public.integration_consents from kindlast_app;
revoke all on public.integration_tools from kindlast_app;
revoke all on public.integrations from kindlast_app;

-- Before the tables, because the constraint names one this Down is about to
-- drop and `org_evidence` outlives it.
alter table public.org_evidence
  drop constraint if exists org_evidence_connection_fk;

drop table if exists public.audit_evidence;
drop table if exists public.integration_fetches;
drop table if exists public.integration_consents;
drop table if exists public.integration_tools;
drop table if exists public.integrations;
