-- +goose Up
-- 00044_telegram_channel.sql (ENT-263)
--
-- A second channel on the one dispatch path.
--
-- THE THING THIS MIGRATION IS TRYING NOT TO BE
--
-- The failure mode for a second channel is a second everything: its own queue,
-- its own retry policy, its own idea of what "sent" means, and eventually two
-- answers to "did this person get told". §17 and 00014 already decided where
-- the queue is. So nothing here creates a table that holds a message. Telegram
-- rides `transactional_outbox` for a verification code and `notification_outbox`
-- for a finding, exactly as email does, and the only new table is the one that
-- records which chat a person has proved they hold.
--
-- WHAT IS DELIBERATELY NOT IN THE DATABASE
--
-- The bot token. It is an operator secret of the same class as the SMTP
-- password, it is read from core-api's configuration by the dispatcher and
-- nowhere else, and a deployment that has not set it simply does not offer the
-- channel. Putting it in a table would make every organisation's ability to be
-- notified depend on a credential a tenant could in principle read, and would
-- put a live bot token in every backup of the domain schema.
--
-- WHAT A `notification_channels` ROW IS
--
-- A consent record, not a contact book. It says: this person, in this
-- organisation, has claimed this Telegram chat, and here is whether they proved
-- it. Proving it is the same shape as email verification: a code goes to the
-- chat, the person types it into the console, and until they do the dispatcher
-- treats the chat as if it were not there.
--
-- The chat id is not a credential and is not secret. It is an identifier the
-- person supplied about themselves, and it is personal data, which is why it is
-- readable only by its owner (the policies below pin `user_id` to the GUC, the
-- same way `notification_preferences` does and for the same reason) and why the
-- retention pass clears it out of delivered outbox rows alongside the address.
--
-- The verification CODE is a credential, briefly, so it is stored hashed and
-- with an expiry, like `invitations.token_hash` and `capability_tokens`. A
-- database dump must not yield a working code.
--
-- WHY THERE IS NO INBOUND PATH HERE
--
-- No webhook, no long poll, nothing that reads a message a person sent to the
-- bot. That is a deliberate bound rather than an omission: anything typed by a
-- person into a chat is data and never instruction (OWASP LLM01), and the
-- moment the product ingests chat messages it needs a whole answer to where
-- that data may and may not flow. Linking works without it, because the person
-- supplies their own chat id and proves it by reading a code the bot sent them,
-- which is the property that actually matters. The inbound half arrives with
-- the Messenger (ENT-260), which is what would have a reason to read a reply.

------------------------------------------------------------------------------
-- notification_channels
------------------------------------------------------------------------------

create table public.notification_channels (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  -- Whose channel. Not who linked it on somebody's behalf: there is no such
  -- thing here, because consent to be messaged is not delegable and the
  -- policies below make that structural rather than a rule in Go.
  user_id uuid not null,

  -- Check-constrained rather than free text, and widened by the change that
  -- introduces a channel, never in advance. Same reasoning as
  -- `transactional_outbox.kind` (00014): a typo in a channel name should be a
  -- refused insert, not a row nothing will ever deliver.
  kind text not null check (kind in ('telegram')),

  -- Telegram's chat id. Text rather than bigint because it is an opaque
  -- identifier the product never does arithmetic on, and because a channel
  -- added later will not have a numeric one.
  chat_id text not null check (chat_id <> ''),

  -- The pending proof. Hashed, never in the clear: a code that reaches this
  -- chat is briefly enough to link somebody else's chat to your account, so it
  -- gets the invitation token's treatment.
  verification_code_hash text,
  verification_expires_at timestamptz,
  -- Counted, so a code short enough for a person to type is not also short
  -- enough for a caller to guess. Go refuses past the ceiling; the column is
  -- here because the count has to survive the request that incremented it.
  verification_attempts integer not null default 0,

  verified_at timestamptz,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  -- One chat per person per channel. Relinking replaces rather than
  -- accumulating, so "which chat does this person use" has one answer and
  -- unlinking cannot leave a stale row that still receives.
  constraint notification_channels_one_per_kind unique (org_id, user_id, kind),

  -- And one person per chat, within an organisation.
  --
  -- Without this, two members could both claim the same chat, and the second to
  -- verify would start receiving findings addressed to the first. The unique
  -- index is the enforcement; verification is what stops somebody claiming a
  -- chat they do not hold in the first place.
  constraint notification_channels_one_owner_per_chat unique (org_id, kind, chat_id),

  -- A verified channel has no code outstanding.
  --
  -- The pair is the state machine: a code and no `verified_at` is pending, a
  -- `verified_at` and no code is linked, and both at once is a row that would
  -- let a stale code re-verify a channel somebody has already proved. The
  -- database refuses the combination rather than trusting every writer to clear
  -- one when it sets the other.
  constraint notification_channels_verified_has_no_pending_code
    check (verified_at is null or verification_code_hash is null),

  -- A code without an expiry never expires, which is the whole point of an
  -- expiry. Stated as an equivalence so neither half can be written alone.
  constraint notification_channels_code_expires
    check ((verification_code_hash is null) = (verification_expires_at is null))
);

