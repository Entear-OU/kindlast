-- +goose Up
-- 00015_notification_dispatch.sql (ENT-209)
--
-- Somebody to answer the doorbell, and a way to say stop ringing it.
--
-- THE GAP, MEASURED
--
-- `enqueue_finding_notification` has been a live trigger since 00002: every
-- finding inserted writes a row into `notification_outbox`. Nothing has ever
-- read one. The doorbell rings and no process, in any language, is listening.
--
-- It is worse than "not built yet", because the two roles that could listen are
-- each missing a different half:
--
--   kindlast_app    has a SELECT policy and no UPDATE policy, so it can see a
--                   pending row and cannot mark it delivered.
--   kindlast_agent  is granted `select, insert` and no update, so it can ring
--                   the bell and cannot record that anyone answered.
--
-- 00008 wrote the intent down plainly: "the agent enqueues, and something else
-- delivers." This is that something else, and it is the agent after all, on the
-- other side of the same table. 00014 drew the mirror-image split for the
-- transactional outbox, where the application enqueues and the agent delivers.
-- Neither role can do both halves of either path, which is the property worth
-- keeping.
--
-- WHY THE DISPATCHER READS EVERY ORGANISATION
--
-- Same reasoning as 00014. Draining an outbox is inherently cross-tenant: a
-- loop that could only see one organisation's rows would first have to ask
-- which organisations exist, and that question is the cross-tenant read it was
-- trying to avoid.
--
-- So SELECT and UPDATE become unconditional for kindlast_agent on this table,
-- and INSERT deliberately does not. The existing `notification_outbox_agent`
-- policy stays exactly as it is and keeps enqueue org-scoped: a doorbell can
-- only be rung for the organisation whose GUC is set, which is what the finding
-- trigger does. Permissive policies OR together, so adding SELECT and UPDATE
-- here widens exactly those two commands and leaves INSERT alone.
--
-- WHY RECIPIENTS COME FROM A FUNCTION AND NOT FROM GRANTS
--
-- This is the part worth arguing with.
--
-- Resolving who to tell needs `memberships`, `notification_preferences` and
-- `user_identities`. The agent has grants on none of them, deliberately: 00008
-- says it gets "no organisations, no memberships, no audit_log", on the
-- principle that a role which can fabricate a finding should not also be able
-- to enumerate people.
--
-- Granting it SELECT on those three tables would close this issue in one line
-- and would mean a compromised agent could read every member's email address
-- and organisation across every tenant in the deployment. That is a real
-- widening of the blast radius for a role whose whole design is narrowness, and
-- it would quietly reverse a decision somebody made on purpose.
--
-- `notification_recipients` is the narrow version. It is SECURITY DEFINER, it
-- takes one outbox row, and it answers only "who has asked to hear about this
-- one thing". The agent still cannot list users, cannot read a membership it
-- was not handed, and cannot ask about an organisation it has no outbox row
-- for. What it gains is exactly the question it needs answered and nothing
-- adjacent to it.
--
-- The function fetches and does not decide. It returns each candidate's raw
-- preference fields and the finding's severity, and Go compares them (§14.5:
-- Postgres keeps invariants, Go decides). Filtering inside the function would
-- put the "should this person be emailed" rule in plpgsql, where it is hard to
-- test and easy for a later reader to disagree with silently.
--
-- SECURITY DEFINER is the third such function in this schema and carries the
-- same obligations 00003 spelled out for `accept_invitation`: a pinned
-- search_path, no dynamic SQL, arguments that cannot widen what it returns, and
-- EXECUTE granted to exactly the role that needs it.

------------------------------------------------------------------------------
-- The doorbell dispatch policies
------------------------------------------------------------------------------

grant update on public.notification_outbox to kindlast_agent;

-- SELECT and UPDATE only. No insert policy is added, so enqueue stays scoped by
-- `notification_outbox_agent` to the organisation the GUC names.
create policy notification_outbox_dispatch_read on public.notification_outbox
  for select to kindlast_agent
  using (true);

create policy notification_outbox_dispatch_write on public.notification_outbox
  for update to kindlast_agent
  using (true)
  with check (true);

