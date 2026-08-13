-- ENT-196: what just-in-time provisioning needs and 00002 did not ship.
--
-- Three things, and each one exists because an acceptance criterion cannot be
-- met without it:
--
--   1. user_identities   the reverse of the one-way subject derivation
--   2. organisations.personal_owner_id, with a partial unique index, so a
--      retry cannot produce a second personal organisation
--   3. invitations       so an invited user joins an existing organisation
--                        rather than getting a personal one
--
-- The RLS shape of the third is the interesting part; see the comment above
-- accept_invitation.

-- +goose Up

-- user_identities ------------------------------------------------------------
--
-- memberships.user_id is a uuid. The IdP's subject is not: Zitadel issues
-- snowflake integers such as 386089961457188867, Auth0 issues auth0|abc123,
-- and only some providers happen to issue uuids. So user_id is a version 5
-- uuid derived from (issuer, subject) by libs/chassis/subject (doc §20.1,
-- shipped in ENT-195).
--
-- That derivation is one-way, and this table is the only way back. Two things
-- need the reverse direction and neither is optional: an operator asking "who
-- is this uuid" during an incident, and a subject access request, which in a
-- GDPR product has to be answerable rather than merely intended.
--
-- No org_id, deliberately. Identity is not tenant-scoped: one human belongs to
-- several organisations and is the same person in each, which is the whole
-- point of the §20.1 split between who data belongs to and who acted. RLS is
-- still enabled and forced, because the table holds personal data, and the
-- policy is self-only rather than org-scoped.
create table public.user_identities (
  -- The derived uuid. Not generated here: it is computed from the two columns
  -- below, so a row where it disagrees with them is corrupt by definition.
  user_id     uuid primary key,
  issuer      text not null,
  subject     text not null,
  email       text,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),

  -- The derivation is deterministic, so this is what makes provisioning
  -- idempotent on the sub claim rather than merely intended to be.
  constraint user_identities_issuer_subject_key unique (issuer, subject)
);

comment on table public.user_identities is
  'Reverse mapping for the one-way UUIDv5 subject derivation. Needed for incident response and DSAR.';

alter table public.user_identities enable row level security;
alter table public.user_identities force row level security;

-- Single-argument current_setting, matching every other policy in this schema:
-- an app connection that never set its GUCs fails loudly rather than quietly
-- reading nothing.
create policy user_identities_select_self on public.user_identities
  for select using (
    user_id = (select current_setting('app.current_user_id')::uuid)
  );

create policy user_identities_insert_self on public.user_identities
  for insert with check (
    user_id = (select current_setting('app.current_user_id')::uuid)
  );

create policy user_identities_update_self on public.user_identities
  for update
  using (user_id = (select current_setting('app.current_user_id')::uuid))
  with check (user_id = (select current_setting('app.current_user_id')::uuid));

-- organisations.personal_owner_id ---------------------------------------------
--
-- Marks the organisation created for one person on first arrival, and names
-- whose it is. Null for every organisation a human deliberately created.
--
-- The partial unique index is the acceptance criterion: a subject can own at
-- most one personal organisation, enforced by the database rather than by the
-- provisioning code being careful. That distinction matters because the
-- failure this guards against is a retry, and retries are exactly when
-- careful code is not running the second time.
alter table public.organisations
  add column personal_owner_id uuid;

comment on column public.organisations.personal_owner_id is
  'The subject this organisation was auto-provisioned for. Null when a human created it deliberately.';

create unique index organisations_one_personal_org_per_owner
  on public.organisations (personal_owner_id)
  where personal_owner_id is not null;

-- An organisation is visible to the person it was provisioned for, in addition
-- to being visible to its members.
--
-- This looks like a convenience and is a requirement, for a reason that is not
-- obvious until it bites. `organisations_select_member` in 00002 makes a row
-- visible through `memberships`, so during provisioning there is a moment when
-- the organisation exists and the membership does not, and in that moment its
-- creator cannot see it.
--
-- That matters because of how Postgres treats INSERT ... ON CONFLICT under
-- RLS: the proposed row must satisfy a SELECT policy, whether or not the
-- statement has a RETURNING clause and whether or not a conflicting row
-- actually exists. So the upsert that makes concurrent provisioning safe is
-- refused with "new row violates row-level security policy", which reads as a
-- permissions bug and is really a chicken-and-egg one. Measured against this
-- schema rather than reasoned about: a plain insert succeeds, and adding any
-- `on conflict` clause to the same statement fails.
--
-- Narrower than it looks: it grants sight of an organisation only to the one
-- subject it was created for, which is a fact about their own data. Policies
-- are OR-ed, so this widens nothing for anybody else.
create policy organisations_select_personal_owner on public.organisations
  for select using (
    personal_owner_id = (select current_setting('app.current_user_id')::uuid)
  );