-- The dispatcher's question, asked once per recipient per notification: does
-- this person have a verified chat in this organisation.
create index notification_channels_org_user_idx
  on public.notification_channels (org_id, user_id);

create trigger set_updated_at
  before update on public.notification_channels
  for each row execute function public.set_updated_at();

alter table public.notification_channels enable row level security;
alter table public.notification_channels force row level security;

-- Written out rather than inherited. Since 00029 a table arrives with no
-- default privilege attached to it, so these four are the whole of what any
-- application role can do here.
--
-- No grant to kindlast_agent, deliberately. The dispatcher needs one fact about
-- a chat and gets it through `notification_recipients`, which is SECURITY
-- DEFINER and answers about one outbox row. 00015 argued that at length for
-- memberships and addresses and the argument is unchanged: a role that can
-- fabricate a finding should not also be able to enumerate every member's
-- messaging identity across every tenant in the deployment.
grant select, insert, update, delete on public.notification_channels to kindlast_app;

-- Personal within the organisation, like notification_preferences and unlike
-- almost everything else in this schema. Members share an organisation and do
-- not share a messenger account, so `user_id = app.current_user_id` is on every
-- command rather than only on the writes.
create policy notification_channels_select_own on public.notification_channels
  for select to kindlast_app
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy notification_channels_insert_own on public.notification_channels
  for insert to kindlast_app
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

