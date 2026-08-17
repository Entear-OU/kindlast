-- +goose Up
-- 00014_transactional_outbox.sql (ENT-219)
--
-- A place to put a message that must survive the transaction that caused it.
--
-- THE BUG THIS CLOSES, WHICH IS WORSE THAN "THE EMAIL IS NOT WRITTEN YET"
--
-- `InviteMember` mints a 32-byte token, hashes it, stores the hash, and returns
-- the invitation id. The raw token exists for the life of one handler and is
-- then gone forever, because only its hash is persisted (00003:130-134, and
-- that is correct: an invitation token is a bearer credential and a database
-- dump must not yield a working one).
--
-- So an invitation created before a delivery path exists is not "an email that
-- arrives later". It is **permanently undeliverable**. Nothing can reconstruct
-- the link, reissuing is the only cure, and nobody can tell which rows need it,
-- because a minted-but-never-sent invitation is indistinguishable from one the
-- recipient has simply not clicked yet. Doc §20.1 states the rule this obeys:
-- the secret's only egress is at mint.
--
-- WHY A SECOND TABLE AND NOT `notification_outbox`
--
-- They are two shapes, and the difference is not cosmetic.
--
-- `notification_outbox` resolves its recipient at dispatch time, from
-- memberships and notification preferences. That is right for a finding
-- doorbell: an organisation has members, and who should be told about a finding
-- is a question best answered when the telling happens, against preferences as
-- they are then. Its schema says so: `finding_id` is `not null unique` with an
-- FK to `findings` (00001:431-443, 00001:928-929), so a row that is not about a
-- finding cannot exist in it at all.
--
-- A transactional message is the opposite on every axis. Its recipient is an
-- email address that may belong to no user of this system and never will until
-- they accept. Its payload contains a secret that exists only at mint and
-- cannot be regenerated later. There is nothing to resolve at dispatch time and
-- nothing that may be re-decided: the message is fixed the moment the fact it
-- announces becomes true.
--
-- Forcing both into one table means a `kind` discriminator, `finding_id`
-- nullable, `user_id` nullable, `recipient_email` nullable, and a set of
-- check constraints describing which combinations are legal. That table would
-- have been designed inside whichever pull request reached it first. So:
-- `notification_outbox` stays exactly what it is, ENT-209's doorbell path, and
-- this is the transactional one.
--
-- WHY THE INSERT POLICY IS OWNER-ONLY, WHICH IS NARROWER THAN IT LOOKS
--
-- A table holding an arbitrary recipient address and an arbitrary body, that a
-- background process then delivers, is a mail relay. If any member of any
-- organisation could write a row, any member could send mail from this
-- deployment's domain to anyone, and the compensating control would be "the
-- application only inserts through one function", which is a code path rather
-- than a boundary. AGENTS.md is unambiguous that authority is RLS and database
-- constraints, never the discipline of the caller.
--
-- Today exactly one thing enqueues a transactional message: minting an
-- invitation, which 00003:167 already restricts to an owner. So the insert
-- policy is the same `app_org_role(org_id) = 'owner'` test, and the two run in
-- the same transaction against the same rule. When a second kind arrives that a
-- non-owner may cause, this policy gets widened deliberately, in the change that
-- introduces it, rather than having been permissive from the beginning for a
-- caller that did not exist yet.
--
-- `kind` is check-constrained to the kinds that exist for the same reason. A
-- free-text kind column is a place for a typo to become a message nobody
-- delivers.
--
-- WHY THE DISPATCHER READS EVERY ORGANISATION, AND WHY THAT IS NOT A HOLE
--
-- Every other policy in this schema is an org equality plus a membership
-- `exists`, and the agent role's are an org equality alone (00008). This table
-- needs a third shape, because draining an outbox is inherently cross-tenant:
-- a delivery loop that could only see one organisation's rows would have to be
-- told which organisations exist, and the query that answers that is itself the
-- cross-tenant read it was trying to avoid.
--
-- So the agent's policy here is unconditional. What keeps it honest is the
-- grant beside it: `select, update` and **no insert**. The role that delivers
-- messages cannot create one. It can read a row that an owner already caused
-- and mark what happened to it, and that is the whole of its reach into this
-- table. Compare the same role on `notification_outbox`, which is granted
-- `select, insert` and no update: there the agent enqueues and something else
-- delivers, here the application enqueues and the agent delivers. Neither role
-- can do both halves of either path.
--
-- The narrowness is the control. It is written as a grant rather than a policy
-- because a missing grant fails closed at parse time, where a policy that is
-- subtly wrong fails open at run time.

