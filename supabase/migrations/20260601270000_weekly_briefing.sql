-- ENT-74 — Founder receives weekly Monday compliance briefing.
--
-- Adds the two preference fields the weekly briefing needs (a per-user timezone
-- so "Monday 09:00" is local, and an opt-out flag the footer link toggles) and a
-- once-per-week idempotency log. The full notification-preferences settings UI
-- and the remaining columns (min_severity_for_email, quiet hours) land in ENT-76
-- on top of these.

-- 1. notification_preferences — timezone + opt-out ──────────────────────────────
alter table public.notification_preferences
  add column if not exists timezone text not null default 'Europe/Tallinn';

alter table public.notification_preferences
  add column if not exists weekly_briefing_enabled boolean not null default true;

-- 2. weekly_briefing_log — one row per user per delivered week ───────────────────
--
-- The composite primary key (user_id, period_start) is the idempotency guard:
-- the hourly briefing cron inserts the Monday's date (in the user's timezone)
-- with on-conflict-do-nothing, so a user is briefed at most once per week even
-- though the cron evaluates them every hour. period_start is the local Monday.
create table if not exists public.weekly_briefing_log (
  user_id      uuid        not null references auth.users(id) on delete cascade,
  period_start date        not null,
  sent_at      timestamptz not null default now(),
  primary key (user_id, period_start)
);

-- RLS: a user may read their own briefing history (for an in-app activity view
-- later). There is no INSERT/UPDATE/DELETE policy — the dispatcher writes via the
-- service role, and RLS denies by default, which is the enforcement.
alter table public.weekly_briefing_log enable row level security;

drop policy if exists "weekly_briefing_log_select_own" on public.weekly_briefing_log;
create policy "weekly_briefing_log_select_own" on public.weekly_briefing_log
  for select using (auth.uid() = user_id);