------------------------------------------------------------------------------
-- notification_recipients
------------------------------------------------------------------------------

-- +goose StatementBegin
create or replace function public.notification_recipients(p_outbox_id uuid)
returns table (
  -- The minimal projection delivery needs, and deliberately nothing adjacent.
  --
  -- No role, no membership row, and no identity beyond the address. `user_id`
  -- is here only because each recipient gets their own unsubscribe token and a
  -- shared one would let any recipient unsubscribe every other. A display name
  -- would be pleasant in a greeting and is exactly the sort of extra column
  -- that turns a narrow answer into a slow enumeration of everybody.
  user_id            uuid,
  email              text,
  min_severity       public.severity_level,
  finding_severity   public.severity_level,
  timezone           text,
  quiet_hours_start  time,
  quiet_hours_end    time,
  -- The organisation, carried here rather than fetched separately.
  --
  -- Every notification links into `/o/{slug}/`, so the dispatcher needs the
  -- slug, and the agent has no grant on `organisations` either. Adding one
  -- would reopen exactly the argument this function exists to close: it would
  -- let a compromised agent enumerate every tenant in the deployment to save a
  -- join it can already reach through here.
  --
  -- Repeated on every row, which is redundant and is the cheaper redundancy: a
  -- second round trip per notification, or a second grant, are both worse.
  org_slug           text,
  org_name           text
)
language sql
stable
security definer
set search_path to 'public', 'pg_temp'
as $function$
  -- One outbox row in, the people who might want to hear about it out.
  --
  -- The join to `notification_preferences` is a LEFT join on purpose: somebody
  -- who has never opened the settings page has no row, and the product default
  -- is that they are told. Requiring a row would mean silence for every member
  -- of every organisation until each of them opted in individually, which for a
  -- compliance product means a critical finding nobody hears about.
  --
  -- The address falls back from the preferences override to the identity's own,
  -- because `notification_preferences.email` exists so a person can be told
  -- somewhere other than where they sign in.
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
    -- No address, nothing to send to. Excluded here rather than in Go so the
    -- caller never holds a row it cannot act on.
    and coalesce(nullif(np.email, ''), ui.email) is not null;
$function$;
-- +goose StatementEnd

revoke all on function public.notification_recipients(uuid) from public;
grant execute on function public.notification_recipients(uuid) to kindlast_agent;

------------------------------------------------------------------------------
-- Capability tokens
------------------------------------------------------------------------------
--
-- A link in an email that acts without a session.
--
-- The threat model is the invitation token's, because it is the same object: a
-- bearer credential that lives in a mailbox, in a mail server's logs, and in
-- whatever proxy scanned the message on the way. So it is stored hashed,
-- expires, is single use, and is answered for identically whether it is
-- expired, already used, or was never real. That last property is what stops it
-- being an oracle for which tokens exist.
--
-- `kind` is check-constrained to `unsubscribe` and nothing else, deliberately.
--
-- The design (§8) also anticipates an "act from email" token, so a finding can
-- be approved from a link. That is not minted here and the constraint refuses
-- it, because approving a finding is a regulatory decision that this schema
-- already gates on a verified email address and a role, and reproducing that
-- authority in a bearer link is a design question rather than a column. When
-- it is answered, widening this constraint is the smallest part of the change.
--
-- `org_id` is not decoration. §8 names the failure this prevents: a consultant
-- with three clients clicking a stale link and acting against the wrong
-- company. The token names the organisation it belongs to, so redemption can
-- land in that one rather than in whichever the recipient's session last
-- pointed at.

create table public.capability_tokens (
  id     uuid primary key default gen_random_uuid(),
  org_id uuid not null references public.organisations(id) on delete cascade,

  kind text not null check (kind in ('unsubscribe')),

  -- Hashed, never in the clear, for the same reason `invitations.token_hash`
  -- is: a database dump, a backup or a support engineer reading this row must
  -- not yield a working link.
  token_hash text not null,

  -- Who the link was issued to. Not who is redeeming it: there is no session,
  -- so the token is the only claim of identity, which is exactly why it is
  -- single use and expiring.
  user_id uuid not null,

  expires_at timestamptz not null,
  redeemed_at timestamptz,
  created_at timestamptz not null default now(),

  constraint capability_tokens_token_hash_key unique (token_hash)
);