create table public.transactional_outbox (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- Constrained rather than free text: see the header. Widened by the change
  -- that introduces a new kind, never in advance.
  kind text not null check (kind in ('invitation')),

  -- The recipient is an address, not a user. An invitee has no row in
  -- `memberships` and may have no account anywhere until they accept, which is
  -- exactly why this cannot be resolved at dispatch time.
  recipient_email text not null,

  subject   text not null,
  body_text text not null,
  -- Optional. A text-only message is deliverable and readable; an HTML-only one
  -- is neither in a client that will not render it.
  body_html text,

  status   text not null default 'pending'
    check (status in ('pending', 'sent', 'failed')),
  attempts integer not null default 0,
  last_error text,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  sent_at    timestamptz,

  -- "Delivered" is one fact, not two that can disagree.
  --
  -- Without this, `status = 'sent'` with a null `sent_at` and `status =
  -- 'pending'` with a `sent_at` are both storable, and each is a different way
  -- for a retry to deliver a message twice: the first because a claim query
  -- filtering on `sent_at is null` would pick the row back up, the second
  -- because one filtering on status would. The acceptance criterion is that a
  -- delivered row is not re-sent, and this is the part of it a code review
  -- cannot forget to check.
  constraint transactional_outbox_sent_at_matches_status
    check ((status = 'sent') = (sent_at is not null))
);

-- The dispatcher's only query, and it only ever wants pending rows. A partial
-- index keeps it the size of the backlog rather than the size of everything
-- ever sent, which on this table is the difference between a few rows and every
-- invitation the deployment has issued.
create index transactional_outbox_pending_idx
  on public.transactional_outbox (created_at)
  where status = 'pending';

-- Org-scoped reads, ordered the way a console would show them.
create index transactional_outbox_org_created_idx
  on public.transactional_outbox (org_id, created_at desc);

create trigger set_updated_at
  before update on public.transactional_outbox
  for each row execute function public.set_updated_at();

alter table public.transactional_outbox enable row level security;
alter table public.transactional_outbox force row level security;

------------------------------------------------------------------------------
-- kindlast_app: enqueue and read back, within one organisation
------------------------------------------------------------------------------

grant select, insert on public.transactional_outbox to kindlast_app;

-- The ordinary two-GUC form. A middleware bug that names an organisation the
-- caller does not belong to still reads zero rows.
create policy transactional_outbox_select_org on public.transactional_outbox
  for select using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- Owner-only, matching invitations_insert_owner. See the header.
create policy transactional_outbox_insert_owner on public.transactional_outbox
  for insert with check (public.app_org_role(org_id) = 'owner');

-- Deliberately absent: no update and no delete policy for kindlast_app. The
-- application enqueues and reads; it does not get to mark a message delivered,
-- because it is not the thing that delivers it. Recording an outcome is the
-- dispatcher's, and a request handler that could write `sent` could assert a
-- delivery that never happened.

------------------------------------------------------------------------------
-- kindlast_agent: deliver, across every organisation
------------------------------------------------------------------------------

-- No insert. See the header: the role that delivers cannot create a message.
grant select, update on public.transactional_outbox to kindlast_agent;

create policy transactional_outbox_agent on public.transactional_outbox
  to kindlast_agent
  using (true)
  with check (true);

-- +goose Down

drop policy if exists transactional_outbox_agent        on public.transactional_outbox;
drop policy if exists transactional_outbox_insert_owner on public.transactional_outbox;
drop policy if exists transactional_outbox_select_org   on public.transactional_outbox;

revoke all on public.transactional_outbox from kindlast_agent;
revoke all on public.transactional_outbox from kindlast_app;

drop table if exists public.transactional_outbox;
