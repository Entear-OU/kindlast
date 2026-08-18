-- +goose Up
-- 00027_act_from_email.sql (ENT-249, §8, §1.8, §26.3)
--
-- The one-tap approve link, as a second redemption path for the ENT-230
-- delegation rather than as a second kind of bearer credential.
--
-- WHAT 00021 LEFT UNBUILT, AND WHY IT IS FOUR SMALL THINGS RATHER THAN A TABLE
--
-- 00021 said the second consumer would be "a redemption path plus a column,
-- not a second table of bearer credentials", and committed `single_use` in
-- advance because that was the difference it could already name. This is the
-- rest: the column that binds a delegation to one finding, the resolution rule
-- that makes the binding bite, the gate that keeps authority away from an
-- address nobody proved they control, and the narrow way the dispatcher mints
-- one for somebody who is not signed in.
--
-- `capability_tokens.kind` is NOT widened here, and 00015's comment predicting
-- that it would be is now wrong rather than merely early. The reason is the one
-- 00021 gave: `redeem_capability_token` PERFORMS the act inside redemption, so a
-- widened `kind` would mean a second implementation of approval living in
-- plpgsql beside the one in Go. A delegation performs nothing. It answers "who
-- is this" and hands the answer to the ordinary act path, which is what makes an
-- approval from an email indistinguishable in the database from the same
-- person's approval at the keyboard, except in what is named beside it.
--
-- WHY THE LINK NAMES THE FINDING AS WELL AS CARRYING THE TOKEN
--
-- The credential is bound to one finding here, and the caller redeeming it has
-- to say which finding it is for. Both halves are needed and the second is the
-- one that is easy to leave out.
--
-- Binding alone stops a token minted for a low severity finding approving a
-- critical one, which is the §8 failure. Requiring the caller to name the
-- finding as well means the token on its own is not enough to act: a token
-- recovered from a mail relay's logs, where the URL was truncated or the body
-- was never stored, approves nothing. It also gives the "wrong finding" case one
-- answer with every other unusable case, in the database rather than in a Go
-- comparison somebody could later reorder.
--
-- The mechanism is `is not distinct from`, which is symmetrical and therefore
-- does two jobs at once. A finding-bound delegation cannot be resolved by a
-- caller who names no finding, so the approve link cannot be presented in the
-- `Kindlast-Delegation` header as a general purpose session for that person. And
-- a run delegation cannot be resolved by naming a finding, so the rail's
-- credential cannot be spent through the approve endpoint.

-- +goose StatementBegin
-- The address, and whether anybody proved it belongs to the person.
--
-- §1.8 gates acting on a finding behind a verified address, and until now that
-- gate lived entirely in a claim on a live access token. A link in an email has
-- no token, so the fact has to be mirrored here, exactly as `email` already is
-- and for the same reason: this table is the reverse of a one-way derivation,
-- and what the IdP said about the address is part of the answer.
--
-- Default false, and NOT backfilled to true for the rows that already exist.
-- Backfilling would fabricate a verification nobody performed, in the one table
-- whose job is to say what is actually known. Every existing identity therefore
-- reads as unverified until its owner signs in again and provisioning records
-- what the token said, which is the fail-closed direction: the cost is an email
-- that carries no approve link, and the alternative cost is authority handed to
-- an unverified address.
alter table public.user_identities
  add column email_verified boolean not null default false;
-- +goose StatementEnd

comment on column public.user_identities.email_verified is
  'What the IdP said about this address at the last sign-in that carried one. Gates the act-from-email link (ENT-249).';

------------------------------------------------------------------------------
-- The finding binding
------------------------------------------------------------------------------

-- Null for a run delegation, which is every delegation minted before this
-- migration. `on delete cascade` because a delegation to approve a finding that
-- no longer exists is not a credential worth keeping: it can never resolve, and
-- keeping it would leave a live-looking row pointing at nothing.
alter table public.act_delegations
  add column finding_id uuid references public.findings(id) on delete cascade;

