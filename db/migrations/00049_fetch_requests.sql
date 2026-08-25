-- +goose Up
-- 00049_fetch_requests.sql (ENT-279)
--
-- The agent asks, and the ask is a row.
--
-- WHAT THIS IS
--
-- ENT-279's settled shape is a mediated fetch: the Watcher asks core-api for
-- a fetch of one granted tool on one connection, core-api decides whether the
-- ask stands, and the fetch itself happens later, through the workers gateway,
-- exactly as a scheduled fetch does. This table is the middle of that
-- sentence: an ask core-api accepted, durable, so "queued" in the
-- acknowledgement the agent gets is a fact rather than a hope.
--
-- WHAT THIS DELIBERATELY IS NOT
--
-- It is not a widening of the producer role. `kindlast_agent` keeps the
-- column-limited select on `integrations` that 00025 argued for, omitting
-- `credential_ciphertext`, and nothing here brings the role a step closer to
-- dialling anybody: a row in this table causes a fetch the way a stale
-- observation does, by being picked up by the scheduled relay, which reads
-- the credential on the application role under the standing consent of the
-- person who connected the system (the scheduled half of ENT-279, landing
-- separately).
-- The role that runs models writes a request; every hop that touches a
-- credential or a network happens on roles and processes it cannot reach.
--
-- A REQUEST IS A RECORD, NOT A JOB
--
-- No status column, no attempts, no settle, and that is the difference from
-- `executor_jobs` (00036), on purpose. A job row carries authority (whose
-- approval executes) and so must be claimed and settled by the role that
-- executes it. A fetch request carries none: the authority a fetch runs under
-- comes from the connection's own consent record, not from the ask, so there
-- is nothing to hand over and nothing to mark done. Whether a request has
-- been served is derivable: an `integration_fetches` attempt for the same
-- pair, requested after the request was created, is the request served, and
-- the relay's due-listing derives exactly that rather than writing back.
-- What the row does permanently is answer "why was this customer's system
-- dialled outside its schedule", which is the question an agent-caused fetch
-- exists to be asked.
--
-- HOW OFTEN AN AGENT CAN CAUSE A DIAL IS GO'S DECISION, NOT A CONSTRAINT
--
-- The cooldown (how recently a pair may have been attempted before an ask is
-- answered from the record instead of queued) and the pending window (how
-- long an unserved request keeps answering `already_queued`) are thresholds
-- that could reasonably change next quarter, so they live in Go per 00016's
-- rule, as parameters the service passes. What Postgres holds is the
-- invariants: the ask names a real connection in a real organisation, the
-- tool is not blank, and the reason cannot become an essay.

create table public.fetch_requests (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- The connection and tool the ask names. The grant and write-capability
  -- checks are NOT constraints here: they are decided at ask time in Go and
  -- decided again at fetch time by the relay's own filters, because a grant
  -- withdrawn between the ask and the fetch must stop the fetch, and a
  -- constraint checked at insert could not.
  integration_id uuid not null
    references public.integrations(id) on delete cascade,
  tool text not null,
  constraint fetch_requests_tool_check check (btrim(tool) <> ''),

  -- The model's own sentence about why, for the person reading the request
  -- later. Bounded because it is model output on its way into a customer's
  -- record: a sentence explains, an essay smuggles.
  reason text not null default '',
  constraint fetch_requests_reason_bounded check (char_length(reason) <= 500),

  created_at timestamptz not null default now()
);

-- The two questions asked of this table: "is an ask for this pair already
-- waiting" (the ask path) and "which pending requests are due" (the relay's
-- union arm, when it lands). Both are pair-then-recency.
create index fetch_requests_pair_recency_idx
  on public.fetch_requests (integration_id, tool, created_at desc);

alter table public.fetch_requests enable row level security;
alter table public.fetch_requests force row level security;

------------------------------------------------------------------------------
-- kindlast_agent: ask, and see its own organisation's asks. Nothing else.
------------------------------------------------------------------------------
--
-- The producer role inserts (the ask arrives through WatcherService, on the
-- producer pool, with the organisation GUC set) and selects (to answer
-- `already_queued` without a second pool). Both policies are org equality in
-- the one-argument form, per 00037: a producer path that forgot to say whose
-- data it wants fails loudly on the first call rather than reading zero rows
-- quietly. No membership `exists`, for the reason every producer policy has
-- none: the role holds no grant on `memberships` and no human is behind it.
--
-- No update and no delete, for anyone. A request that has been served is
-- visible as served through `integration_fetches`; rewriting or removing the
-- ask would un-answer the question the row exists to answer. The only thing
-- that removes one is the cascade from `organisations`, which is how erasing
-- an organisation already works.
--
-- `kindlast_app` holds nothing here, deliberately. The console does not show
-- these yet, and the relay's cross-tenant due-listing arrives as an arm of
-- the scheduled fetch's own definer listing rather than as an open select
-- policy, so nothing needs the open form 00037 spent a migration narrowing.

grant select, insert on public.fetch_requests to kindlast_agent;

create policy fetch_requests_agent on public.fetch_requests
  for select
  to kindlast_agent
  using (org_id = (select current_setting('app.current_org_id')::uuid));

create policy fetch_requests_agent_insert on public.fetch_requests
  for insert
  to kindlast_agent
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

-- +goose Down

drop policy if exists fetch_requests_agent_insert on public.fetch_requests;
drop policy if exists fetch_requests_agent        on public.fetch_requests;

revoke all on public.fetch_requests from kindlast_agent;

drop table if exists public.fetch_requests;
