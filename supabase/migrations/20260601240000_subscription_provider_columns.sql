-- Subscription billing-provider columns (ENT-85)
--
-- Checkout (ENT-85) and the webhook (ENT-86) need to tie a `subscriptions` row to
-- the payment provider's customer record. Kept provider-agnostic so Stripe can be
-- swapped for another processor without a schema change:
--
--   * `provider`             — which processor owns this customer ('stripe', …).
--   * `provider_customer_id` — the processor's customer id, recorded at checkout.
--
-- The webhook resolves the local user from `provider_customer_id`, so it's
-- indexed. Both columns are nullable: a Free user who has never started checkout
-- has neither. Writes stay service-role-only (no user write policy added) — the
-- checkout action and webhook run as the service role; a user still cannot change
-- their own billing state.
--
-- Idempotent: `add column if not exists`, `create index if not exists`.
-- ─────────────────────────────────────────────────────────────────────────────

alter table public.subscriptions
  add column if not exists provider             text,
  add column if not exists provider_customer_id text;

create index if not exists subscriptions_provider_customer_idx
  on public.subscriptions (provider_customer_id);
