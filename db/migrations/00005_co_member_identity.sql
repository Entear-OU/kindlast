-- +goose Up
-- 00005_co_member_identity.sql (ENT-202)
--
-- Two changes, and the second is a deliberate reversal of a decision 00003
-- made on purpose. Reversals should be legible where they happen rather than
-- only in a pull request nobody will find in a year, so the reasoning is here.
--
-- 1. user_identities gains display_name.
-- 2. A member may read the identity of anyone they share an organisation with.

-- 1. display_name --------------------------------------------------------------
--
-- /me already receives a name from the userinfo endpoint and currently throws
-- it away, so the product knows this and forgets it on every sign-in. The
-- members list needs it, and it is the least sensitive identifier available:
-- given the choice between showing a colleague a name or an email address, a
-- name is the smaller disclosure and the more useful label.
--
-- Nullable, because an authorization server is not obliged to return one. A
-- caller with no name is not an error, it is a person whose IdP told us
-- nothing, and the members list falls back to the email.
--
-- Worth knowing for later: once this is populated, §20.1's personal-org naming
-- chain can prefer the stored display_name over the email local part, which
-- quietly improves provisioning for everyone whose access token carries no
-- name claim.
alter table public.user_identities add column display_name text;

-- 2. Co-member visibility ------------------------------------------------------
--
-- WHAT 00003 DECIDED, AND WHY THIS CHANGES IT
--
-- 00003 made user_identities self-only, and its comment is worth re-reading
-- rather than paraphrasing: identity is not tenant-scoped, one human belongs to
-- several organisations and is the same person in each, and the table holds
-- personal data. All of that remains true. None of it is being repudiated.
--
-- What was not foreseen is the cost. Under a self-only policy, listing an
-- organisation's members returns uuids and roles, so a settings page can only
-- offer to remove `3f9a1c72-...`. There is no way to build the members surface
-- ENT-202 asks for without deciding this, and it was better decided explicitly
-- than discovered as a workaround.
--
-- The user ruled on 2026-08-15: members see each other's display name and
-- email, uniformly across owner, member and viewer. A variant masking email
-- from members and viewers was offered and declined, so uniformity is a
-- conscious choice rather than an unexamined default. That is why the test
-- suite asserts a viewer sees what an owner sees: a later tightening should
-- have to break a test rather than pass quietly.
--
-- WHY THIS IS DEFENSIBLE IN A PRODUCT THAT SELLS GDPR COMPLIANCE
--
-- Within an organisation, members' work identities are already functionally
-- known to that organisation: it either invited them or they joined it.
-- Visibility is scoped to exactly the people who share that context, and it is
-- the ordinary expectation in every workspace product a customer already uses.
-- What 00003 was actually protecting against is cross-tenant identity leakage,
-- and that protection survives entirely: this grants nothing outside a shared
-- membership.
--
-- WHY A POLICY AND NOT A SECURITY DEFINER FUNCTION
--
-- The definer functions in this schema exist because RLS structurally cannot
-- express their check: app_org_role subqueries memberships from a policy on
-- memberships, which recurses. Co-member visibility is the opposite case. RLS
-- expresses it directly, as below, so routing it through a definer function
-- would move a policy decision out of the policy surface and into procedural
-- code. That is worse auditability wearing a security costume.
--
-- THE SHAPE
--
-- Additive, alongside user_identities_select_self rather than replacing it.
-- Permissive policies are OR'd, so self-visibility stays independently stated
-- and this reads as what it is: an addition, reviewable on its own.
--
-- It is the two-GUC predicate every tenant table carries, adapted for a table
-- that deliberately has no org_id. Where those policies ask "is this row's
-- organisation the active one", this asks "is this row's person a member of
-- the active one". The caller's own membership clause is identical to theirs.
create policy user_identities_select_co_member on public.user_identities
  for select using (
    exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
    and exists (
      select 1 from public.memberships theirs
      where theirs.org_id = (select current_setting('app.current_org_id')::uuid)
        and theirs.user_id = user_identities.user_id
    )
  );

-- Nothing here touches insert or update. Visibility was widened; authority was
-- not. Conflating the two is the easy mistake when relaxing a policy, and `for
-- all` instead of `for select` would let any co-member rewrite another
-- person's email address. db/tests/co-member-identity.test.ts asserts that it
-- cannot.

-- +goose Down
drop policy if exists user_identities_select_co_member on public.user_identities;
alter table public.user_identities drop column if exists display_name;
