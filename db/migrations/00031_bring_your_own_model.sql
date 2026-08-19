-- +goose Up
-- 00031_bring_your_own_model.sql (ENT-236, §26.6)
--
-- Where an organisation's model runs, recorded as a decision rather than
-- stored as a preference.
--
-- WHY THIS IS NOT A SETTINGS COLUMN ON `organisations`
--
-- The bundled stack serves its own model (ENT-235) and needs no API key, which
-- is what lets a deployment holding a compliance record run with no outbound
-- internet at all. `docs/self-hosting.md` promises exactly that. Pointing one
-- organisation at a hosted provider is the act of giving it up: from that
-- moment its compliance profile, its findings and its DSAR content leave the
-- deployment and are processed by somebody else.
--
-- For the customer that is a new sub-processor and a processing decision they
-- are obliged to be able to account for. A boolean on `organisations` records
-- the state and destroys the decision: it says where the data goes today and
-- nothing about when that changed, who changed it, or where it went before. A
-- customer asked "since when has your findings text been going to a US
-- provider" could not answer from it.
--
-- So the table below is append-mostly and the sequence of its rows is the
-- history. `audit_log` carries the human-readable event beside it, written by
-- core-api through `record_audit_log` the same way every other decision is,
-- which means it exports with the rest of the record and needs no second
-- export path.
--
-- WHAT THE APPLICATION MAY CHANGE, AND WHAT IT MAY NOT
--
-- Not the provider, not the endpoint and not the model. Those three are the
-- decision. If they were updatable in place, "switch provider" would be an
-- UPDATE that leaves one row saying where the data goes now and no trace of
-- where it went yesterday, with every other check in this file still passing.
-- Moving is revoking one row and inserting another, and 00025 reached the same
-- conclusion for the same reason about a connection's endpoint.
--
-- What the application may change is the status (with its revocation stamps)
-- and the credential, which is rotation. A partial unique index keeps one
-- active row per organisation so "switch" cannot silently become "two, and
-- whichever the query ordered first".
--
-- WHY A REVOKED ROW HOLDS NO CIPHERTEXT
--
-- Reverting to the bundled model is the customer withdrawing the provider from
-- their processing. A row that kept the sealed key would leave the credential
-- at rest in a system that has been told to stop using it, and would make
-- "revoked" a state somebody could undo with an UPDATE on one column. The
-- check constraint makes the revert destroy the key in the same statement, so
-- turning it back on means the customer supplies the key again, which is the
-- honest shape: reconnecting to a sub-processor is a fresh decision.
--
-- WHAT THIS DOES NOT DECIDE
--
-- Whether BYOK is permitted at all, and which providers exist. That is
-- instance-level configuration in core-api (`KINDLAST_BYOK_PROVIDERS`), not a
-- table, because "nobody at this company may point our compliance data at an
-- external API" has to be enforceable by the operator who runs the deployment
-- and not editable by any organisation inside it. A row here is only ever the
-- choice of one option from a list the deployment already permits, and the
-- allow-list is re-checked on every run rather than once at insert, for the
-- reason 00025 gives about connection endpoints: a provider withdrawn from the
-- list must stop being reachable for organisations that already chose it.

------------------------------------------------------------------------------
-- org_model_config: which model serves one organisation's runs
------------------------------------------------------------------------------

-- +goose StatementBegin
create table public.org_model_config (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- The sub-processor's name, as the customer's own record will have to name
  -- it. Free text with a non-blank check rather than a value list, because the
  -- permitted set is deployment configuration and a check constraint here
  -- would mean an operator adding a provider needs a migration.
  --
  -- There is deliberately no `local` or `none` value. The bundled model is the
  -- ABSENCE of an active row, so the default state of every organisation is
  -- the one where nothing leaves the deployment, and it is that way without
  -- anybody having written a row to say so.
  provider text not null,
  constraint org_model_config_provider_check check (btrim(provider) <> ''),

  -- Where the provider answers, OpenAI-compatible (ENT-235). Checked here only
  -- for shape: the real check is in Go, resolves the host and refuses private,
  -- loopback and link-local addresses, and runs again on every use.
  base_url text not null,
  constraint org_model_config_base_url_check check (base_url ~ '^https://'),

  -- Which model to ask for. Required, unlike the bundled server, which serves
  -- exactly one file and ignores the field.
  model text not null,
  constraint org_model_config_model_check check (btrim(model) <> ''),

  -- The API key, sealed by core-api before it reaches this column, with the
  -- row id bound in as additional authenticated data so a ciphertext cannot be
  -- moved between organisations. See internal/secrets.
  --
  -- NULLABLE, and null is a real configuration rather than a missing value: a
  -- provider on a customer's own private network may authenticate by mutual
  -- TLS or by nothing at all, and NOT NULL would force a writer to invent an
  -- empty credential. It is also the state a revoked row is required to be in.
  credential_ciphertext bytea,
  credential_key_id text,
  constraint org_model_config_credential_key_check
    check ((credential_ciphertext is null) = (credential_key_id is null)),

  -- The last four characters, for the console to show. Not a secret and not
  -- enough to be one: it exists so somebody can tell which key is in place
  -- without the product ever being able to show them the key.
  credential_last_four text,
  constraint org_model_config_last_four_check
    check (credential_last_four is null or credential_last_four ~ '^[A-Za-z0-9]{4}$'),

  status text not null default 'active',
  constraint org_model_config_status_check check (status in ('active', 'revoked')),

  created_by uuid,
  created_at timestamptz not null default now(),
  revoked_at timestamptz,
  revoked_by uuid,

  constraint org_model_config_revocation_consistent
    check ((status = 'revoked') = (revoked_at is not null)),

  -- The one that makes reverting destroy the key rather than park it.
  constraint org_model_config_revoked_holds_no_credential
    check (status = 'active' or credential_ciphertext is null)
);
-- +goose StatementEnd