-- A finding-bound delegation is single use, as an invariant rather than as a
-- habit of the minting code. The approve link is the consumer `single_use`
-- was added for in 00021, and a finding-bound row that could be redeemed twice
-- would approve, be un-approved by a human, and approve again from the same
-- mail.
alter table public.act_delegations
  add constraint act_delegations_finding_is_single_use
  check (finding_id is null or single_use);

-- Resolution by hash is served by the unique index on `token_hash`. This is for
-- the other direction: "what is outstanding against this finding", which is what
-- a revoke-on-approval sweep would need and what an operator asks after a
-- customer reports a link they did not expect.
create index act_delegations_finding_idx
  on public.act_delegations (finding_id)
  where finding_id is not null;

-- The narrow-update trigger gains the new column.
--
-- Without this, `finding_id` would be the one field on a minted delegation that
-- an update could repoint, which is precisely the widening the original trigger
-- exists to prevent: a credential already sitting in somebody's mailbox would
-- start approving a different finding.
-- +goose StatementBegin
create or replace function public.act_delegations_narrow_update() returns trigger
  language plpgsql
  as $$
begin
  if (new.id, new.org_id, new.user_id, new.acting_agent, new.token_hash,
      new.single_use, new.finding_id, new.expires_at, new.created_at)
     is distinct from
     (old.id, old.org_id, old.user_id, old.acting_agent, old.token_hash,
      old.single_use, old.finding_id, old.expires_at, old.created_at)
  then
    raise exception 'act_delegations: only revoked_at and redeemed_at may change on row %', old.id
      using errcode = 'check_violation';
  end if;

  if old.revoked_at is not null and new.revoked_at is null then
    raise exception 'act_delegations: row % is revoked and cannot be un-revoked', old.id
      using errcode = 'check_violation';
  end if;
  if old.redeemed_at is not null and new.redeemed_at is null then
    raise exception 'act_delegations: row % is redeemed and cannot be un-redeemed', old.id
      using errcode = 'check_violation';
  end if;

  return new;
end;
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- §1.8's gate, held by the table rather than by whoever writes to it
------------------------------------------------------------------------------
--
-- A finding-bound delegation is a credential that travels to an email address,
-- so the address has to be one somebody proved they control. Approving a
-- finding is a regulatory decision recorded in a customer's compliance record,
-- and an unverified address is, by definition, one that may belong to whoever
-- typed it during sign-up.
--
-- A trigger rather than a check in the dispatcher, because it must hold no
-- matter who writes: the mint below runs as the schema owner and bypasses RLS,
-- so a policy would not bind it, and a Go check binds only the caller that
-- remembers to make it. The dispatcher still asks first, so that an unverified
-- recipient gets an email without an approve link rather than an exception that
-- fails the whole delivery. That is the split delegation.go already describes:
-- the readable refusal is in Go, the boundary is here.
--
-- Scoped to finding-bound rows on purpose. A run delegation is minted while its
-- owner is signed in, holding a token this deployment verified, and requiring a
-- mirrored flag there would break the rail for every identity provisioned
-- before this migration for no gain.
-- +goose StatementBegin
create function public.act_delegations_verified_address() returns trigger
  language plpgsql
  as $$
begin
  if new.finding_id is not null
     and not exists (
       select 1 from public.user_identities ui
       where ui.user_id = new.user_id
         and ui.email_verified
     )
  then
    raise exception 'act_delegations: % has no verified address, so no finding-bound delegation may be minted for them', new.user_id
      using errcode = 'check_violation';
  end if;
  return new;
end;
$$;
-- +goose StatementEnd

create trigger act_delegations_verified_address
  before insert on public.act_delegations
  for each row execute function public.act_delegations_verified_address();

