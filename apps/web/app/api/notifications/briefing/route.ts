import { NextResponse } from 'next/server'

import { getEmailProvider } from '@/lib/email/provider'
import { dispatchWeeklyBriefing } from '@/lib/notifications/briefing-dispatch'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Weekly briefing cron endpoint (ENT-74).
 *
 * Vercel Cron hits this hourly (see vercel.json) with a GET and an
 * `Authorization: Bearer ${CRON_SECRET}` header. The dispatcher decides, per
 * user, whether it's their local Monday 09:00 and sends the posture digest.
 * Writes go through the service role; the email provider is env-chosen.
 */

export const dynamic = 'force-dynamic'

function authorized(request: Request): boolean {
  const secret = process.env.CRON_SECRET
  if (!secret) return false // fail closed
  return request.headers.get('authorization') === `Bearer ${secret}`
}

function resolveBaseUrl(request: Request): string {
  return process.env.NEXT_PUBLIC_APP_URL ?? new URL(request.url).origin
}

export async function GET(request: Request) {
  if (!authorized(request)) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  const tokenSecret = process.env.NOTIFICATION_TOKEN_SECRET
  if (!tokenSecret) {
    return NextResponse.json({ error: 'NOTIFICATION_TOKEN_SECRET is not configured' }, { status: 500 })
  }

  const summary = await dispatchWeeklyBriefing({
    supabase: createServiceRoleClient(),
    emailProvider: getEmailProvider(),
    baseUrl: resolveBaseUrl(request),
    tokenSecret,
  })

  return NextResponse.json(summary, { status: 200 })
}
