-- Subscriptions (ENT-81)
--
-- One row per user holding their billing tier. This is the source of truth every
-- tier check reads (`getPlan`) and the upgrade / webhook flows write. Created
-- here so the rest of the Subscriptions epic (ENT-82..86) has a row to gate on.
--
-- Two design choices worth calling out:
--
--   * `user_id` is UNIQUE — exactly one subscription per user. A user is either
--     Free or Pro; we never carry parallel rows. The unique constraint also
--     makes the insert trigger and the webhook upsert (ENT-86) idempotent.
--   * Writes are service-role-only. Unlike the user-owned tables in the baseline
--     convention, a user must NOT be able to flip their own `plan` to 'pro' — only
--     the signup trigger and the billing webhook (both service role) write here.
--     So RLS enables a select-own policy and *no* user insert/update/delete
--     policies; service role bypasses RLS entirely.
--
-- `provider`-specific columns (e.g. a customer id) are intentionally absent here;
-- ENT-85/86 add them when the payment provider lands, kept behind a swappable
-- provider seam.
--
-- Idempotent: `if not exists`, `or replace`, `drop policy/trigger if exists` +
-- recreate, so the migration re-applies cleanly during local development.
-- ─────────────────────────────────────────────────────────────────────────────

create table if not exists public.subscriptions (
  id         uuid        primary key default gen_random_uuid(),
  user_id    uuid        not null unique
                           references auth.users(id) on delete cascade,
  plan       text        not null default 'free'
                           check (plan in ('free', 'pro')),
  status     text        not null default 'active'
                           check (status in ('active', 'past_due', 'canceled')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists subscriptions_user_idx
  on public.subscriptions (user_id);

drop trigger if exists set_updated_at on public.subscriptions;
create trigger set_updated_at
  before update on public.subscriptions
  for each row execute function public.set_updated_at();

-- RLS ────────────────────────────────────────────────────────────────────────
-- Read-own only. No user write policies: the row is mutated solely by the signup
-- trigger and the billing webhook, which run as the service role (RLS-exempt).

alter table public.subscriptions enable row level security;

drop policy if exists "subscriptions_select_own" on public.subscriptions;
create policy "subscriptions_select_own" on public.subscriptions
  for select using (auth.uid() = user_id);

-- Signup trigger ───────────────────────────────────────────────────────────────
-- Every new `auth.users` row gets a default Free/active subscription, so tier
-- checks always have a row to read and the upgrade flow has somewhere to update.
--
-- SECURITY DEFINER so the trigger writes to public.subscriptions regardless of
-- the (unauthenticated) context inserting into auth.users. `on conflict
-- (user_id) do nothing` keeps it safe against re-runs and the backfill below.

create or replace function public.handle_new_user_subscription()
returns trigger
language plpgsql
security definer
set search_path = public
as $$
begin
  insert into public.subscriptions (user_id, plan, status)
  values (new.id, 'free', 'active')
  on conflict (user_id) do nothing;
  return new;
end;
$$;

drop trigger if exists on_auth_user_created_subscription on auth.users;
create trigger on_auth_user_created_subscription
  after insert on auth.users
  for each row execute function public.handle_new_user_subscription();

-- Backfill ─────────────────────────────────────────────────────────────────────
-- Defensive: give any pre-existing user (none expected yet) a Free row so no
-- account is left without a subscription to read.

insert into public.subscriptions (user_id, plan, status)
select u.id, 'free', 'active'
from auth.users u
on conflict (user_id) do nothing;