------------------------------------------------------------------------------
-- Resolution, now with the finding in the question
------------------------------------------------------------------------------
--
-- Dropped and recreated rather than replaced, because a new argument makes a
-- new function rather than a new body, and leaving both would make the
-- one-argument call ambiguous.
--
-- The default is null so the existing call site keeps its meaning: a caller
-- that names no finding resolves unbound delegations only. That is also the
-- safe direction for a caller written later that forgets the argument, since
-- forgetting it narrows what resolves rather than widening it.
drop function if exists public.resolve_act_delegation(text);

-- +goose StatementBegin
create function public.resolve_act_delegation(
  p_token_hash text,
  p_finding_id uuid default null
)
returns table (user_id uuid, org_id uuid, acting_agent text)
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $function$
declare
  v public.act_delegations%rowtype;
begin
  select * into v
  from public.act_delegations d
  where d.token_hash = p_token_hash
    and d.revoked_at is null
    and d.expires_at > now()
    -- Symmetrical, which is the point. A finding-bound delegation is invisible
    -- to a caller naming no finding, and a run delegation is invisible to one
    -- naming a finding, so neither credential can be spent through the other's
    -- path.
    and d.finding_id is not distinct from p_finding_id
  for update;

  if not found then
    return;
  end if;

  if v.single_use then
    if v.redeemed_at is not null then
      return;
    end if;
    update public.act_delegations d
       set redeemed_at = now()
     where d.id = v.id;
  end if;

  user_id := v.user_id;
  org_id := v.org_id;
  acting_agent := v.acting_agent;
  return next;
end;
$function$;
-- +goose StatementEnd

revoke all on function public.resolve_act_delegation(text, uuid) from public;
grant execute on function public.resolve_act_delegation(text, uuid) to kindlast_app;

------------------------------------------------------------------------------
-- What the dispatcher needs to know before it puts a link in a message
------------------------------------------------------------------------------
--
-- `notification_recipients` gains one column and keeps its shape. Dropped and
-- recreated because a `returns table` signature cannot be replaced in place.
--
-- The new column answers a question about the address in the row above it,
-- rather than adding a new fact about the person: is THIS address, the one this
-- message is going to, one the IdP said was verified.
--
-- That distinction is what makes the preferences override behave correctly
-- without a rule in Go. `notification_preferences.email` exists so somebody can
-- be told somewhere other than where they sign in, and nobody has proved they
-- control that second address. So a recipient reading mail at an override
-- address is `email_verified = false` here even when their sign-in address is
-- verified, and their message carries no approve link. The doorbell still
-- arrives; only the authority does not.
drop function if exists public.notification_recipients(uuid);

