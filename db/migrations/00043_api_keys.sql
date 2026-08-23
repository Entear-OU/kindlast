-- +goose Up
-- 00043_api_keys.sql (ENT-262, §23, §1.7, §24 step 7b)
--
-- The third token model: a partner's key. Session for the web console,
-- bearer-on-device for mobile, and this for everything that is neither.
--
-- WHAT A KEY IS, AND WHAT IT DELIBERATELY IS NOT
--
-- A key is a long-lived credential bound to ONE organisation and minted from
-- inside a live human session. It is not an identity. It has no membership of
-- its own, it belongs to nobody, and it cannot be handed authority directly.
-- What it carries instead is a pointer to the person who minted it, and every
-- request it makes runs under that person's membership, checked afresh on each
-- call by the same `memberships` policy every human request already runs under.
--
-- That is the whole design, and the consequences are the reason for it:
--
--   * Offboarding a person kills their keys, on the next request, with no
--     sweep to run and nothing to remember. A key that outlived its minter
--     would be an access path that survives a leaver, which is exactly the gap
--     PR #229 closed for humans and would be silly to reopen for machines.
--   * There is no second policy surface. A key reads what its minter reads and
--     writes where its minter may write, because it IS its minter as far as
--     Postgres is concerned. Nothing here adds a policy for keys, and that is
--     the property to protect if this table ever grows.
--   * A key can be narrower than its minter but never wider. `scopes` below is
--     a subset check performed in Go at mint time; the ceiling that no code can
--     raise is the constraint on this table plus the membership the request
--     runs under.
--
-- WHY THE SECRET IS SHA-256 AND NOT ARGON2, WHICH LOOKS LIKE THE WRONG ANSWER
--
-- A password hash exists to make brute force expensive over a space a human
-- chose, which is small. The secret half of a key is 32 bytes from the system
-- CSPRNG, so the space is 2^256 and there is nothing to slow down: an attacker
-- who can enumerate that does not need the seconds argon2 would cost them. What
-- a per-request argon2 WOULD buy, reliably, is a denial-of-service surface on
-- an unauthenticated path, because the work happens before the caller has
-- proved anything.
--
-- The property a password hash gives that this does not is resistance to a
-- stolen digest being cracked back to the credential. At 256 bits of uniform
-- entropy that is not a property worth paying for, it is arithmetic. GitHub and
-- Stripe hash their tokens the same way for the same reason.
--
-- The comparison still has to be constant time, and it is, in Go, with
-- crypto/subtle. See `internal/domain/apikey`.
--
-- WHY THERE IS A PUBLIC `key_id` AT ALL
--
-- So that authentication is an index lookup rather than a scan. Without a
-- lookup handle, verifying a presented key means hashing it and comparing
-- against every row, which is a full scan of a credential table on every
-- request. The handle is the first half of the credential, it is not secret,
-- and it is stored in the clear precisely so it can be indexed and shown in a
-- console next to the key's name.
--
-- WHY REVOCATION IS A COLUMN AND NOT A DELETE
--
-- Same reason `act_delegations` revokes rather than erases (00021): what a
-- customer granted, to whom, and when they took it back is evidence, and
-- evidence that can be deleted is worth less than evidence that cannot. The app
-- holds no delete grant on this table.

