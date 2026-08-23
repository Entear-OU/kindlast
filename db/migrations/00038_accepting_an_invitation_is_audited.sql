-- +goose Up
-- 00038_accepting_an_invitation_is_audited.sql (ENT-268)
--
-- The audit log said who was invited and by whom. It did not say whether they
-- ever arrived.
--
-- WHAT THE GAP LOOKED LIKE
--
-- ENT-255 put renaming, inviting, role changes and removals into the log. Four
-- of the five. "Invited somebody to join" is the same row whether the
-- invitation is still sitting unread in a mailbox, whether it expired, or
-- whether the person has been reading the compliance record every day since,
-- and telling those apart is the reason access is recorded at all. An auditor
-- asking "who can reach this record, and when did they get here" could get the
-- offer and not the arrival.
--
-- WHY THIS ROW COULD NOT BE WRITTEN FROM GO WITH THE OTHER FOUR
--
-- It was left out of ENT-255 deliberately, and the reason is structural rather
-- than a matter of time. `audit_log_insert_org` requires three things of every
-- insert: the row's `org_id` equals `app.current_org_id`, the row's `user_id`
-- equals `app.current_user_id`, and a membership exists for that pair.
--
-- A person redeeming an invitation satisfies none of the first two and, at the
-- moment they act, not the third either. §1.8 has the invitation redeemed in
-- the auth callback before the first `GetCurrentUser`, so the caller arrives
-- with no organisation header and `BeginTenant` resolves them to the
-- no-organisation sentinel. There is no active organisation to bind the row to
-- until this function creates the membership, and by then the transaction's
-- GUC still says otherwise. A `record_audit_log` call placed beside
-- `AcceptInvitation` in Go is refused with a 42501 and takes the acceptance
-- down with it.
--
-- The ENT-255 self-removal lesson is the same problem read backwards. There,
-- writing the audit row after deleting the membership refused, because the
-- membership the policy checks for is exactly the one that had just stopped
-- existing, and the fix was to write the row first. Here the membership does
-- not exist yet and the fix is the mirror image: create it, then write the row.
--
-- WHY INSIDE THE FUNCTION IS THE RIGHT PLACE RATHER THAN A CONVENIENT ONE
--
-- `accept_invitation` is already SECURITY DEFINER, and 00003 and 00033 both
-- say why: no org-scoped policy can show an invitation to somebody who is not
-- yet a member, and a policy permissive enough to try would show every pending
-- invitation to every authenticated stranger. That decision is made and this
-- change does not extend it. What it does is use the one place that already
-- knows all three facts the row needs, at the one moment all three are true:
-- the organisation (from the invitation), the actor (`app_current_user_id()`,
-- the same GUC everything else trusts, not an argument), and that the
-- membership now exists, because the statement above just created it.
--
-- The alternative, loosening `audit_log_insert_org` so a non-member can insert
-- into an organisation's log, is not a trade worth considering. That policy is
-- what stops one tenant writing into another's regulatory record.
--
-- ORDERING, AND WHAT IT BUYS
--
-- The `record_audit_log` call goes after the membership insert, not before.
-- `record_audit_log` snapshots the actor's role by reading `memberships`, so
-- called first it would record a null role and the log would say somebody
-- joined without saying what they became. The Go test asserts the snapshot is
-- `member` rather than merely that a row exists, which is what pins this
-- ordering rather than leaving it to be re-derived by whoever edits next.
--
-- It goes before the `update invitations`, which is arbitrary and safe: both
-- are in the same transaction, so a reader sees all of it or none of it.
--
-- WHAT THE ROW SAYS, AND WHAT IT DELIBERATELY DOES NOT
--
--   action_type   accept_invitation
--   target        the invitation row, so the log joins the offer to the arrival
--   before        null. The absence is the content: this is an arrival, not a
--                 change to an access that already existed.
--   after         the role granted, and only that.
--
-- No token and no hash. The token is a capability, the audit log is readable by
-- every member and exportable to CSV, and 00033's whole subject is that holding
-- the token is most of what it takes to walk in.
--
-- No email address either, and that is a change from `invite_member`, which
-- records one. There it is the only place the invited person is named. Here the
-- `user_id` column already names the joiner, `target_id` already points at the
-- invitation that carries the address, and adding it would put the same piece
-- of personal data in a second row for no question it helps answer.
--
-- REFUSALS WRITE NOTHING
--
-- Every not-usable path returns before the insert, so expired, already
-- accepted, never existed and addressed to somebody else stay one answer in the
-- log as well as one answer to the caller. Silence is the only answer that
-- preserves that: a row on refusal would record an access grant that did not
-- happen, and would let anyone holding a guessed token write into an
-- organisation's regulatory record.
--
-- The body below is 00033's, unchanged except for the `perform`. It is restated
-- in full because that is what `create or replace function` requires, not
-- because anything else moved.
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
    --
    -- Nothing is recorded on this path, for the same reason. See the header.
    return null;
  end if;

  insert into public.memberships (org_id, user_id, role)
  values (v_inv.org_id, v_user, v_inv.role)
  on conflict (org_id, user_id) do nothing;

  -- Somebody arrived, and this is the row that says so.
  --
  -- After the membership insert, because `record_audit_log` snapshots the
  -- actor's role out of `memberships`: called before it, this records a null
  -- role and the log says a person joined without saying at what authority.
  --
  -- The actor is also the approving user. There is no second party to an
  -- acceptance: the authority came from the invitation, which has its own
  -- audit row naming whoever issued it.
  perform public.record_audit_log(
    v_inv.org_id, v_user, null, 'accept_invitation', 'invitations', v_inv.id,
    null, jsonb_build_object('role', v_inv.role), v_user
  );

  update public.invitations
  set accepted_at = now(),
      accepted_by = v_user
  where id = v_inv.id;

  return v_inv.org_id;
end;
$$;
-- +goose StatementEnd

-- Restated rather than relied upon. `create or replace function` keeps the
-- existing ACL, so these are already in place, and spelling them out means a
-- reader of this file does not have to go back to 00033 to learn that a definer
-- function which PUBLIC may execute is a definer function anyone may execute.
revoke all on function public.accept_invitation(text, text) from public;
grant execute on function public.accept_invitation(text, text) to kindlast_app;

-- +goose Down

-- 00033's function, without the audit row.
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

  if p_email is null or btrim(p_email) = '' then
    return null;
  end if;

  select * into v_inv
  from public.invitations
  where token_hash = p_token_hash
    and accepted_at is null
    and expires_at > now()
    and lower(email) = lower(btrim(p_email))
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

revoke all on function public.accept_invitation(text, text) from public;
grant execute on function public.accept_invitation(text, text) to kindlast_app;
