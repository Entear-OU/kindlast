import { NextResponse } from 'next/server'

import { getEmailProvider } from '@/lib/email/provider'
import { dispatchDeadlineAlerts } from '@/lib/notifications/deadline-dispatch'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Deadline alert cron endpoint (ENT-75).
 *
 * Vercel Cron hits this daily (see vercel.json) with a GET and an
 * `Authorization: Bearer ${CRON_SECRET}` header — after the pg_cron Watcher /
 * Analyst refresh the findings' live days-remaining. The dispatcher emails any
 * deadline finding that has crossed a 30/14/7/1-day threshold since last run.
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

  const summary = await dispatchDeadlineAlerts({
    supabase: createServiceRoleClient(),
    emailProvider: getEmailProvider(),
    baseUrl: resolveBaseUrl(request),
    tokenSecret,
  })

  return NextResponse.json(summary, { status: 200 })
}
