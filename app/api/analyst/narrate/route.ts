import { NextResponse } from 'next/server'

import { narratePendingFindings } from '@/lib/analyst/narrate-sweep'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Analyst narrative cron endpoint (ENT-162).
 *
 * The Watcher and Analyst run as pg_cron SQL inside Postgres (06:00 and 06:05
 * UTC), and `analyst_convert_signal` leaves each new finding on a generic
 * baseline sentence. SQL cannot call the TypeScript narrative layer, so this
 * route is the bridge: Vercel Cron hits it shortly after the SQL sweep and
 * replaces those baselines with specific, critic-approved actions.
 *
 * Same contract as the notification crons: GET with
 * `Authorization: Bearer ${CRON_SECRET}`, fail closed when the secret is unset.
 */

export const dynamic = 'force-dynamic'

function authorized(request: Request): boolean {
  const secret = process.env.CRON_SECRET
  if (!secret) return false // fail closed
  return request.headers.get('authorization') === `Bearer ${secret}`
}

export async function GET(request: Request) {
  if (!authorized(request)) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  const summary = await narratePendingFindings({ supabase: createServiceRoleClient() })

  return NextResponse.json(summary, { status: 200 })
}
