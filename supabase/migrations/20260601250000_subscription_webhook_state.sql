-- Subscription webhook state + idempotency (ENT-86)
--
-- The billing webhook (ENT-86) drives the source-of-truth subscription state. It
-- needs two things this migration adds:
--
--   * `subscriptions.current_period_end` — when the paid period ends, so the UI
--     and any dunning logic know how long Pro access is good for.
--   * `billing_webhook_events` — a ledger of processed provider event ids, so a
--     replayed webhook is a no-op (AC: "replays don't double-apply"). The id is
--     the primary key; the handler skips any event id it has already recorded.
--
-- Both are service-role-only: the webhook runs as the service role and a user
-- must never write their own billing state. `subscriptions` already carries no
-- user write policy; the new events table enables RLS with no policies at all, so
-- only the service role (RLS-exempt) can touch it.
--
-- Idempotent: `add column if not exists`, `create table if not exists`.
-- ─────────────────────────────────────────────────────────────────────────────

alter table public.subscriptions
  add column if not exists current_period_end timestamptz;

create table if not exists public.billing_webhook_events (
  event_id     text        primary key,
  processed_at timestamptz not null default now()
);

alter table public.billing_webhook_events enable row level security;
-- No policies: service-role only (the webhook handler). RLS-enabled with zero
-- policies denies all non-service-role access by default.