------------------------------------------------------------------------------
-- The scope ceiling, as a function so a CHECK can call it
------------------------------------------------------------------------------
-- A check constraint may not contain a subquery, and `not exists (... unnest
-- ...)` is one. An IMMUTABLE function over the argument alone is the form
-- Postgres accepts, and it is genuinely immutable: it reads no table, no
-- setting and no clock.
--
-- `coalesce(..., false)` matters. `bool_and` over an empty array is NULL, and a
-- CHECK that evaluates to NULL PASSES. Without the coalesce, an empty scope
-- array would slip through this constraint; `api_keys_scopes_present` catches
-- that case too, and a security constraint should not depend on a second
-- constraint being remembered.
-- +goose StatementBegin
create function public.api_key_scopes_are_tenant_only(p_scopes text[])
returns boolean
language sql
immutable
set search_path to 'pg_catalog', 'pg_temp'
as $$
  select coalesce(bool_and(s not like 'internal:%'), false)
  from unnest(p_scopes) as s
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create table public.api_keys (
  id uuid primary key default gen_random_uuid(),

  org_id uuid not null references public.organisations(id) on delete cascade,

  -- The public half of the credential and the lookup handle. Sixteen lowercase
  -- hex characters, which is eight bytes: enough that two keys never collide,
  -- and not a secret, so the shape is checked rather than guarded.
  key_id text not null,
  constraint api_keys_key_id_key unique (key_id),
  constraint api_keys_key_id_shape check (key_id ~ '^[0-9a-f]{16}$'),

  -- The digest of the secret half. Exactly 32 bytes, because it is a SHA-256
  -- and a row carrying anything else is a row written by something that did not
  -- understand what this column is.
  --
  -- `kindlast_app` holds NO SELECT ON THIS COLUMN. See the grants below: that
  -- is a column-level grant rather than an oversight, and it is what keeps the
  -- ordinary listing path structurally unable to read a digest even for its own
  -- organisation. The one thing that reads it is `authenticate_api_key`, which
  -- is SECURITY DEFINER and answers only for an exact key id.
  secret_hash bytea not null,
  constraint api_keys_secret_hash_length check (octet_length(secret_hash) = 32),

  -- What a person calls it, so a console can show which key to revoke. Bounded
  -- because it is rendered next to an audit row a customer reads.
  name text not null,
  constraint api_keys_name_check check (length(btrim(name)) between 1 and 100),

  -- What this key may do, and it is a ceiling rather than a grant: the request
  -- still runs under the minter's membership and every RLS policy still
  -- applies, so a scope here can only ever narrow.
  scopes text[] not null,
  constraint api_keys_scopes_present check (cardinality(scopes) > 0),

  -- A KEY CAN NEVER CARRY THE PLATFORM SURFACE. THIS IS THE INVARIANT.
  --
  -- `internal:*` is the vocabulary that acts ACROSS organisations: ingesting
  -- the corpus, running a sweep, acting on behalf of a person. A partner
  -- credential holding one of those would be a tenant-scoped credential with a
  -- cross-tenant verb attached, which is the one shape this whole schema exists
  -- to make impossible.
  --
  -- In the database rather than in Go, and the AGENTS.md test is what decides
  -- that: it must hold no matter who writes, including a future handler, a
  -- migration, or somebody at a psql prompt. Which of the ordinary scopes a
  -- partner key may carry IS a decision and IS in Go
  -- (`apikey.GrantableScopes`), because that list will change and this will
  -- not.
  constraint api_keys_no_internal_scope check (public.api_key_scopes_are_tenant_only(scopes)),

  -- The person whose authority this key borrows. Not a foreign key, matching
  -- every other user reference in this schema: identity is the IdP's and the
  -- domain mirrors rather than owns it.
  created_by uuid not null,
  created_at timestamptz not null default now(),

  -- WRITTEN BY THE AUTHENTICATOR, NOT BY THE APP.
  --
  -- Coarsened to a minute inside `touch_api_key` rather than written on every
  -- request. An exact last-used timestamp would mean a write on every single
  -- API call, which turns a read-only request into a write and puts a row lock
  -- on the hot path of the busiest key. A minute is the resolution the question
  -- "is this key still in use" actually needs.
  last_used_at timestamptz,

  -- Revocation, one way. The trigger below is what stops an update being a way
  -- to un-revoke.
  revoked_at timestamptz,
  revoked_by uuid,
  constraint api_keys_revocation_is_whole
    check ((revoked_at is null) = (revoked_by is null))
);
-- +goose StatementEnd

-- The two reads this table gets. Authentication is by `key_id`, served by the
-- unique index above. The console lists an organisation's keys newest first.
create index api_keys_org_created_idx
  on public.api_keys (org_id, created_at desc);

------------------------------------------------------------------------------
-- A key cannot be widened, or resurrected, after it is minted
------------------------------------------------------------------------------
-- The update grant below exists so a key can be revoked. Without this trigger
-- that same grant would also be a way to add a scope to a key somebody already
-- holds, to repoint it at another organisation, or to clear `revoked_at` and
-- turn a credential the customer has already stopped back into a working one.
--
-- A trigger rather than a policy, because it binds EVERY role including the
-- migrator, and "a revoked key stays revoked" is a claim this table makes to a
-- customer rather than to its own application. Same argument as
-- `act_delegations_narrow_update` (00021) and `agent_runs_no_update` (00019).
-- +goose StatementBegin
create function public.api_keys_narrow_update() returns trigger
  language plpgsql
  as $$
