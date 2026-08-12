import { NextResponse } from 'next/server'

import { applySubscriptionChange } from '@/lib/billing/apply'
import { getBillingProvider } from '@/lib/billing/provider'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Billing webhook endpoint (ENT-86).
 *
 * Provider-agnostic: it reads the raw body and hands it plus the request headers
 * to the configured provider, which verifies its own signature and returns a
 * normalized state change (or null for events we ignore). An invalid/missing
 * signature is rejected with 400; everything else 200s so the provider stops
 * retrying. State is written via the service role, and idempotency (replays
 * don't double-apply) lives in `applySubscriptionChange`.
 */

// The signature is computed over the raw body, so it must not be parsed/cached.
export const dynamic = 'force-dynamic'

export async function POST(request: Request) {
  const rawBody = await request.text()

  let change
  try {
    const provider = getBillingProvider()
    change = await provider.parseWebhook(rawBody, request.headers)
  } catch {
    return NextResponse.json({ error: 'invalid signature' }, { status: 400 })
  }

  if (!change) {
    return NextResponse.json({ received: true, ignored: true }, { status: 200 })
  }

  const admin = createServiceRoleClient()
  const applied = await applySubscriptionChange(admin, change)
  return NextResponse.json({ received: true, applied }, { status: 200 })
}