-- One active choice per organisation. Partial, so the revoked rows that make
-- up the history do not collide with each other or with the current one.
create unique index org_model_config_one_active_idx
  on public.org_model_config (org_id) where status = 'active';

create index org_model_config_org_time_idx
  on public.org_model_config (org_id, created_at desc);

alter table public.org_model_config enable row level security;
alter table public.org_model_config force row level security;

------------------------------------------------------------------------------
-- Grants. EVERY TABLE STARTS CLOSED (ENT-243, 00029).
------------------------------------------------------------------------------
-- Nothing is attached to a new table since 00029 emptied the default
-- privileges, so what is written here is what the roles hold.

grant select, insert on public.org_model_config to kindlast_app;

-- COLUMN-LEVEL UPDATE, WHICH IS WHERE THIS TABLE STOPS BEING A SETTINGS ROW.
--
-- Revoking is one permitted edit and rotating the key is the other. NOT the
-- provider, NOT the endpoint, NOT the model. See the header: those three are
-- the decision, and a decision that can be edited in place has no history.
--
-- The credential columns are here rather than insert-only because rotating a
-- key is not a new processing decision: the sub-processor is unchanged, and
-- forcing a revoke-and-recreate for it would fill the record with events that
-- did not happen.
grant update (status, revoked_at, revoked_by,
              credential_ciphertext, credential_key_id)
  on public.org_model_config to kindlast_app;

-- No delete for anybody. Erasure is `delete from organisations`, which
-- cascades; a row-level delete would make "we stopped using that provider" and
-- "there is no record that we ever used it" the same gesture.

------------------------------------------------------------------------------
-- Policies, in the two-GUC form
------------------------------------------------------------------------------
-- Org equality plus a membership `exists`, so a middleware bug that set an
-- organisation the caller does not belong to still reads zero rows. Whether
-- the caller may WRITE one is an owner check in Go, because a role threshold
-- is a decision and decisions are Go's (db/README.md).

create policy org_model_config_select_org on public.org_model_config
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy org_model_config_insert_org on public.org_model_config
  for insert with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- `using` and `with check` both. `using` decides which rows may be updated and
-- `with check` decides what they may become; omitting the second would let a
-- caller move a choice into another organisation, which is a tenancy escape
-- written as an update.
create policy org_model_config_update_org on public.org_model_config
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
-- kindlast_agent: what a run needs, and nothing about who chose it
------------------------------------------------------------------------------
-- The narration job runs on the producer pool, for organisations nobody is
-- signed in to, so this is the pool that has to resolve which endpoint a run
-- goes to. It therefore needs the sealed credential, which is the one place
-- this file departs from 00025's rule that the producer role never reads a
-- ciphertext.
--
-- IT IS THE SAME RULE, NOT AN EXCEPTION TO IT. 00025's argument is that a role
-- reaches exactly what its job needs, and the producer's job there is to
-- reason over stored evidence, which never involves dialling a customer's
-- system: core-api's application role holds the credential because the gateway
-- call is made from a request. Here the job IS the model call, so the endpoint
-- and the key are what it needs. Nothing is widened by it either: the pool and
-- the keyring live in the same core-api process, so a ciphertext readable on
-- this pool is readable by a process that already holds the key.
--
-- Columns named rather than a blanket select, because `created_by` and
-- `revoked_by` decide nothing about a run and naming the list costs one line.
grant select (id, org_id, provider, base_url, model,
              credential_ciphertext, credential_key_id, status, created_at)
  on public.org_model_config to kindlast_agent;

-- Unconditional, matching every other producer policy since 00008: the agent
-- runs for organisations nobody is signed in to, so it has no tenancy GUCs to
-- be checked against. What keeps that honest is that the role reaches almost
-- nothing else, and that it holds no write here at all.
create policy org_model_config_agent on public.org_model_config
  for select to kindlast_agent using (true);

------------------------------------------------------------------------------
-- agent_runs: which provider served the run
------------------------------------------------------------------------------
-- ENT-218 records the model and the model version per run, and both come from
-- the Intelligence container's own environment. They are therefore constants
-- of the DEPLOYMENT, so an organisation switching provider changes neither,
-- and every run before and after the switch reads identically. Provenance that
-- cannot distinguish "processed here" from "processed by a third party" is
-- exactly the distinction this feature exists to make.
--
-- `instance` rather than `local` as the default, because it is what the column
-- can actually claim. A deployment is free to point KINDLAST_MODEL_URL at
-- anything it likes (ENT-235 made that the same code path), so the honest
-- statement about a run with no organisation-level choice is that it went to
-- whatever this instance serves, not that it stayed on the machine.

alter table public.agent_runs
  add column provider text not null default 'instance';

comment on column public.agent_runs.provider is
  'Who served this run: `instance` for the deployment''s own model endpoint, '
  'otherwise the organisation''s chosen provider (ENT-236).';

-- No check constraint, for the reason the provider column above has none: the
-- permitted set is deployment configuration.

-- +goose Down

alter table public.agent_runs drop column provider;

drop policy if exists org_model_config_agent on public.org_model_config;
drop policy if exists org_model_config_update_org on public.org_model_config;
drop policy if exists org_model_config_insert_org on public.org_model_config;
drop policy if exists org_model_config_select_org on public.org_model_config;

drop table if exists public.org_model_config;