-- invitations -----------------------------------------------------------------
--
-- A tenant table: org_id, indexed first, RLS enabled and forced, per the rule
-- in AGENTS.md.
create table public.invitations (
  id           uuid primary key default gen_random_uuid(),
  org_id       uuid not null references public.organisations(id) on delete cascade,
  email        text not null,
  role         text not null check (role in ('owner', 'member', 'viewer')),

  -- The token is stored hashed, never in the clear. An invitation token is a
  -- bearer credential: anyone holding it joins the organisation. A database
  -- dump, a backup or a support engineer reading a row must not yield a
  -- working one, which is the same reasoning that applies to a password.
  token_hash   text not null,

  invited_by   uuid,
  expires_at   timestamptz not null,
  accepted_at  timestamptz,
  accepted_by  uuid,
  created_at   timestamptz not null default now(),

  constraint invitations_token_hash_key unique (token_hash)
);

create index invitations_org_id_created_at_idx
  on public.invitations (org_id, created_at desc);

alter table public.invitations enable row level security;
alter table public.invitations force row level security;

-- Reading and managing invitations is org-scoped in the ordinary two-GUC
-- form. Note what this deliberately does NOT cover: the invitee, who is not a
-- member yet and therefore cannot see their own invitation through any of
-- these. That case is accept_invitation's, below.
create policy invitations_select_org on public.invitations
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy invitations_insert_owner on public.invitations
  for insert with check (public.app_org_role(org_id) = 'owner');

create policy invitations_update_owner on public.invitations
  for update
  using (public.app_org_role(org_id) = 'owner')
  with check (public.app_org_role(org_id) = 'owner');

create policy invitations_delete_owner on public.invitations
  for delete using (public.app_org_role(org_id) = 'owner');

-- accept_invitation ------------------------------------------------------------
--
-- The third SECURITY DEFINER function in this schema, and it needs the same
-- justification the other two carry, because a definer function is how RLS
-- gets bypassed by accident.
--
-- The invitee is not a member of the organisation yet. That is the entire
-- point of an invitation. So there is no org-scoped policy under which they
-- could see the row naming them, and a policy permissive enough to show it to
-- them would show every pending invitation to every authenticated stranger.
-- The same bootstrap shape as the memberships with_check in 00002, arriving
-- from a different direction.
--
-- What makes it safe is that the caller must present the token. The row is
-- found by its hash, never by org or by email, so holding the capability is
-- the authorization, and the acting user comes from the GUC rather than from
-- an argument the caller controls.
--
-- Everything happens in one statement's transaction: the membership insert
-- uses `on conflict do nothing` against the (org_id, user_id) primary key
-- shipped in 00002, so accepting twice, or two tabs accepting at once, joins
-- once.
-- +goose StatementBegin
create or replace function public.accept_invitation(p_token_hash text)
returns uuid
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $$
declare
  v_user uuid := public.app_current_user_id();
  v_inv  public.invitations%rowtype;
begin
  if v_user is null then
    raise exception 'not authenticated' using errcode = '28000';
  end if;

  -- for update, so two tabs accepting the same invitation serialise here
  -- rather than racing on the update below.
  select * into v_inv
  from public.invitations
  where token_hash = p_token_hash
    and accepted_at is null
    and expires_at > now()
  for update;

  if not found then
    -- Null rather than an exception: expired, already accepted and never
    -- existed are the same answer to the caller on purpose. Distinguishing
    -- them turns this into an oracle for which tokens are real.
    return null;
  end if;

  insert into public.memberships (org_id, user_id, role)
  values (v_inv.org_id, v_user, v_inv.role)
  on conflict (org_id, user_id) do nothing;

  update public.invitations
  set accepted_at = now(),
      accepted_by = v_user
  where id = v_inv.id;

  return v_inv.org_id;
end;
$$;
-- +goose StatementEnd

-- A definer function that PUBLIC may execute is a definer function anyone may
-- execute. Only the application role needs this one.
revoke all on function public.accept_invitation(text) from public;
grant execute on function public.accept_invitation(text) to kindlast_app;

-- Table grants. 00002 set default privileges for future tables created by the
-- migrator, so this is belt and braces rather than strictly required, and it
-- keeps the grant visible in the migration that creates the table.
grant select, insert, update, delete on public.user_identities to kindlast_app;
grant select, insert, update, delete on public.invitations to kindlast_app;

-- +goose Down

drop function if exists public.accept_invitation(text);
drop table if exists public.invitations;
drop index if exists public.organisations_one_personal_org_per_owner;
alter table public.organisations drop column if exists personal_owner_id;
drop table if exists public.user_identities;