create index capability_tokens_org_created_idx
  on public.capability_tokens (org_id, created_at desc);

alter table public.capability_tokens enable row level security;
alter table public.capability_tokens force row level security;

-- Minted by the dispatcher, which is the only thing that sends a link.
grant select, insert, update on public.capability_tokens to kindlast_agent;

create policy capability_tokens_agent on public.capability_tokens
  to kindlast_agent
  using (true)
  with check (true);

-- Revoked from kindlast_app explicitly, rather than left to the absence of a
-- policy.
--
-- 00002 set default privileges granting the application DML on every table the
-- migrator creates, so this one arrived with select, insert, update and delete
-- already attached. With FORCE RLS and no policy the reads would return zero
-- rows and the writes would touch none, silently, which reads exactly like a
-- boundary and is not one: it is a table the application can address and simply
-- finds empty. A missing grant fails closed at parse time, where a missing
-- policy fails quietly at run time, and for a table of bearer credentials the
-- loud version is the one worth having.
--
-- The application never needs a row here anyway. It hands a hash to
-- `redeem_capability_token` and is told an organisation or nothing, which is
-- how a caller with no session, and therefore no GUCs, can act at all.
revoke all on public.capability_tokens from kindlast_app;

-- +goose StatementBegin
create or replace function public.redeem_capability_token(p_token_hash text, p_kind text)
returns uuid
language plpgsql
security definer
set search_path to 'public', 'pg_temp'
as $function$
declare
  v_token public.capability_tokens%rowtype;
begin
  -- No `app_current_user_id()` here, unlike accept_invitation, and that is the
  -- whole point: this runs for somebody who has no session. The token is the
  -- only identity claim, which is why it is single use, short lived, and why
  -- redemption is recorded in the same statement that acts on it.
  select * into v_token
  from public.capability_tokens
  where token_hash = p_token_hash
    and kind = p_kind
    and redeemed_at is null
    and expires_at > now()
  for update;

  if not found then
    -- Expired, already redeemed, wrong kind, and never existed are one answer.
    -- Distinguishing them turns this into an oracle for which tokens are real,
    -- and the caller has no session to prove they are entitled to the
    -- difference. Same decision as accept_invitation (00003).
    return null;
  end if;

  update public.capability_tokens
     set redeemed_at = now()
   where id = v_token.id;

  if p_kind = 'unsubscribe' then
    -- Upsert rather than update: somebody who has never opened the settings
    -- page has no preferences row, and they are exactly the person most likely
    -- to want out. An update touching zero rows would report success and change
    -- nothing, which is the worst possible outcome for this particular button.
    insert into public.notification_preferences
      (org_id, user_id, weekly_briefing_enabled, deadline_alerts_enabled,
       min_severity_for_email)
    values
      (v_token.org_id, v_token.user_id, false, false, 'critical')
    on conflict (org_id, user_id) do update
      set weekly_briefing_enabled = false,
          deadline_alerts_enabled = false,
          min_severity_for_email  = 'critical';
  end if;

  -- The organisation the token names, so the caller can be shown which one they
  -- have just changed rather than guessing from a session they do not have.
  return v_token.org_id;
end;
$function$;
-- +goose StatementEnd

revoke all on function public.redeem_capability_token(text, text) from public;
grant execute on function public.redeem_capability_token(text, text) to kindlast_app;

-- +goose Down

drop function if exists public.redeem_capability_token(text, text);

drop policy if exists capability_tokens_agent on public.capability_tokens;
revoke all on public.capability_tokens from kindlast_agent;
drop table if exists public.capability_tokens;

drop function if exists public.notification_recipients(uuid);

drop policy if exists notification_outbox_dispatch_write on public.notification_outbox;
drop policy if exists notification_outbox_dispatch_read  on public.notification_outbox;
revoke update on public.notification_outbox from kindlast_agent;
