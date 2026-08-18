-- +goose Up
-- 00030_outbox_retention.sql (ENT-242)
--
-- The outbox holds raw bearer tokens in plaintext, and nothing ever emptied it.
--
-- WHAT IS ACTUALLY IN THIS TABLE
--
-- `notify.InvitationLink` builds the URL an invitee follows to accept, with the
-- raw token occupying one path segment, and `notify.Invitation` renders that
-- link into the message. `notify_test.go` asserts exactly that, because a body
-- without the link is an invitation that cannot be accepted. So
-- `transactional_outbox.body_text` is, by construction, a store of raw
-- invitation tokens in the clear, next to the address of the person each was
-- issued to.
--
-- Two tables away, 00003 stores only the token's hash, and 00014's own header
-- explains why: "an invitation token is a bearer credential and a database dump
-- must not yield a working one". The outbox is the single place that property is
-- suspended, because the raw token has to exist somewhere for the message to be
-- sendable at all. That was a bounded exception when the row's lifetime was
-- "until the dispatcher drains it". It was never bounded, because nothing
-- drained it: a row moves to `sent` and stays, address, subject and both bodies
-- intact, for as long as the deployment lives.
--
-- So the design's own rule does not hold today, and this is what restores it.
-- It is a credential-lifetime fix before it is a data-minimisation one, though
-- it is both.
--
-- THE SHAPE: REDACTION, NOT DELETION, AND NOTHING IS EVER DELETED HERE
--
-- The row is two separable things. It is a delivery fact ("this organisation
-- sent an invitation at this time, in two attempts, and it went out"), and it
-- is a rendered message with a recipient's address in it. Only the second is
-- personal data, and only the second holds a credential.
--
-- Deleting the row drops the data by throwing away the fact. Keeping the row
-- holds the fact by keeping the data. Redaction is the only option that does
-- not force that trade, so the reclaim clears `recipient_email`, `subject`,
-- `body_text`, `body_html` and `last_error`, stamps `redacted_at`, and leaves
-- everything else exactly where it is.
--
-- **Nothing in this migration deletes a row, and no role gains a delete grant
-- on this table, not even through the definer function below.** The only thing
-- that removes a row from `transactional_outbox` remains the cascade from
-- `organisations`, which is how erasing an organisation already works. That is
-- a stronger position than the one ENT-242 sketched, and it is available only
-- because redaction turns out to be sufficient.
--
-- WHY THIS DIFFERS FROM `audit_log`, WHICH DELIBERATELY HAS NO RETENTION
--
-- `db/README.md` argues at length that nothing deletes from `audit_log`, and a
-- reader who knows that argument should be told why this table is treated
-- differently rather than left to assume somebody forgot.
--
-- The audit log is kept because a regulator may be shown it, and a record that
-- thins out after a fixed window is one whose answer to "what happened in 2024"
-- depends on when somebody asks. Its content is the evidence.
--
-- The outbox is the envelope, not the letter. What a customer or a regulator
-- would be shown about an invitation is in `invitations`: who was invited, at
-- what address, by whom, when, and whether they accepted. That row has no
-- retention either, and it survives all of this untouched. What the outbox adds
-- on top is the rendered text and the delivery attempt, and the rendered text is
-- a dead credential and a person's address a few days after it is sent.
--
-- So the two tables are treated differently for a stated reason. Keep the
-- evidence forever. Redact the envelope once it has been opened, and keep the
-- postmark.
--
-- WHAT HAPPENS TO EACH STATE, WHICH IS THE PART TO GET RIGHT
--
--   pending, and its invitation can still be accepted
--       Nothing. Ever. No window reaches it, because the rule that protects it
--       is not a window: the raw token in that body exists nowhere else in the
--       system, so blanking it destroys the only copy of a credential the
--       recipient is waiting for, and reissue is the only cure. This is the
--       state whose loss is silent and unrecoverable, and it is why the
--       predicate that protects it takes no argument from the caller.
--
--   pending or failed, and its invitation can no longer be accepted
--       Abandoned: `status` becomes `failed`, `last_error` records why, and the
--       body is redacted. A message whose secret has expired or has already
--       been spent can never be usefully sent, so what remains is a dead
--       credential and an address belonging to somebody who may never have had
--       an account here at all.
--
--       Moving it to `failed` is not bookkeeping. The dispatcher claims
--       `status = 'pending'` and, having no maximum attempt count, retries a
--       permanently undeliverable message every ten seconds forever. Abandoning
--       it is the first thing in this codebase to write `failed`, which 00014
--       reserved for exactly this: giving up.
--
--   sent
--       Redacted, at the earlier of two moments. The body window elapsing since
--       `sent_at`, or its invitation ceasing to be acceptable. The second
--       matters more than the first: a link that has been used, or that has
--       expired, has no remaining value to anybody, so waiting out a window
--       before dropping it would be keeping a person's address for no reason.
--       The delivery fact stays for the life of the organisation.
--
--   failed and already redacted
--       Nothing. The predicates all test `redacted_at is null`, so the job is
--       idempotent, which is what makes it safe to run every hour and safe to
--       run in more than one replica at once.
--
-- WHY A DEFINER FUNCTION, WHEN THE AGENT ALREADY HOLDS UPDATE
--
-- This is the part worth arguing with, because the update grant already exists
-- and the reclaim is only an update.
--
-- What the agent lacks is the question, not the write. Deciding whether an
-- undelivered message can still be usefully sent means asking whether the
-- invitation it carries can still be accepted, and that is a read of
-- `invitations`. The agent has no grant there, deliberately: 00008 gives it no
-- organisations, no memberships and no audit log, on the principle that a role
-- which can fabricate a finding should not also be able to enumerate people.
-- `invitations.email` is the address of somebody who may never have accepted,
-- so a select grant would hand a compromised agent every invited address in
-- every tenant of the deployment.
--
-- That is the identical argument 00015 made for `notification_recipients`, and
-- this function is the same narrow shape: it takes the window, it answers about
-- rows it is already looking at, and the caller cannot use it to ask anything
-- about `invitations` directly. RLS structurally cannot express this, because a
-- policy subquery is evaluated with the querying role's privileges and would
-- need that same grant.
--
-- The blast radius is small enough to state exactly. A caller who passes a
-- window of zero redacts every delivered body immediately, losing diagnostics
-- and nothing else. There is no argument value, none, that touches a message
-- which can still be delivered, because the predicate that protects one
-- contains no parameter.
--
-- WHERE THE PERIOD LIVES
--
-- In Go, as `dispatch.DeliveredBodyRetention`, passed in as an argument. A
-- retention period consults nothing and could reasonably be different next
-- quarter, which is §14.5's test for a decision. What is in here is the
-- invariant: which rows may be touched at all, and the constraint below that
-- makes a recorded redaction honest no matter who writes it.

alter table public.transactional_outbox
  -- Stamped when the body was cleared. Not derivable from the columns being
  -- empty: an empty subject is a legal message, and "we removed this" and "there
  -- was never anything here" have to be tellable apart by an operator answering
  -- an access request.
  add column redacted_at timestamptz;

alter table public.transactional_outbox
  -- A redaction that left the body behind is a lie, and this is the shape of
  -- lie nobody notices: the row says the personal data is gone and the column
  -- still holds it.
  add constraint transactional_outbox_redaction_is_complete
    check (
      redacted_at is null
      or (recipient_email = ''
          and subject = ''
          and body_text = ''
          and body_html is null)
    ),

  -- A message that has not been given up on cannot be recorded as redacted.
  --
  -- A constraint rather than a predicate inside the function, because it must
  -- hold no matter who writes. `kindlast_agent` holds an unconditional update
  -- policy on this table, so without this any code path in the dispatcher, or
  -- any future one, could blank a pending message and stamp it, destroying a
  -- token that exists nowhere else. The database refuses instead.
  add constraint transactional_outbox_redacted_only_when_finished
    check (redacted_at is null or status in ('sent', 'failed'));

-- The two backlogs the reclaim walks, each sized to the work outstanding rather
-- than to the table.
--
-- Both are partial on `redacted_at is null`, so they shrink as the job runs and
-- hold only what it has still to do. That is the same reasoning
-- `transactional_outbox_pending_idx` follows, and that index stays exactly as it
-- is: it serves the dispatcher's claim query, which tests `status = 'pending'`
-- and cannot use either of these.
create index transactional_outbox_delivered_unredacted_idx
  on public.transactional_outbox (sent_at)
  where status = 'sent' and redacted_at is null;

create index transactional_outbox_undelivered_unredacted_idx
  on public.transactional_outbox (created_at)
  where status <> 'sent' and redacted_at is null;

------------------------------------------------------------------------------
-- The grant 00014's comment already claimed
------------------------------------------------------------------------------
--
-- 00014 says: "Deliberately absent: no update and no delete policy for
-- kindlast_app. The application enqueues and reads; it does not get to mark a
-- message delivered." That was true about policies and false about grants.
-- 00002 set default privileges granting the application select, insert, update
-- and delete on every table the migrator creates afterwards, so this one
-- arrived with all four attached, and only the absent policies were holding.
--
-- They do hold: with FORCE ROW LEVEL SECURITY and no policy, an update or a
-- delete from `kindlast_app` touches zero rows. But it touches them silently,
-- which reads exactly like a boundary and is not one. 00015 hit this with
-- `capability_tokens` and revoked rather than relying on it, and wrote down
-- why: "a missing grant fails closed at parse time, where a missing policy
-- fails quietly at run time, and for a table of bearer credentials the loud
-- version is the one worth having."
--
-- `transactional_outbox` is a table of bearer credentials by the argument at
-- the top of this file. It gets the same treatment. Narrowing, not widening:
-- nothing gains anything here.
revoke update, delete on public.transactional_outbox from kindlast_app;

------------------------------------------------------------------------------
-- reclaim_transactional_outbox
------------------------------------------------------------------------------

-- +goose StatementBegin
create or replace function public.reclaim_transactional_outbox(
  p_delivered_body_retention interval,
  p_batch integer
)
returns table (redacted bigint, abandoned bigint)
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_redacted  bigint := 0;
  v_abandoned bigint := 0;
begin
  -- Delivered messages whose body has no remaining value.
  --
  -- Either the window has passed since delivery, or the invitation the message
  -- carries can no longer be accepted, whichever comes first. The second
  -- disjunct is what makes the guarantee absolute rather than approximate: a
  -- token that has expired or been spent is gone from this table on the next
  -- pass, whatever the window says, because there is no case in which keeping
  -- it is worth a person's address.
  --
  -- `kind = 'invitation'` guards that disjunct so a second kind, when one
  -- arrives, falls through to the window alone rather than being redacted
  -- immediately because it has no invitation to look up. Failing closed, in the
  -- same spirit as 00014's check-constrained `kind`.
  with due as (
    select o.id
      from public.transactional_outbox o
     where o.status = 'sent'
       and o.redacted_at is null
       and (
         o.sent_at < now() - p_delivered_body_retention
         or (o.kind = 'invitation' and not public.invitation_is_live(o.org_id, o.recipient_email))
       )
     order by o.sent_at
     limit p_batch
     -- Skip locked so two replicas running this at the same time do not block
     -- each other, and neither waits on a row the other has already taken.
     for update skip locked
  )
  update public.transactional_outbox t
     set recipient_email = '',
         subject         = '',
         body_text       = '',
         body_html       = null,
         -- Cleared with the rest. A delivery that succeeded has none, but a
         -- delivery that succeeded on the third attempt kept the text of the
         -- first two failures, and an SMTP rejection quotes the address it
         -- rejected: the error is the address in another shape.
         last_error      = null,
         redacted_at     = now()
    from due
   where t.id = due.id;
  get diagnostics v_redacted = row_count;

  -- Undelivered messages that can never usefully be sent.
  --
  -- The `not invitation_is_live` test takes no argument from the caller, which
  -- is the whole safety property of this function: a message whose invitation
  -- can still be accepted is unreachable from here at any window, including
  -- zero.
  with dead as (
    select o.id
      from public.transactional_outbox o
     where o.status <> 'sent'
       and o.redacted_at is null
       and o.kind = 'invitation'
       and not public.invitation_is_live(o.org_id, o.recipient_email)
     order by o.created_at
     limit p_batch
     for update skip locked
  )
  update public.transactional_outbox t
     set status          = 'failed',
         recipient_email = '',
         subject         = '',
         body_text       = '',
         body_html       = null,
         -- Replaces whatever the mail server last said, for the reason above,
         -- and says the thing an operator reading this row actually needs to
         -- know: this was not a transient failure, it was given up on.
         last_error      = 'abandoned undelivered: the invitation this message carries can no longer be accepted',
         redacted_at     = now()
    from dead
   where t.id = dead.id;
  get diagnostics v_abandoned = row_count;

  return query select v_redacted, v_abandoned;
end;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Can the invitation this message carries still be accepted?
--
-- Split out of the reclaim so the question is written once and reads as what it
-- is. It is `stable` and takes no side effects, and it is not `security
-- definer` itself: it is only ever called from inside one that is, and marking
-- it definer would make it a standalone way to probe whether a given address
-- has a live invitation in a given organisation.
--
-- The match is on the address rather than on a foreign key because there is no
-- foreign key: the outbox row is written in the same transaction as the
-- invitation but carries no reference to it (00014), and adding one now would
-- be a wider change than this issue should make. Case-insensitive, because a
-- mail address is, and the two columns are written from the same input anyway.
--
-- An invitation that has been deleted answers false, which is correct: revoking
-- an invitation should stop the email that carries it, not leave a link in a
-- queue that will still work when it arrives.
create or replace function public.invitation_is_live(p_org_id uuid, p_email text)
returns boolean
language sql
stable
set search_path to 'public', 'pg_temp'
as $function$
  select exists (
    select 1
      from public.invitations i
     where i.org_id = p_org_id
       and lower(i.email) = lower(p_email)
       and i.accepted_at is null
       and i.expires_at > now()
  );
$function$;
-- +goose StatementEnd

revoke all on function public.invitation_is_live(uuid, text) from public;
revoke all on function public.reclaim_transactional_outbox(interval, integer) from public;

-- The dispatcher, and nothing else. `kindlast_app` is deliberately not granted
-- execute: it serves requests, and a request handler that could reclaim could
-- blank a queued invitation somebody is waiting for.
grant execute on function public.reclaim_transactional_outbox(interval, integer) to kindlast_agent;

-- +goose Down

revoke all on function public.reclaim_transactional_outbox(interval, integer) from kindlast_agent;
drop function if exists public.reclaim_transactional_outbox(interval, integer);
drop function if exists public.invitation_is_live(uuid, text);

-- Restores what 00002's default privileges had attached. Not an endorsement:
-- the up direction argues that this grant should never have been here, and a
-- down migration puts the schema back as it was rather than as it should be.
grant update, delete on public.transactional_outbox to kindlast_app;

drop index if exists public.transactional_outbox_undelivered_unredacted_idx;
drop index if exists public.transactional_outbox_delivered_unredacted_idx;

alter table public.transactional_outbox
  drop constraint if exists transactional_outbox_redacted_only_when_finished,
  drop constraint if exists transactional_outbox_redaction_is_complete,
  drop column if exists redacted_at;
