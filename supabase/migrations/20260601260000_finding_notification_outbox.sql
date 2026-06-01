-- ENT-73 — Founder receives email when a new finding is created.
--
-- The Analyst inserts findings from SQL (analyst_convert_signal, run by cron),
-- so there is no TypeScript hook at creation time. This migration adds a
-- transactional outbox: an AFTER INSERT trigger on findings enqueues exactly one
-- notification row per finding, and the Comms dispatcher (a cron-invoked Next
-- route, lib/notifications/dispatch.ts) drains it — applying the severity gate,
-- rendering the email, and sending. The UNIQUE(finding_id) constraint is what
-- guarantees the AC "one email per finding (deduplicated by finding id)".
--
-- It also parameterizes reject_finding / snooze_finding with an explicit acting
-- user, so the session-less one-tap email links can call them via the service
-- role (approve_finding already takes p_approving_user_id).

-- 1. notification_outbox ────────────────────────────────────────────────────────
create table if not exists public.notification_outbox (
  id          uuid        primary key default gen_random_uuid(),
  finding_id  uuid        not null unique
                            references public.findings(id) on delete cascade,
  user_id     uuid        not null references auth.users(id) on delete cascade,
  channel     text        not null default 'email',
  status      text        not null default 'pending'
                            check (status in ('pending', 'sent', 'skipped', 'failed')),
  attempts    integer     not null default 0,
  last_error  text,
  created_at  timestamptz not null default now(),
  sent_at     timestamptz,
  updated_at  timestamptz not null default now()
);

-- Drain query: oldest pending first.
create index if not exists notification_outbox_status_created_idx
  on public.notification_outbox (status, created_at);

drop trigger if exists set_updated_at on public.notification_outbox;
create trigger set_updated_at
  before update on public.notification_outbox
  for each row execute function public.set_updated_at();

-- RLS: a user may read their own queued/sent notifications (for an in-app
-- activity view later). There are no INSERT/UPDATE/DELETE policies — the only
-- writer is the SECURITY DEFINER enqueue trigger below, and the dispatcher
-- writes via the service role. RLS denies by default, which is the enforcement.
alter table public.notification_outbox enable row level security;

drop policy if exists "notification_outbox_select_own" on public.notification_outbox;
create policy "notification_outbox_select_own" on public.notification_outbox
  for select using (auth.uid() = user_id);

-- 2. enqueue trigger ────────────────────────────────────────────────────────────
--
-- Every new finding (the Analyst inserts them as 'pending') gets one outbox row.
-- on conflict do nothing makes a re-inserted / re-converted finding idempotent.
create or replace function public.enqueue_finding_notification()
returns trigger
language plpgsql
security definer
set search_path = public, pg_temp
as $$
begin
  insert into public.notification_outbox (finding_id, user_id)
  values (new.id, new.user_id)
  on conflict (finding_id) do nothing;
  return new;
end;
$$;

drop trigger if exists enqueue_finding_notification on public.findings;
create trigger enqueue_finding_notification
  after insert on public.findings
  for each row execute function public.enqueue_finding_notification();

-- 3. reject_finding — acting-user parameter ─────────────────────────────────────
--
-- Recreated (drop, not overload, to avoid default-arg ambiguity) with a trailing
-- p_acting_user_id. Resolves the actor as coalesce(p_acting_user_id, auth.uid()),
-- so the in-app path (session → auth.uid()) is unchanged while the one-tap email
-- channel can pass the finding owner explicitly under the service role. Body is
-- otherwise identical to the ENT-69 version (the third-rejection review flag).
drop function if exists public.reject_finding(uuid, text);
create function public.reject_finding(
  p_finding_id      uuid,
  p_reason          text default null,
  p_acting_user_id  uuid default null
)
returns boolean
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user    uuid := coalesce(p_acting_user_id, auth.uid());
  v_updated uuid;
  v_profile uuid;
  v_slug    text;
  v_count   int;
  c_threshold constant int := 3;
begin
  if v_user is null then
    raise exception 'reject_finding: not authenticated';
  end if;

  update public.findings
    set status = 'rejected',
        rejection_reason = nullif(btrim(p_reason), ''),
        snoozed_until = null
  where id = p_finding_id
    and user_id = v_user
    and status <> 'rejected'
  returning id, profile_id, obligation_slug
    into v_updated, v_profile, v_slug;

  if v_updated is not null and v_slug is not null then
    select count(*)
      into v_count
      from public.findings
     where profile_id = v_profile
       and obligation_slug = v_slug
       and status = 'rejected';

    if v_count >= c_threshold then
      insert into public.product_review_flags (
        user_id, profile_id, obligation_slug, finding_id, rejection_count, reasons
      )
      values (
        v_user,
        v_profile,
        v_slug,
        v_updated,
        v_count,
        (
          select array_remove(array_agg(distinct rejection_reason), null)
            from public.findings
           where profile_id = v_profile
             and obligation_slug = v_slug
             and status = 'rejected'
        )
      )
      on conflict (profile_id, obligation_slug) do nothing;
    end if;
  end if;

  return v_updated is not null;
end;
$$;

-- 4. snooze_finding — acting-user parameter ─────────────────────────────────────
drop function if exists public.snooze_finding(uuid, integer);
create function public.snooze_finding(
  p_finding_id     uuid,
  p_days           integer default 7,
  p_acting_user_id uuid default null
)
returns timestamptz
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_user  uuid := coalesce(p_acting_user_id, auth.uid());
  v_days  integer := greatest(1, least(coalesce(p_days, 7), 365));
  v_until timestamptz;
begin
  if v_user is null then
    raise exception 'snooze_finding: not authenticated';
  end if;

  update public.findings
    set status = 'snoozed',
        snoozed_until = now() + make_interval(days => v_days)
  where id = p_finding_id
    and user_id = v_user
  returning snoozed_until into v_until;

  return v_until;  -- null when nothing matched (unknown / not owned)
end;
$$;