-- +goose StatementBegin
create function public.notification_recipients(p_outbox_id uuid)
returns table (
  user_id            uuid,
  email              text,
  email_verified     boolean,
  min_severity       public.severity_level,
  finding_severity   public.severity_level,
  timezone           text,
  quiet_hours_start  time,
  quiet_hours_end    time,
  org_slug           text,
  org_name           text
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select
    m.user_id,
    coalesce(nullif(np.email, ''), ui.email)                     as email,
    coalesce(
      ui.email_verified
        and coalesce(nullif(np.email, ''), ui.email) = ui.email,
      false)                                                     as email_verified,
    coalesce(np.min_severity_for_email, 'medium'::public.severity_level)
                                                                 as min_severity,
    f.severity                                                   as finding_severity,
    coalesce(np.timezone, 'Europe/Tallinn')                      as timezone,
    np.quiet_hours_start,
    np.quiet_hours_end,
    org.slug                                                     as org_slug,
    org.name                                                     as org_name
  from public.notification_outbox o
  join public.findings f
    on f.id = o.finding_id
  join public.organisations org
    on org.id = o.org_id
  join public.memberships m
    on m.org_id = o.org_id
  left join public.notification_preferences np
    on np.org_id = o.org_id and np.user_id = m.user_id
  left join public.user_identities ui
    on ui.user_id = m.user_id
  where o.id = p_outbox_id
    and coalesce(nullif(np.email, ''), ui.email) is not null;
$function$;
-- +goose StatementEnd

revoke all on function public.notification_recipients(uuid) from public;
grant execute on function public.notification_recipients(uuid) to kindlast_agent;

------------------------------------------------------------------------------
-- Minting one for somebody who is not signed in
------------------------------------------------------------------------------
--
-- WHY THIS EXISTS AT ALL, WHICH IS THE UNCOMFORTABLE PART
--
-- 00021 says a delegation can only be minted from inside a transaction that is
-- already the person's, and that this is what stops the minting side naming
-- somebody else. The dispatcher breaks that shape by being the one legitimate
-- minter with no session: it is draining an outbox row on the `kindlast_agent`
-- pool, and the person it is minting for is asleep.
--
-- The honest options were a grant plus a policy for `kindlast_agent`, or this.
-- A policy cannot be written: it would have to check membership, `kindlast_agent`
-- deliberately holds no grant on `memberships` (00008), and a policy expression
-- reading a table the querying role cannot read errors rather than refuses. A
-- grant without that check would let the dispatcher mint a delegation naming any
-- user id it liked, which is exactly the authority laundering 00021 exists to
-- prevent.
--
-- So this is the same shape `notification_recipients` already has and it is
-- narrow for the same reasons. The caller passes an OUTBOX ROW IT HAS CLAIMED,
-- not an organisation and not a finding: both are read from that row, so the
-- dispatcher cannot pair a person with a finding it was not sent to deliver.
-- The person must be a member of that row's organisation. The lifetime is
-- checked by the table's own ceiling. The address gate is the trigger above.
-- `kindlast_agent` gains no privilege on `act_delegations` and still cannot
-- select one back: it holds the token it generated and nothing else.
--
-- WHAT IT REFUSES BY ANSWERING NULL RATHER THAN RAISING
--
-- An outbox row that vanished, one with no finding, and a person who is not a
-- member. All three are ordinary races against a dispatcher that claimed a row
-- moments ago, and an exception would abort the delivery transaction and retry
-- the whole notification forever. The address gate is the exception, because
-- reaching it means the caller did not ask first, and that is a bug to see.
-- +goose StatementBegin
create function public.mint_finding_approval_delegation(
  p_outbox_id  uuid,
  p_user_id    uuid,
  p_token_hash text,
  p_lifetime   interval
)
returns uuid
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_org     uuid;
  v_finding uuid;
  v_id      uuid;
begin
  select o.org_id, o.finding_id into v_org, v_finding
  from public.notification_outbox o
  where o.id = p_outbox_id;

  if v_org is null or v_finding is null then
    return null;
  end if;

  if not exists (
    select 1 from public.memberships m
    where m.org_id = v_org and m.user_id = p_user_id
  ) then
    return null;
  end if;

  v_id := gen_random_uuid();

  -- `email` names the channel, not a skill. §26.3 asks the audit row to say
  -- what was holding the pen, and for this path the answer is the medium the
  -- decision arrived through, which is what a person reading the trail needs in
  -- order to ask whether a link in a mailbox should have been able to do this.
  insert into public.act_delegations
    (id, org_id, user_id, acting_agent, token_hash, single_use, finding_id, expires_at)
  values
    (v_id, v_org, p_user_id, 'email', p_token_hash, true, v_finding, now() + p_lifetime);

  return v_id;
end;
$function$;
-- +goose StatementEnd

revoke all on function public.mint_finding_approval_delegation(uuid, uuid, text, interval) from public;
grant execute on function public.mint_finding_approval_delegation(uuid, uuid, text, interval)
  to kindlast_agent;

-- +goose Down

drop function if exists public.mint_finding_approval_delegation(uuid, uuid, text, interval);

-- notification_recipients, restored to its 00015 shape.
drop function if exists public.notification_recipients(uuid);

-- +goose StatementBegin
create function public.notification_recipients(p_outbox_id uuid)
returns table (
  user_id            uuid,
  email              text,
  min_severity       public.severity_level,
  finding_severity   public.severity_level,
  timezone           text,
  quiet_hours_start  time,
  quiet_hours_end    time,
  org_slug           text,
  org_name           text
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select
    m.user_id,
    coalesce(nullif(np.email, ''), ui.email)                     as email,
    coalesce(np.min_severity_for_email, 'medium'::public.severity_level)
                                                                 as min_severity,
    f.severity                                                   as finding_severity,
    coalesce(np.timezone, 'Europe/Tallinn')                      as timezone,
    np.quiet_hours_start,
    np.quiet_hours_end,
    org.slug                                                     as org_slug,
    org.name                                                     as org_name
  from public.notification_outbox o
  join public.findings f
    on f.id = o.finding_id
  join public.organisations org
    on org.id = o.org_id
  join public.memberships m
    on m.org_id = o.org_id
  left join public.notification_preferences np
    on np.org_id = o.org_id and np.user_id = m.user_id
  left join public.user_identities ui
    on ui.user_id = m.user_id
  where o.id = p_outbox_id
    and coalesce(nullif(np.email, ''), ui.email) is not null;
$function$;
-- +goose StatementEnd

revoke all on function public.notification_recipients(uuid) from public;
grant execute on function public.notification_recipients(uuid) to kindlast_agent;

-- resolve_act_delegation, restored to its 00021 shape.
drop function if exists public.resolve_act_delegation(text, uuid);

-- +goose StatementBegin
create function public.resolve_act_delegation(p_token_hash text)
returns table (user_id uuid, org_id uuid, acting_agent text)
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $function$
declare
  v public.act_delegations%rowtype;
begin
  select * into v
  from public.act_delegations d
  where d.token_hash = p_token_hash
    and d.revoked_at is null
    and d.expires_at > now()
  for update;

  if not found then
    return;
  end if;

  if v.single_use then
    if v.redeemed_at is not null then
      return;
    end if;
    update public.act_delegations d
       set redeemed_at = now()
     where d.id = v.id;
  end if;

  user_id := v.user_id;
  org_id := v.org_id;
  acting_agent := v.acting_agent;
  return next;
end;
$function$;
-- +goose StatementEnd

revoke all on function public.resolve_act_delegation(text) from public;
grant execute on function public.resolve_act_delegation(text) to kindlast_app;

drop trigger if exists act_delegations_verified_address on public.act_delegations;
drop function if exists public.act_delegations_verified_address();

-- +goose StatementBegin
create or replace function public.act_delegations_narrow_update() returns trigger
  language plpgsql
  as $$
begin
  if (new.id, new.org_id, new.user_id, new.acting_agent, new.token_hash,
      new.single_use, new.expires_at, new.created_at)
     is distinct from
     (old.id, old.org_id, old.user_id, old.acting_agent, old.token_hash,
      old.single_use, old.expires_at, old.created_at)
  then
    raise exception 'act_delegations: only revoked_at and redeemed_at may change on row %', old.id
      using errcode = 'check_violation';
  end if;

  if old.revoked_at is not null and new.revoked_at is null then
    raise exception 'act_delegations: row % is revoked and cannot be un-revoked', old.id
      using errcode = 'check_violation';
  end if;
  if old.redeemed_at is not null and new.redeemed_at is null then
    raise exception 'act_delegations: row % is redeemed and cannot be un-redeemed', old.id
      using errcode = 'check_violation';
  end if;

  return new;
end;
$$;
-- +goose StatementEnd

drop index if exists public.act_delegations_finding_idx;
alter table public.act_delegations
  drop constraint if exists act_delegations_finding_is_single_use;
alter table public.act_delegations drop column if exists finding_id;

alter table public.user_identities drop column if exists email_verified;