create policy notification_channels_update_own on public.notification_channels
  for update to kindlast_app
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  )
  with check (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

-- Unlinking is a delete, and it is the acceptance criterion that says future
-- messages go to the remaining channel or nowhere and never to the unlinked
-- chat. A soft delete would have left a row the dispatcher had to remember to
-- filter, which is the shape of bug that only shows up in production.
create policy notification_channels_delete_own on public.notification_channels
  for delete to kindlast_app
  using (
    org_id = (select current_setting('app.current_org_id')::uuid)
    and user_id = (select current_setting('app.current_user_id')::uuid)
    and exists (
      select 1 from public.memberships m
      where m.org_id = (select current_setting('app.current_org_id')::uuid)
        and m.user_id = (select current_setting('app.current_user_id')::uuid)
    )
  );

------------------------------------------------------------------------------
-- A channel choice on the preferences row
------------------------------------------------------------------------------
--
-- One column, and what it does NOT change is the point. Quiet hours and the
-- severity floor stay exactly where they are and keep applying per person
-- regardless of channel, because they are statements about when somebody wants
-- to be interrupted rather than about how. Duplicating them per channel would
-- let a person set a quiet window on email and forget the one on Telegram, and
-- then be woken at four in the morning by a product that had been told twice
-- not to.
--
-- Defaulting to email means every existing row keeps behaving identically, and
-- a person only reaches Telegram by linking a chat, proving it, and then saying
-- so. Three deliberate steps for a channel that pushes to somebody's phone.
alter table public.notification_preferences
  add column finding_channel text not null default 'email'
    check (finding_channel in ('email', 'telegram'));

------------------------------------------------------------------------------
-- The transactional outbox carries a channel
------------------------------------------------------------------------------
--
-- WHY THE VERIFICATION CODE GOES THROUGH THE OUTBOX AT ALL
--
-- Because the alternative is the second mechanism this issue exists to avoid.
-- A handler that called Telegram directly would have its own retry, its own
-- failure log, and a window between "the code is in the database" and "the code
-- reached the person" in which a crash loses one and not the other. Written
-- inside the same transaction as the pending code, the row cannot disagree with
-- it, and the existing relay delivers it with the existing retry policy.
--
-- WHY A SECOND RECIPIENT COLUMN RATHER THAN RENAMING THE FIRST
--
-- `recipient_email` is not merely a name here. 00030's retention pass keys
-- `invitation_is_live` off it and redacts it, so widening its meaning to "the
-- address, whatever kind" would either break that function or quietly change
-- what it is matching on. Two typed columns leave the email path byte for byte
-- as it was and make a telegram row obviously a telegram row.
alter table public.transactional_outbox
  add column channel text not null default 'email'
    check (channel in ('email', 'telegram')),
  -- Empty rather than null when unused, matching how `recipient_email` records
  -- a redacted address, so the redaction constraint below can say one thing
  -- about both columns.
  add column recipient_chat_id text not null default '';

-- And the older column learns the same default, which is a smaller change than
-- it looks and was found by driving the path rather than by reading it.
--
-- `recipient_email` has been `not null` with no default since 00014, which was
-- exactly right while every row was an email: a message with no recipient was a
-- bug, and the absent default made it a refused insert. With a second channel
-- that same absence means a Telegram row must name an address it does not have,
-- so every writer has to remember to pass an empty string for a column that has
-- nothing to do with it. The Go store already does; the next writer, and anybody
-- inserting a row by hand while debugging, would not, and would meet a
-- `not-null` violation naming a column their message has no concept of.
--
-- The invariant that matters did not live here anyway. It is
-- `transactional_outbox_recipient_matches_channel` below, which says a row
-- addresses exactly one channel's worth of recipient, and it says it for both
-- columns at once.
alter table public.transactional_outbox
  alter column recipient_email set default '';

-- A row addresses exactly one channel's worth of recipient.
--
-- The `redacted_at` escape is not laziness: a reclaimed row has had both
-- columns cleared on purpose, and a constraint that forbade that would make the
-- retention pass unable to do the thing it exists to do.
alter table public.transactional_outbox
  add constraint transactional_outbox_recipient_matches_channel
    check (
      redacted_at is not null
      or (channel = 'email'    and recipient_chat_id =  '')
      or (channel = 'telegram' and recipient_chat_id <> '')
    );

-- The redaction is complete only when both recipients are gone.
--
-- Dropped and re-added rather than added beside, because two constraints each
-- describing "complete" is how one of them ends up describing it wrongly.
alter table public.transactional_outbox
  drop constraint transactional_outbox_redaction_is_complete;

alter table public.transactional_outbox
  add constraint transactional_outbox_redaction_is_complete
    check (
      redacted_at is null
      or (recipient_email   = ''
          and recipient_chat_id = ''
          and subject       = ''
          and body_text     = ''
          and body_html is null)
    );

-- A second kind, widened here and not in advance, which is what 00014 asked
-- for. `telegram_verification` carries a short-lived code to a chat somebody is
-- claiming.
alter table public.transactional_outbox
  drop constraint transactional_outbox_kind_check;

alter table public.transactional_outbox
  add constraint transactional_outbox_kind_check
    check (kind in ('invitation', 'telegram_verification'));

------------------------------------------------------------------------------
-- The retention pass learns about the second recipient
------------------------------------------------------------------------------
--
-- Two changes and no new behaviour beyond them.
--
-- The first is that redaction clears `recipient_chat_id`. A chat id is personal
-- data in the same sense an address is, and a retention pass that cleared one
-- and left the other would report a redaction it had not performed. The new
-- constraint above makes that a refused write rather than a silent gap, so
-- these two edits have to land together.
--
-- The second is a bound on an undelivered verification code. 00030 abandons a
-- message whose invitation can no longer be accepted, and gave a second kind a
-- deliberate fall-through to the window alone. That fall-through is right for a
-- kind whose value decays slowly and wrong for this one: a verification code is
-- useless within minutes, so a pending row for a chat that cannot be reached
-- would retry with backoff for as long as the deployment lives, carrying a code
-- nobody can use any more. An hour is generous against a ten minute code and
-- short enough that the retry stops being a fixture of the queue.
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
     for update skip locked
  )
  update public.transactional_outbox t
     set recipient_email   = '',
         recipient_chat_id = '',
         subject           = '',
         body_text         = '',
         body_html         = null,
         last_error        = null,
         redacted_at       = now()
    from due
   where t.id = due.id;
  get diagnostics v_redacted = row_count;

  with dead as (
    select o.id
      from public.transactional_outbox o
     where o.status <> 'sent'
       and o.redacted_at is null
       and (
         (o.kind = 'invitation'
            and not public.invitation_is_live(o.org_id, o.recipient_email))
         or (o.kind = 'telegram_verification'
            and o.created_at < now() - interval '1 hour')
       )
     order by o.created_at
     limit p_batch
     for update skip locked
  )
  update public.transactional_outbox t
     set status            = 'failed',
         recipient_email   = '',
         recipient_chat_id = '',
         subject           = '',
         body_text         = '',
         body_html         = null,
         last_error        = case t.kind
           when 'telegram_verification' then
             'abandoned undelivered: the verification code this message carries has expired'
           else
             'abandoned undelivered: the invitation this message carries can no longer be accepted'
         end,
         redacted_at       = now()
    from dead
   where t.id = dead.id;
  get diagnostics v_abandoned = row_count;

  return query select v_redacted, v_abandoned;