begin
  if (new.id, new.org_id, new.key_id, new.secret_hash, new.name, new.scopes,
      new.created_by, new.created_at)
     is distinct from
     (old.id, old.org_id, old.key_id, old.secret_hash, old.name, old.scopes,
      old.created_by, old.created_at)
  then
    raise exception 'api_keys: only last_used_at, revoked_at and revoked_by may change on row %', old.id
      using errcode = 'check_violation';
  end if;

  if old.revoked_at is not null
     and (new.revoked_at is null or new.revoked_at is distinct from old.revoked_at)
  then
    raise exception 'api_keys: row % is revoked and cannot be un-revoked', old.id
      using errcode = 'check_violation';
  end if;

  return new;
end;
$$;
-- +goose StatementEnd

create trigger api_keys_narrow_update
  before update on public.api_keys
  for each row execute function public.api_keys_narrow_update();

alter table public.api_keys enable row level security;
alter table public.api_keys force row level security;

------------------------------------------------------------------------------
-- Grants. THIS TABLE STARTS CLOSED (ENT-243)
------------------------------------------------------------------------------
-- Nothing arrives attached since 00029, so what follows is the whole privilege
-- surface rather than a narrowing of a default.
--
-- THE SELECT IS COLUMN-LEVEL, AND THAT IS THE POINT.
--
-- RLS is row-level: a select policy scoped to the caller's organisation still
-- lets the statement read every COLUMN of every row it admits, `secret_hash`
-- included. A column grant is the only thing in Postgres that can say "this
-- role may list its keys and may never read a digest", and since the listing
-- surface exists, saying it is worth the extra line.
grant select (
  id, org_id, key_id, name, scopes,
  created_by, created_at, last_used_at, revoked_at, revoked_by
) on public.api_keys to kindlast_app;

-- Insert takes every column the mint writes, `secret_hash` among them. Writing
-- a column a role cannot read back is exactly what is wanted here.
grant insert (
  id, org_id, key_id, secret_hash, name, scopes, created_by
) on public.api_keys to kindlast_app;

-- Update is revocation and nothing else. `last_used_at` is deliberately absent:
-- it is written by `touch_api_key`, which runs as the owner, so the application
-- has no way to backdate or clear it.
grant update (revoked_at, revoked_by) on public.api_keys to kindlast_app;

-- NO DELETE, for anybody. A revoked key is a record of access that existed.

-- Nothing at all for `kindlast_agent`, `kindlast_ingest` or `kindlast_billing`.
-- The sweep role has no organisation, and the two write-path roles have no
-- business near a table of credentials.

------------------------------------------------------------------------------
-- Policies, in the two-GUC form
------------------------------------------------------------------------------
-- READING: every member of the organisation sees its keys.
--
-- Not "only the person who minted it", which is where `act_delegations` landed,
-- and the difference is deliberate. A delegation names one person's authority
-- for the length of one run and nobody else needs to see it. An API key is
-- infrastructure the organisation runs on: an owner has to be able to find the
-- key that is still calling after a contractor left, and a key nobody but its
-- author can see is a key nobody revokes.
create policy api_keys_select on public.api_keys
  for select to kindlast_app
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- MINTING: in the caller's organisation, in the caller's name, and only while
-- they are a member of it.
--
-- `created_by` is pinned to the GUC user rather than taken from the request,
-- which is the same anti-laundering rule `act_delegations_mint` encodes. A key
-- borrows its minter's authority on every later request, so a handler that
-- could name somebody else as the minter could mint a key that acts as them.
-- Postgres refuses that rather than the handler remembering not to.
create policy api_keys_mint on public.api_keys
  for insert to kindlast_app
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and created_by = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- REVOKING: any member of the organisation may revoke any of its keys.
--
-- The same row set the select policy admits, on purpose: a key you can see is a
-- key you can stop. Whether a viewer SHOULD be able to is a role threshold and
-- therefore a decision, so it lives in Go, where the handler requires an owner.
-- The policy is the boundary (never another organisation's key) and the handler
-- is the rule.
create policy api_keys_revoke on public.api_keys
  for update to kindlast_app
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
  );

