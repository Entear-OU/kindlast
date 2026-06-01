-- ENT-75 — Founder receives 30-day deadline alerts.
--
-- A deadline finding (Watcher 'deadline'/'dsar' signal) gets a dedicated alert
-- email that re-fires only as it crosses the 30/14/7/1-day thresholds — never
-- daily noise. This table is the per-threshold dedup guard: the dispatcher
-- claims (finding_id, threshold) before sending, so each threshold a finding
-- passes through fires exactly once. Writes go through the service role.

create table if not exists public.deadline_alert_log (
  finding_id uuid        not null references public.findings(id) on delete cascade,
  threshold  integer     not null check (threshold in (1, 7, 14, 30)),
  user_id    uuid        not null references auth.users(id) on delete cascade,
  sent_at    timestamptz not null default now(),
  primary key (finding_id, threshold)
);

-- The RLS predicate leads on user_id; the PK leads on finding_id, so add an
-- index for owner reads.
create index if not exists deadline_alert_log_user_idx
  on public.deadline_alert_log (user_id);

-- RLS: a user may read their own alert history (for an in-app activity view
-- later). No INSERT/UPDATE/DELETE policy — the dispatcher writes via the service
-- role and RLS denies by default, which is the enforcement.
alter table public.deadline_alert_log enable row level security;

drop policy if exists "deadline_alert_log_select_own" on public.deadline_alert_log;
create policy "deadline_alert_log_select_own" on public.deadline_alert_log
  for select using (auth.uid() = user_id);