end;
$function$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- notification_recipients learns the channel and the chat
------------------------------------------------------------------------------
--
-- Two more columns, and the function still fetches rather than decides.
--
-- It would be one line shorter to return the chat id only when `verified_at` is
-- set, and that line would be the product rule "an unverified chat is not
-- delivered to" written in plpgsql, where it cannot be unit tested and where a
-- later reader can disagree with it without anything going red. §14.5 puts that
-- rule in Go. So this returns the chat and whether it is verified, separately,
-- and `notify.Route` refuses. The test that proves the refusal can fail is a Go
-- table test rather than something that needs a live database.
--
-- The address filter is relaxed at the same time, and it has to be: it read
-- "exclude anybody with no email address", which was right when email was the
-- only channel and silently drops a person who has deliberately chosen Telegram
-- and happens to have no address on file.
--
-- Dropped and recreated because a `returns table` signature cannot be replaced
-- in place.
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
  org_name           text,
  -- Which channel this person asked for. `email` for everybody who has never
  -- said otherwise, including everybody who has no preferences row at all.
  finding_channel    text,
  -- The chat they claimed, and whether they proved it. Both, always, so the
  -- refusal is Go's to make and Go's to be tested on.
  telegram_chat_id   text,
  telegram_verified  boolean
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  select
    m.user_id,
    coalesce(nullif(np.email, ''), ui.email, '')                 as email,
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
    org.name                                                     as org_name,
    coalesce(np.finding_channel, 'email')                        as finding_channel,
    coalesce(nc.chat_id, '')                                     as telegram_chat_id,
    (nc.verified_at is not null)                                 as telegram_verified
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
  left join public.notification_channels nc
    on nc.org_id = o.org_id and nc.user_id = m.user_id and nc.kind = 'telegram'
  where o.id = p_outbox_id
    -- Somewhere to send it. An address, or a chat that has been claimed;
    -- whether the claim was proved is decided upstream, in Go.
    and (coalesce(nullif(np.email, ''), ui.email) is not null
         or nc.chat_id is not null);
$function$;
-- +goose StatementEnd

revoke all on function public.notification_recipients(uuid) from public;
grant execute on function public.notification_recipients(uuid) to kindlast_agent;

-- +goose Down

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
     for update skip locked
  )
  update public.transactional_outbox t
     set recipient_email = '',
         subject         = '',
         body_text       = '',
         body_html       = null,
         last_error      = null,
         redacted_at     = now()
    from due
   where t.id = due.id;
  get diagnostics v_redacted = row_count;

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
         last_error      = 'abandoned undelivered: the invitation this message carries can no longer be accepted',
         redacted_at     = now()
    from dead
   where t.id = dead.id;
  get diagnostics v_abandoned = row_count;

  return query select v_redacted, v_abandoned;
end;
$function$;
-- +goose StatementEnd

alter table public.transactional_outbox
  drop constraint if exists transactional_outbox_kind_check;

alter table public.transactional_outbox
  add constraint transactional_outbox_kind_check
    check (kind in ('invitation'));

alter table public.transactional_outbox
  drop constraint if exists transactional_outbox_redaction_is_complete;

alter table public.transactional_outbox
  add constraint transactional_outbox_redaction_is_complete
    check (
      redacted_at is null
      or (recipient_email = ''
          and subject     = ''
          and body_text   = ''
          and body_html is null)
    );

alter table public.transactional_outbox
  drop constraint if exists transactional_outbox_recipient_matches_channel;

alter table public.transactional_outbox
  alter column recipient_email drop default;

alter table public.transactional_outbox
  drop column if exists recipient_chat_id,
  drop column if exists channel;

alter table public.notification_preferences
  drop column if exists finding_channel;

drop policy if exists notification_channels_delete_own on public.notification_channels;
drop policy if exists notification_channels_update_own on public.notification_channels;
drop policy if exists notification_channels_insert_own on public.notification_channels;
drop policy if exists notification_channels_select_own on public.notification_channels;
revoke all on public.notification_channels from kindlast_app;
drop table if exists public.notification_channels;