------------------------------------------------------------------------------
-- Authentication, for a caller who has no session yet
------------------------------------------------------------------------------
-- SECURITY DEFINER for exactly one reason and no other: every policy above
-- reads `app.current_org_id`, and deciding what that should be is what this
-- call is FOR. The caller has not been identified yet, so RLS structurally
-- cannot express the check. That is the same argument `resolve_act_delegation`
-- (00021) and `redeem_capability_token` (00015) rest on, and it is the narrow
-- exception AGENTS.md allows rather than a licence to add the next function.
--
-- Deliberately mechanical. It finds a live row by its public handle and returns
-- what the row says. It checks NO MEMBERSHIP, because that is the next call's
-- job, run in Go against the same `memberships` policy every human request
-- already uses. A person removed from the organisation is therefore refused on
-- their key's next request rather than at some sweep, and the check that
-- refuses them is the one the whole system already depends on.
--
-- IT RETURNS THE DIGEST, AND THE COMPARISON HAPPENS IN GO.
--
-- Comparing in here would mean `bytea = bytea`, which is memcmp and short
-- circuits. The exposure is not the timing (an attacker can only vary the
-- digest by guessing preimages, so there is nothing to walk) but the rule is
-- cheaper to keep than to re-derive: credential comparisons are constant time,
-- always, and crypto/subtle is where that is available. Handing the digest back
-- is what makes that possible, and the column grant above is what stops it
-- being readable any other way.
--
-- ONE ANSWER FOR EVERY UNUSABLE KEY. Revoked and never existed both return zero
-- rows. Distinguishing them would make this an oracle for which key ids are
-- real.
-- +goose StatementBegin
create function public.authenticate_api_key(p_key_id text)
returns table (id uuid, org_id uuid, created_by uuid, scopes text[], secret_hash bytea)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $$
  select k.id, k.org_id, k.created_by, k.scopes, k.secret_hash
  from public.api_keys k
  where k.key_id = p_key_id
    and k.revoked_at is null
$$;
-- +goose StatementEnd

revoke all on function public.authenticate_api_key(text) from public;
grant execute on function public.authenticate_api_key(text) to kindlast_app;

-- Recording that a key was used, coarsened to a minute.
--
-- SECURITY DEFINER for the same reason as the lookup: it runs before there is a
-- session, so no policy can admit it. Separate from the lookup rather than
-- folded into it, because it must run only AFTER the digest matched. A touch
-- inside the lookup would move `last_used_at` for a key somebody merely guessed
-- the public handle of, which turns the console's "last used" column into a
-- report of failed attacks.
--
-- The one-minute floor is what keeps a read-only API call from taking a row
-- lock on every request. See the column comment.
-- +goose StatementBegin
create function public.touch_api_key(p_id uuid)
returns void
language sql
volatile
security definer
set search_path to 'public', 'pg_temp'
as $$
  update public.api_keys
     set last_used_at = now()
   where id = p_id
     and revoked_at is null
     and (last_used_at is null or last_used_at < now() - interval '1 minute')
$$;
-- +goose StatementEnd

revoke all on function public.touch_api_key(uuid) from public;
grant execute on function public.touch_api_key(uuid) to kindlast_app;

------------------------------------------------------------------------------
-- The audit row names the key
------------------------------------------------------------------------------
-- §23 asks that a key acting on a finding be an actor in the regulatory record.
-- `audit_log` already carries `user_id` (whose authority) and `acting_agent`
-- (what was holding the pen, 00021); this is the third of that set and it names
-- the credential.
--
-- WHY A DEFAULT AND NOT A PARAMETER ON record_audit_log
--
-- Copied deliberately from 00021, which made the same choice for the same
-- reason. Every audit row today goes through `record_audit_log`, so widening
-- its signature would work today. A default also catches a writer that inserts
-- directly, including the three Executor triggers that call `record_audit_log`
-- from inside an UPDATE, and including one written years from now by somebody
-- who never read this migration. For a table whose claim is "nobody, including
-- us, can revise this", the mechanism that cannot be forgotten is the right
-- one.
--
-- The GUC is transaction-local, set by the API key request's transaction
-- alongside the two tenancy GUCs, and it ends at commit because `set_config`
-- with `is_local` true does. It is NOT a third tenancy GUC: no policy reads it
-- and nothing decides anything from it. It labels a row.
--
-- NULL is the ordinary case of a person acting at a keyboard, and that is the
-- point of a nullable column: one that was never null would say nothing.
alter table public.audit_log
  add column actor_api_key_id uuid
  default nullif(current_setting('app.current_api_key_id', true), '')::uuid;

-- +goose Down
alter table public.audit_log drop column if exists actor_api_key_id;

drop function if exists public.touch_api_key(uuid);
drop function if exists public.authenticate_api_key(text);

drop policy if exists api_keys_revoke on public.api_keys;
drop policy if exists api_keys_mint on public.api_keys;
drop policy if exists api_keys_select on public.api_keys;
revoke all on public.api_keys from kindlast_app;

drop table if exists public.api_keys;
-- After the table, because dropping it takes the trigger with it and the
-- function would otherwise be dropped out from under a live trigger.
drop function if exists public.api_keys_narrow_update();
drop function if exists public.api_key_scopes_are_tenant_only(text[]);
