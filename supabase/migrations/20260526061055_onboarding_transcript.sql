-- Onboarding transcript persistence (ENT-47)
--
-- Two tables backing the conversational onboarding flow (ENT-31 epic):
--
--   * `onboarding_sessions`  — one row per onboarding attempt by a user.
--   * `onboarding_messages`  — every turn in the conversation, ordered.
--
-- Both are user-owned and follow the RLS convention codified in the baseline
-- migration: `user_id` references `auth.users(id)` and the four per-operation
-- policies scope to `auth.uid() = user_id`. `onboarding_messages.user_id` is
-- denormalised from its parent session so RLS can be enforced without a join.
--
-- Re-interviews (PRD §6.1 — shadow-AI follow-ups, profile drift, audit replays)
-- are modelled as additional `onboarding_sessions` rows. There is no unique
-- constraint on `(user_id)` and no in-place mutation of completed sessions:
-- the historical transcript stays intact and queryable.
--
-- Idempotent: `if not exists`, `drop policy/trigger if exists` + recreate, so
-- the migration can be re-applied during local development without error.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. onboarding_sessions ─────────────────────────────────────────────────────

create table if not exists public.onboarding_sessions (
  id            uuid        primary key default gen_random_uuid(),
  user_id       uuid        not null references auth.users(id) on delete cascade,
  status        text        not null default 'in_progress'
                  check (status in ('in_progress', 'completed', 'abandoned')),
  started_at    timestamptz not null default now(),
  completed_at  timestamptz,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

create index if not exists onboarding_sessions_user_started_idx
  on public.onboarding_sessions (user_id, started_at desc);

drop trigger if exists set_updated_at on public.onboarding_sessions;
create trigger set_updated_at
  before update on public.onboarding_sessions
  for each row execute function public.set_updated_at();

alter table public.onboarding_sessions enable row level security;

drop policy if exists "onboarding_sessions_select_own" on public.onboarding_sessions;
create policy "onboarding_sessions_select_own" on public.onboarding_sessions
  for select using (auth.uid() = user_id);

drop policy if exists "onboarding_sessions_insert_own" on public.onboarding_sessions;
create policy "onboarding_sessions_insert_own" on public.onboarding_sessions
  for insert with check (auth.uid() = user_id);

drop policy if exists "onboarding_sessions_update_own" on public.onboarding_sessions;
create policy "onboarding_sessions_update_own" on public.onboarding_sessions
  for update using (auth.uid() = user_id)
               with check (auth.uid() = user_id);

drop policy if exists "onboarding_sessions_delete_own" on public.onboarding_sessions;
create policy "onboarding_sessions_delete_own" on public.onboarding_sessions
  for delete using (auth.uid() = user_id);

-- 2. onboarding_messages ─────────────────────────────────────────────────────
--
-- `ordering` is a per-session integer, kept distinct from `created_at` so the
-- API can assign a monotonic position without depending on insert timing
-- (useful for batched inserts at session completion).

create table if not exists public.onboarding_messages (
  id          uuid        primary key default gen_random_uuid(),
  session_id  uuid        not null references public.onboarding_sessions(id) on delete cascade,
  user_id     uuid        not null references auth.users(id) on delete cascade,
  role        text        not null check (role in ('user', 'assistant')),
  content     text        not null,
  ordering    int         not null,
  created_at  timestamptz not null default now(),
  unique (session_id, ordering)
);

-- The unique constraint above implicitly indexes (session_id, ordering), which
-- is also the read pattern: "fetch all messages for a session in order".

alter table public.onboarding_messages enable row level security;

drop policy if exists "onboarding_messages_select_own" on public.onboarding_messages;
create policy "onboarding_messages_select_own" on public.onboarding_messages
  for select using (auth.uid() = user_id);

drop policy if exists "onboarding_messages_insert_own" on public.onboarding_messages;
create policy "onboarding_messages_insert_own" on public.onboarding_messages
  for insert with check (auth.uid() = user_id);

drop policy if exists "onboarding_messages_update_own" on public.onboarding_messages;
create policy "onboarding_messages_update_own" on public.onboarding_messages
  for update using (auth.uid() = user_id)
               with check (auth.uid() = user_id);

drop policy if exists "onboarding_messages_delete_own" on public.onboarding_messages;
create policy "onboarding_messages_delete_own" on public.onboarding_messages
  for delete using (auth.uid() = user_id);
