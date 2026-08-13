import type { SupabaseClient } from '@supabase/supabase-js'

import type { SubscriptionStateChange } from './types'

/**
 * Apply a normalized subscription change from a verified webhook (ENT-86).
 *
 * Idempotent: each provider event id is recorded in `billing_webhook_events`
 * once. A replayed event is detected up front and skipped, so it never
 * double-applies. The caller must pass a service-role client — `subscriptions`
 * and the events ledger are both service-role-write only.
 *
 * The user is resolved by `user_id` when the event carried it in metadata
 * (preferred), otherwise by `provider_customer_id` (recorded at checkout). The
 * period end is only written when the event reports one, so an event that omits
 * it (e.g. checkout.session.completed) never nulls a value a later event set.
 *
 * Returns true when the change was applied, false when it was a recorded replay.
 */
export async function applySubscriptionChange(
  admin: SupabaseClient,
  change: SubscriptionStateChange,
): Promise<boolean> {
  // Idempotency gate: have we already processed this event?
  const { data: seen } = await admin
    .from('billing_webhook_events')
    .select('event_id')
    .eq('event_id', change.eventId)
    .maybeSingle()
  if (seen) return false

  const patch: Record<string, unknown> = {
    plan: change.plan,
    status: change.status,
    provider_customer_id: change.customerId,
  }
  if (change.currentPeriodEnd !== null) {
    patch.current_period_end = change.currentPeriodEnd
  }

  const update = admin.from('subscriptions').update(patch)
  const { error } = await (change.userId
    ? update.eq('user_id', change.userId)
    : update.eq('provider_customer_id', change.customerId))
  if (error) {
    throw new Error(`applySubscriptionChange: ${error.message}`)
  }

  // Record only after a successful apply, so a failed apply is retried by the
  // provider rather than silently marked done.
  await admin.from('billing_webhook_events').insert({ event_id: change.eventId })
  return true
}
