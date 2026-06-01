import { NextResponse } from 'next/server'

import { getEmailProvider } from '@/lib/email/provider'
import { dispatchPendingNotifications } from '@/lib/notifications/dispatch'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Comms dispatch endpoint (ENT-73).
 *
 * Drains the notification outbox and sends finding emails. Designed to be hit
 * by a scheduler — Vercel Cron invokes the path with a GET and an
 * `Authorization: Bearer ${CRON_SECRET}` header, so the route is a GET that
 * rejects anything without that secret. Writes go through the service role; the
 * email provider is chosen from env (console in dev, Resend in prod).
 */

export const dynamic = 'force-dynamic'

function authorized(request: Request): boolean {
  const secret = process.env.CRON_SECRET
  if (!secret) return false // fail closed: no secret configured ⇒ no access
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

  const summary = await dispatchPendingNotifications({
    supabase: createServiceRoleClient(),
    emailProvider: getEmailProvider(),
    baseUrl: resolveBaseUrl(request),
    tokenSecret,
  })

  return NextResponse.json(summary, { status: 200 })
}
