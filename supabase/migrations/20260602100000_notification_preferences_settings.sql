-- ENT-76 — Founder configures email notification preferences.
--
-- Completes the notification_preferences schema the Comms epic seeded
-- (ENT-61/74 added email_frequency, timezone, weekly_briefing_enabled) and
-- pivots the finding-email gate from the email_frequency stand-in to a real
-- per-severity floor. The settings screen (this issue) writes these columns.

-- New preference columns ────────────────────────────────────────────────────────
alter table public.notification_preferences
  -- Preferred delivery address; null falls back to the auth email at send time.
  add column if not exists email                   text,
  -- Severity floor for the immediate finding email. critical always sends.
  add column if not exists min_severity_for_email  public.severity_level not null default 'medium',
  add column if not exists deadline_alerts_enabled boolean not null default true,
  -- Quiet hours in the user's timezone; null = no quiet window. Non-critical
  -- finding emails are held until the window ends.
  add column if not exists quiet_hours_start       time,
  add column if not exists quiet_hours_end         time;

-- Drop the superseded cadence column. The gate is now min_severity_for_email;
-- the weekly briefing covers digest cadence. The email_frequency enum type is
-- left in place (harmless, and dropping a type in use elsewhere is riskier).
alter table public.notification_preferences
  drop column if exists email_frequency;
