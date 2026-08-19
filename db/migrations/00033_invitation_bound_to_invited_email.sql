-- +goose Up
-- 00033_invitation_bound_to_invited_email.sql
--
-- An invitation could be redeemed by somebody it was not addressed to, and
-- redeeming it granted the role it named.
--
-- WHAT THE OLD FUNCTION DID
--
-- `accept_invitation(p_token_hash)` found the row by hash alone and joined
-- `app_current_user_id()` at `v_inv.role`. It never looked at `v_inv.email`.
-- So whoever was signed in when the link was followed became a member, at the
-- invited role, of an organisation they were never invited to.
--
-- Measured on a scratch stack rather than reasoned about: an invitation
-- addressed to one person, with role `owner`, redeemed by a signed-in user who
-- was neither that person nor a member, produced an `owner` membership for the
-- redeemer. Owner can read the whole compliance record, approve findings,
-- invite others, export the audit log, and choose a hosted model provider,
-- which is the act that sends a customer's data to a third party.
--
-- 00003 said why that was thought safe, and it is worth quoting rather than
-- quietly deleting: "What makes it safe is that the caller must present the
-- token. The row is found by its hash, never by org or by email, so holding
-- the capability is the authorization."
--
-- WHY THAT REASONING DOES NOT SURVIVE THE DELIVERY CHANNEL
--
-- The paragraph is answering a different question. It is defending the hash
-- lookup against the alternative of an org-scoped or email-scoped read, which
-- would leak pending invitations to strangers, and it is right about that.
-- The hash lookup stays. What it does not establish is that the capability may
-- be exercised by anyone who ends up holding it.
--
-- A bearer capability is a reasonable model when the holder is the intended
-- holder. This one is delivered by email to a named address, in a product
-- whose console tells the sender "Invitation on its way to <address>", and
-- email is forwarded, kept in shared mailboxes, quoted into tickets and read
-- on shared machines. `docs/personal-data-runbook.md` already treats
-- `invitations.email` as personal data about a specific person who is not yet
-- a user, which is the same premise: the row names who this is for.
--
-- There is a second, duller failure that needs no attacker at all. The person
-- who sent the invitation is usually signed in. If they follow the link to see
-- what the recipient will see, the old function consumed it: `accepted_at` was
-- set, and the actual recipient's click then returned null and sent them to an
-- error. An invitation destroyed by being looked at is a bug on its own terms.
--
-- WHY THE EMAIL COMES IN AS AN ARGUMENT
--
-- The obvious alternative is for this function to look the caller's address up
-- in `user_identities`, next to `app_current_user_id()`. It cannot, and the
-- reason is an ordering the product depends on: §1.8 has the invitation
-- redeemed in the auth callback BEFORE the first `GetCurrentUser`, so that
-- provisioning does not hand the invitee a personal organisation alongside the
-- one they were invited to. At that moment there is often no `user_identities`
-- row yet, so a lookup would refuse exactly the flow this exists to serve.
--
-- The verified `email` claim is on the token core-api has already checked. It
-- arrives here with the same trust as `app.current_user_id`, which is also set
-- by core-api from that same verified token, so this adds no new trust in the
-- caller. Anything else reaching this function would need EXECUTE, which only
-- `kindlast_app` holds.
--
-- The comparison is `lower()` on both sides. The local part of an address is
-- case-sensitive by RFC, and no mail provider anybody uses honours that;
-- matching case-sensitively would refuse a person who typed `Carol@` where the
-- inviter typed `carol@`, and that is a support ticket rather than a defence.
--
-- WHY A MISMATCH IS INDISTINGUISHABLE, AND LEAVES THE ROW ALONE
--
-- The email is a predicate in the same `where` as the hash rather than a
-- check after the select. Two things follow, and both are deliberate:
--
--   1. A mismatch selects no row, so `accepted_at` is never set. The person it
--      was addressed to can still use it. This is what makes the inviter's
--      curious click harmless.
--   2. A mismatch is reported exactly like expired, already accepted and never
--      existed: null. 00003's oracle argument applies unchanged, and adding a
--      distinguishable "wrong person" answer would tell a holder whether a
--      token is real and who it was for.
--
-- The old single-argument function is dropped rather than left beside this
-- one. Leaving it would leave the unchecked path callable, and a later caller
-- would have no way to know which of the two was the safe one.
-- +goose StatementBegin
create or replace function public.accept_invitation(p_token_hash text, p_email text)
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

  -- No address, no acceptance. A token whose caller cannot be matched to the
  -- invited person is not usable, and treating an empty claim as a wildcard
  -- would restore the whole bug for any provider that omits the claim.
  if p_email is null or btrim(p_email) = '' then
    return null;
  end if;

  -- for update, so two tabs accepting the same invitation serialise here
  -- rather than racing on the update below.
  select * into v_inv
  from public.invitations
  where token_hash = p_token_hash
    and accepted_at is null
    and expires_at > now()
    and lower(email) = lower(btrim(p_email))
  for update;

  if not found then
    -- Null rather than an exception: expired, already accepted, never existed
    -- and addressed to somebody else are the same answer to the caller on
    -- purpose. Distinguishing them turns this into an oracle for which tokens
    -- are real and who they name.
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
revoke all on function public.accept_invitation(text, text) from public;
grant execute on function public.accept_invitation(text, text) to kindlast_app;

-- The unchecked version goes, so it cannot be called by anything that has not
-- been updated to pass an address.
drop function if exists public.accept_invitation(text);

-- +goose Down

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

  select * into v_inv
  from public.invitations
  where token_hash = p_token_hash
    and accepted_at is null
    and expires_at > now()
  for update;

  if not found then
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

revoke all on function public.accept_invitation(text) from public;
grant execute on function public.accept_invitation(text) to kindlast_app;

drop function if exists public.accept_invitation(text, text);
